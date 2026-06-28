package reconcile

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fluxcd/pkg/runtime/patch"
	fluxssa "github.com/fluxcd/pkg/ssa"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/apply"
	"github.com/open-platform-model/opm-operator/internal/inventory"
	"github.com/open-platform-model/opm-operator/internal/render"
	opmsource "github.com/open-platform-model/opm-operator/internal/source"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// Was: DefaultReleaseInterval
// DefaultModulePackageInterval is the fallback requeue interval when spec.interval
// is not set.
const DefaultModulePackageInterval = 5 * time.Minute

// Was: ReleaseParams
// ModulePackageParams holds the dependencies for the ModulePackage reconcile loop.
type ModulePackageParams struct {
	Client client.Client
	// APIReader is an uncached reader used for one-off reads (e.g. ServiceAccount
	// existence checks for impersonation) that should not provision a cache informer.
	APIReader       client.Reader
	RestConfig      *rest.Config
	ResourceManager *fluxssa.ResourceManager
	EventRecorder   events.EventRecorder

	// Fetcher downloads Flux source artifacts. Typically
	// &opmsource.ArtifactFetcher{} in production; tests inject a stub.
	Fetcher opmsource.Fetcher

	// Renderer loads and renders a CUE package from a local directory.
	// Production wires render.KernelPackageRenderer; tests inject a stub. It is
	// required — a nil Renderer is a programming error.
	Renderer render.PackageRenderer

	// DefaultServiceAccount is the fallback SA name used when a ModulePackage has
	// an empty spec.serviceAccountName. Empty disables the default and
	// preserves the controller-client fallback.
	DefaultServiceAccount string
}

// Was: ReconcileRelease
// ReconcileModulePackage runs the full ModulePackage reconcile loop: source resolution,
// artifact fetch, path navigation, CUE load, kind detection, render, apply,
// prune, and status commit. Mirrors the ModuleInstance loop but sources the
// CUE package from a Flux artifact instead of synthesizing it.
func ReconcileModulePackage(
	ctx context.Context,
	params *ModulePackageParams,
	req ctrl.Request,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var pkg releasesv1alpha1.ModulePackage
	if err := params.Client.Get(ctx, req.NamespacedName, &pkg); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	reconcileStart := time.Now()
	interval := pkg.Spec.Interval.Duration
	if interval == 0 {
		interval = DefaultModulePackageInterval
	}

	// Finalizer patches don't bump .metadata.generation, so
	// GenerationChangedPredicate filters the subsequent UPDATE event —
	// explicit Requeue re-enters the workqueue.
	if !controllerutil.ContainsFinalizer(&pkg, FinalizerName) {
		log.Info("Adding finalizer to ModulePackage")
		if err := addModulePackageFinalizer(ctx, params.Client, &pkg); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
		return ctrl.Result{Requeue: true}, nil
	}

	if !pkg.DeletionTimestamp.IsZero() {
		return handleModulePackageDeletion(ctx, params, &pkg)
	}

	patcher := patch.NewSerialPatcher(&pkg, params.Client)

	if pkg.Spec.Suspend {
		log.Info("Reconciliation is suspended")
		status.MarkSuspended(&pkg)
		pkg.Status.ObservedGeneration = pkg.Generation
		params.EventRecorder.Eventf(&pkg, nil, corev1.EventTypeNormal, status.SuspendedReason, "Suspend", "Reconciliation is suspended")
		if err := patchModulePackageStatus(ctx, patcher, &pkg); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	if ready := apimeta.FindStatusCondition(pkg.Status.Conditions, status.ReadyCondition); ready != nil && ready.Reason == status.SuspendedReason {
		log.Info("Reconciliation resumed")
		params.EventRecorder.Eventf(&pkg, nil, corev1.EventTypeNormal, status.ResumedReason, "Resume", "Reconciliation resumed")
	}

	// Check dependsOn before any other work.
	if blocker, checkErr := checkDependsOn(ctx, params.Client, &pkg); checkErr != nil {
		status.MarkNotReady(&pkg, status.DependenciesNotReadyReason, "%s", checkErr)
		params.EventRecorder.Eventf(&pkg, nil, corev1.EventTypeWarning, status.DependenciesNotReadyReason, "DependsOn", "%s", checkErr)
		pkg.Status.ObservedGeneration = pkg.Generation
		_ = patchModulePackageStatus(ctx, patcher, &pkg)
		return ctrl.Result{RequeueAfter: interval}, nil
	} else if blocker != "" {
		msg := fmt.Sprintf("waiting for dependency %s", blocker)
		status.MarkNotReady(&pkg, status.DependenciesNotReadyReason, "%s", msg)
		params.EventRecorder.Eventf(&pkg, nil, corev1.EventTypeNormal, status.DependenciesNotReadyReason, "DependsOn", "%s", msg)
		pkg.Status.ObservedGeneration = pkg.Generation
		_ = patchModulePackageStatus(ctx, patcher, &pkg)
		return ctrl.Result{RequeueAfter: interval}, nil
	}

	var (
		outcome    Outcome
		digests    status.DigestSet
		reconciled bool
		newEntries []releasesv1alpha1.InventoryEntry
		errMsg     string
		retryAfter time.Duration
		phases     phaseOutcomes
	)

	defer func() {
		now := metav1.Now()
		pkg.Status.ObservedGeneration = pkg.Generation

		if outcome == NoOp {
			status.MarkReady(&pkg, "Reconciliation succeeded")
			updateModulePackageFailureCounters(&pkg.Status, outcome, phases)
			pkg.Status.NextRetryAt = nil
			if err := patchModulePackageStatus(ctx, patcher, &pkg); err != nil {
				log.Error(err, "Failed to patch NoOp status")
			}
			return
		}

		pkg.Status.LastAttemptedAction = "reconcile"
		pkg.Status.LastAttemptedAt = &now
		duration := metav1.Duration{Duration: time.Since(reconcileStart)}
		pkg.Status.LastAttemptedDuration = &duration
		pkg.Status.LastAttemptedSourceDigest = digests.Source
		pkg.Status.LastAttemptedConfigDigest = digests.Config
		pkg.Status.LastAttemptedRenderDigest = digests.Render

		if reconciled {
			pkg.Status.LastAppliedAt = &now
			pkg.Status.LastAppliedSourceDigest = digests.Source
			pkg.Status.LastAppliedConfigDigest = digests.Config
			pkg.Status.LastAppliedRenderDigest = digests.Render

			invDigest := inventory.ComputeDigest(newEntries)
			rev := int64(1)
			if pkg.Status.Inventory != nil {
				rev = pkg.Status.Inventory.Revision + 1
			}
			pkg.Status.Inventory = &releasesv1alpha1.Inventory{
				Revision: rev,
				Digest:   invDigest,
				Count:    int64(len(newEntries)),
				Entries:  newEntries,
			}
			digests.Inventory = invDigest

			status.RecordModulePackageHistory(&pkg.Status, status.NewSuccessEntry("reconcile", "complete", digests, int64(len(newEntries))))
		} else if errMsg != "" {
			status.RecordModulePackageHistory(&pkg.Status, status.NewFailureEntry("reconcile", errMsg, digests))
		}

		updateModulePackageFailureCounters(&pkg.Status, outcome, phases)

		if retryAfter > 0 {
			t := metav1.NewTime(time.Now().Add(retryAfter))
			pkg.Status.NextRetryAt = &t
		} else {
			pkg.Status.NextRetryAt = nil
		}

		if err := patchModulePackageStatus(ctx, patcher, &pkg); err != nil {
			log.Error(err, "Failed to patch ModulePackage status")
		}
	}()

	status.MarkReconciling(&pkg, "Progressing", "Reconciliation in progress")

	applyFail := func(fail *phaseFail) {
		outcome = fail.outcome
		errMsg = fail.errMsg
		retryAfter = fail.retryAfter
	}

	// Phase 1: resolve source.
	artifactRef, fail := resolveModulePackageSource(ctx, params, &pkg, interval)
	if fail != nil {
		applyFail(fail)
		return ctrl.Result{RequeueAfter: retryAfter}, nil
	}
	pkg.Status.Source = &releasesv1alpha1.SourceStatus{
		Ref:              &pkg.Spec.SourceRef,
		ArtifactRevision: artifactRef.Revision,
		ArtifactDigest:   artifactRef.Digest,
		ArtifactURL:      artifactRef.URL,
	}
	digests.Source = artifactRef.Digest

	// Phase 2: fetch + extract artifact.
	extractDir, fail := fetchModulePackageArtifact(ctx, params, &pkg, artifactRef, interval)
	if fail != nil {
		applyFail(fail)
		return ctrl.Result{RequeueAfter: retryAfter}, nil
	}
	defer func() { _ = os.RemoveAll(extractDir) }()

	// Phase 3: navigate to spec.path.
	packageDir, fail := navigateModulePackagePath(&pkg, extractDir, params.EventRecorder)
	if fail != nil {
		applyFail(fail)
		return ctrl.Result{RequeueAfter: retryAfter}, nil
	}

	// Phase 4+5: load CUE, detect kind, render.
	renderResult, fail := renderModulePackage(ctx, params, &pkg, packageDir, interval)
	if fail != nil {
		applyFail(fail)
		return ctrl.Result{RequeueAfter: retryAfter}, nil
	}

	if fail := computeModulePackageDigests(&pkg, renderResult, &digests); fail != nil {
		applyFail(fail)
		return ctrl.Result{RequeueAfter: retryAfter}, nil
	}

	lastApplied := status.DigestSet{
		Source:    pkg.Status.LastAppliedSourceDigest,
		Config:    pkg.Status.LastAppliedConfigDigest,
		Render:    pkg.Status.LastAppliedRenderDigest,
		Inventory: inventoryDigestModulePackage(pkg.Status.Inventory),
	}
	if status.IsNoOp(digests, lastApplied) {
		log.Info("No changes detected, skipping apply")
		params.EventRecorder.Eventf(&pkg, nil, corev1.EventTypeNormal, status.NoOpReason, "Reconcile", "No changes detected")
		outcome = NoOp
		return ctrl.Result{RequeueAfter: interval}, nil
	}

	applyedResult, fail := applyAndPruneModulePackage(ctx, params, &pkg, renderResult, &phases)
	if fail != nil {
		applyFail(fail)
		return ctrl.Result{RequeueAfter: retryAfter}, nil
	}

	outcome = applyedResult.outcome
	newEntries = applyedResult.entries
	reconciled = true
	status.MarkReady(&pkg, "Reconciliation succeeded")
	params.EventRecorder.Eventf(&pkg, nil, corev1.EventTypeNormal, status.ReconciliationSucceededReason, "Reconcile", "Reconciliation succeeded")
	log.Info("Reconciliation complete", "outcome", outcome.String())

	return ctrl.Result{RequeueAfter: interval}, nil
}

// phaseFail captures a phase failure so the top-level loop can record outcome,
// error message, and retry timing without its own switch branches.
type phaseFail struct {
	outcome    Outcome
	errMsg     string
	retryAfter time.Duration
}

func resolveModulePackageSource(
	ctx context.Context,
	params *ModulePackageParams,
	pkg *releasesv1alpha1.ModulePackage,
	interval time.Duration,
) (*opmsource.ArtifactRef, *phaseFail) {
	artifactRef, err := opmsource.Resolve(ctx, params.Client, pkg.Spec.SourceRef, pkg.Namespace)
	if err == nil {
		return artifactRef, nil
	}
	reason := status.SourceNotReadyReason
	stalled := errors.Is(err, opmsource.ErrSourceNotFound) || errors.Is(err, opmsource.ErrUnsupportedSourceKind)
	params.EventRecorder.Eventf(pkg, nil, corev1.EventTypeWarning, reason, "Resolve", "%s", err)
	if stalled {
		status.MarkStalled(pkg, reason, "%s", err)
		return nil, &phaseFail{FailedStalled, err.Error(), StalledRecheckInterval}
	}
	status.MarkNotReady(pkg, reason, "%s", err)
	return nil, &phaseFail{FailedTransient, err.Error(), interval}
}

func fetchModulePackageArtifact(
	ctx context.Context,
	params *ModulePackageParams,
	pkg *releasesv1alpha1.ModulePackage,
	artifactRef *opmsource.ArtifactRef,
	interval time.Duration,
) (string, *phaseFail) {
	extractDir, err := os.MkdirTemp("", "opm-modulepackage-artifact-*")
	if err != nil {
		status.MarkNotReady(pkg, status.FetchFailedReason, "creating temp dir: %s", err)
		return "", &phaseFail{FailedTransient, err.Error(), interval}
	}
	fetcher := params.Fetcher
	if fetcher == nil {
		fetcher = &opmsource.ArtifactFetcher{}
	}
	opts := opmsource.FetchOptions{
		Format:                      opmsource.FormatForKind(artifactRef.Kind),
		SkipRootCUEModuleValidation: true,
	}
	if err := fetcher.Fetch(ctx, artifactRef.URL, artifactRef.Digest, extractDir, opts); err != nil {
		_ = os.RemoveAll(extractDir)
		params.EventRecorder.Eventf(pkg, nil, corev1.EventTypeWarning, status.FetchFailedReason, "Fetch", "%s", err)
		status.MarkNotReady(pkg, status.FetchFailedReason, "%s", err)
		return "", &phaseFail{FailedTransient, err.Error(), interval}
	}
	return extractDir, nil
}

func navigateModulePackagePath(
	pkg *releasesv1alpha1.ModulePackage,
	extractDir string,
	recorder events.EventRecorder,
) (string, *phaseFail) {
	packageDir, err := resolvePackagePath(extractDir, pkg.Spec.Path)
	if err == nil {
		return packageDir, nil
	}
	reason := status.PathNotFoundReason
	if errors.Is(err, errInstanceFileMissing) {
		reason = status.InstanceFileNotFoundReason
	}
	status.MarkStalled(pkg, reason, "%s", err)
	recorder.Eventf(pkg, nil, corev1.EventTypeWarning, reason, "Load", "%s", err)
	return "", &phaseFail{FailedStalled, err.Error(), StalledRecheckInterval}
}

func renderModulePackage(
	ctx context.Context,
	params *ModulePackageParams,
	pkg *releasesv1alpha1.ModulePackage,
	packageDir string,
	interval time.Duration,
) (*render.RenderResult, *phaseFail) {
	kind, result, err := params.Renderer.Render(ctx, packageDir)
	if err != nil {
		// PlatformNotReady is a blocked-on-dependency state, not a stall: the
		// store holds no materialized platform yet. Mark Ready=False/
		// PlatformNotReady (non-stalled), apply and prune nothing, and requeue.
		// The Platform watch (mapPlatformToModulePackages) re-enqueues promptly when
		// the platform materializes; the interval requeue is the safety net.
		if errors.Is(err, render.ErrPlatformNotReady) {
			status.MarkNotReady(pkg, status.PlatformNotReadyReason, "%s", err)
			params.EventRecorder.Eventf(pkg, nil, corev1.EventTypeWarning, status.PlatformNotReadyReason, "Render", "%s", err)
			return nil, &phaseFail{FailedTransient, err.Error(), interval}
		}
		reason := renderErrorReason(err)
		status.MarkStalled(pkg, reason, "%s", err)
		params.EventRecorder.Eventf(pkg, nil, corev1.EventTypeWarning, reason, "Render", "%s", err)
		return nil, &phaseFail{FailedStalled, err.Error(), StalledRecheckInterval}
	}
	if kind != render.KindModuleInstance {
		msg := fmt.Sprintf("unexpected instance kind %q", kind)
		status.MarkStalled(pkg, status.UnsupportedKindReason, "%s", msg)
		return nil, &phaseFail{FailedStalled, msg, StalledRecheckInterval}
	}
	return result, nil
}

func renderErrorReason(err error) string {
	switch {
	case errors.Is(err, render.ErrUnsupportedKind):
		return status.UnsupportedKindReason
	case isResolutionErrorMsg(err):
		return status.ResolutionFailedReason
	default:
		return status.RenderFailedReason
	}
}

func computeModulePackageDigests(
	pkg *releasesv1alpha1.ModulePackage,
	renderResult *render.RenderResult,
	digests *status.DigestSet,
) *phaseFail {
	renderDigest, err := status.RenderDigest(renderResult.Resources)
	if err != nil {
		status.MarkStalled(pkg, status.RenderFailedReason, "computing render digest: %s", err)
		return &phaseFail{FailedStalled, err.Error(), StalledRecheckInterval}
	}
	digests.Render = renderDigest
	digests.Inventory = inventory.ComputeDigest(renderResult.InventoryEntries)
	// A ModulePackage carries no user values — config digest hashes empty input so
	// NoOp detection stays consistent across reconciles.
	digests.Config = status.ConfigDigest(nil)
	return nil
}

// applyPruneResult captures the outputs of the apply+prune phase.
type applyPruneResult struct {
	outcome Outcome
	entries []releasesv1alpha1.InventoryEntry
}

func applyAndPruneModulePackage(
	ctx context.Context,
	params *ModulePackageParams,
	pkg *releasesv1alpha1.ModulePackage,
	renderResult *render.RenderResult,
	phases *phaseOutcomes,
) (*applyPruneResult, *phaseFail) {
	log := logf.FromContext(ctx)

	resources, err := toUnstructuredSlice(renderResult.Resources)
	if err != nil {
		status.MarkStalled(pkg, status.ApplyFailedReason, "converting resources: %s", err)
		return nil, &phaseFail{FailedStalled, err.Error(), StalledRecheckInterval}
	}

	var previousEntries []releasesv1alpha1.InventoryEntry
	if pkg.Status.Inventory != nil {
		previousEntries = pkg.Status.Inventory.Entries
	}
	staleSet := inventory.ComputeStaleSet(previousEntries, renderResult.InventoryEntries)

	applyRM, applyClient, impErr := buildModulePackageApplyClient(ctx, params, pkg)
	if impErr != nil {
		status.MarkStalled(pkg, status.ImpersonationFailedReason, "%s", impErr)
		return nil, &phaseFail{FailedStalled, impErr.Error(), StalledRecheckInterval}
	}

	// Apply.
	phases.applyRan = true
	force := pkg.Spec.Rollout != nil && pkg.Spec.Rollout.ForceConflicts
	applyResult, err := apply.Apply(ctx, applyRM, resources, force)
	if err != nil {
		phases.applyFailed = true
		params.EventRecorder.Eventf(pkg, nil, corev1.EventTypeWarning, status.ApplyFailedReason, "Apply", "%s", err)
		status.MarkNotReady(pkg, status.ApplyFailedReason, "%s", err)
		return nil, &phaseFail{FailedTransient, err.Error(), modulePackageBackoff(pkg)}
	}
	total := applyResult.Created + applyResult.Updated + applyResult.Unchanged
	params.EventRecorder.Eventf(pkg, nil, corev1.EventTypeNormal, status.AppliedReason, "Apply",
		"Applied %d resources (%d created, %d updated, %d unchanged)",
		total, applyResult.Created, applyResult.Updated, applyResult.Unchanged)
	log.Info("Applied resources",
		"created", applyResult.Created, "updated", applyResult.Updated, "unchanged", applyResult.Unchanged)
	status.ClearDrifted(pkg)

	// Prune.
	phases.pruneRan = true
	outcome := Applied
	if pkg.Spec.Prune && len(staleSet) > 0 {
		// ModulePackage does not persist a release UUID on Status; pass empty and rely
		// on the managed-by check in the prune guard.
		pruneResult, pruneErr := apply.Prune(ctx, applyClient, "", staleSet)
		if pruneErr != nil {
			phases.pruneFailed = true
			params.EventRecorder.Eventf(pkg, nil, corev1.EventTypeWarning, status.PruneFailedReason, "Prune", "%s", pruneErr)
			status.MarkNotReady(pkg, status.PruneFailedReason, "%s", pruneErr)
			return nil, &phaseFail{FailedTransient, pruneErr.Error(), modulePackageBackoff(pkg)}
		}
		if pruneResult.Deleted > 0 {
			params.EventRecorder.Eventf(pkg, nil, corev1.EventTypeNormal, status.PrunedReason, "Prune",
				"Pruned %d stale resources", pruneResult.Deleted)
		}
		outcome = AppliedAndPruned
	}

	return &applyPruneResult{outcome: outcome, entries: renderResult.InventoryEntries}, nil
}

func modulePackageBackoff(pkg *releasesv1alpha1.ModulePackage) time.Duration {
	count := int64(0)
	if pkg.Status.FailureCounters != nil {
		count = pkg.Status.FailureCounters.Reconcile
	}
	return ComputeBackoff(count + 1)
}

// errInstanceFileMissing is returned by resolvePackagePath when the target
// directory exists but lacks instance.cue.
var errInstanceFileMissing = errors.New("instance.cue not found")

// resolvePackagePath joins root + relPath safely and verifies the directory
// contains instance.cue. Returns errInstanceFileMissing when the directory
// exists but has no instance.cue.
func resolvePackagePath(root, relPath string) (string, error) {
	cleaned := filepath.Clean("/" + relPath)
	if strings.Contains(cleaned, "..") {
		return "", fmt.Errorf("path %q contains traversal", relPath)
	}
	target := filepath.Join(root, strings.TrimPrefix(cleaned, "/"))
	info, err := os.Stat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("path %q does not exist in artifact", relPath)
		}
		return "", fmt.Errorf("stat %q: %w", relPath, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("path %q is not a directory", relPath)
	}
	if _, err := os.Stat(filepath.Join(target, "instance.cue")); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%w at %q", errInstanceFileMissing, relPath)
		}
		return "", fmt.Errorf("stat instance.cue: %w", err)
	}
	return target, nil
}

// checkDependsOn verifies all referenced ModulePackage CRs are Ready=True.
// Returns ("", nil) when dependencies satisfied or none declared.
// Returns (name, nil) with the first blocking dependency when not ready.
// Returns ("", err) when a dependency references a different namespace or
// another hard error occurs.
func checkDependsOn(
	ctx context.Context,
	c client.Client,
	pkg *releasesv1alpha1.ModulePackage,
) (string, error) {
	if len(pkg.Spec.DependsOn) == 0 {
		return "", nil
	}
	for _, dep := range pkg.Spec.DependsOn {
		if dep.Namespace != "" && dep.Namespace != pkg.Namespace {
			return "", fmt.Errorf("dependency %s/%s: cross-namespace dependencies are not supported", dep.Namespace, dep.Name)
		}
		var other releasesv1alpha1.ModulePackage
		key := types.NamespacedName{Name: dep.Name, Namespace: pkg.Namespace}
		if err := c.Get(ctx, key, &other); err != nil {
			if client.IgnoreNotFound(err) == nil {
				return fmt.Sprintf("%s/%s (not found)", pkg.Namespace, dep.Name), nil
			}
			return "", fmt.Errorf("getting dependency %s/%s: %w", pkg.Namespace, dep.Name, err)
		}
		ready := apimeta.FindStatusCondition(other.Status.Conditions, status.ReadyCondition)
		if ready == nil || ready.Status != metav1.ConditionTrue {
			return fmt.Sprintf("%s/%s", pkg.Namespace, dep.Name), nil
		}
	}
	return "", nil
}

// Was: updateReleaseFailureCounters
// updateModulePackageFailureCounters applies counter increments and resets for a
// ModulePackage based on phase outcomes and overall reconcile result.
func updateModulePackageFailureCounters(
	rs *releasesv1alpha1.ModulePackageStatus,
	outcome Outcome,
	phases phaseOutcomes,
) {
	counters := status.EnsureModulePackageCounters(rs)

	if phases.applyRan {
		if phases.applyFailed {
			status.IncrementCounter(counters, status.CounterApply)
		} else {
			status.ResetCounter(counters, status.CounterApply)
		}
	}
	if phases.pruneRan {
		if phases.pruneFailed {
			status.IncrementCounter(counters, status.CounterPrune)
		} else {
			status.ResetCounter(counters, status.CounterPrune)
		}
	}
	switch outcome {
	case FailedTransient, FailedStalled:
		status.IncrementCounter(counters, status.CounterReconcile)
	case Applied, AppliedAndPruned, NoOp:
		status.ResetCounter(counters, status.CounterReconcile)
	}
}

func patchModulePackageStatus(ctx context.Context, patcher *patch.SerialPatcher, pkg *releasesv1alpha1.ModulePackage) error {
	return patcher.Patch(ctx, pkg,
		patch.WithOwnedConditions{
			Conditions: []string{
				status.ReadyCondition,
				status.ReconcilingCondition,
				status.StalledCondition,
				status.DriftedCondition,
			},
		},
		patch.WithStatusObservedGeneration{},
	)
}

func addModulePackageFinalizer(ctx context.Context, c client.Client, pkg *releasesv1alpha1.ModulePackage) error {
	mergePatch := client.MergeFrom(pkg.DeepCopy())
	controllerutil.AddFinalizer(pkg, FinalizerName)
	return c.Patch(ctx, pkg, mergePatch)
}

func removeModulePackageFinalizer(ctx context.Context, c client.Client, pkg *releasesv1alpha1.ModulePackage) error {
	mergePatch := client.MergeFrom(pkg.DeepCopy())
	controllerutil.RemoveFinalizer(pkg, FinalizerName)
	return c.Patch(ctx, pkg, mergePatch)
}

// Was: handleReleaseDeletion
// handleModulePackageDeletion runs the deletion cleanup path. Mirrors
// handleDeletion in moduleinstance.go — both share the same SA-missing-at-delete
// bug class and are kept symmetric on purpose. See that function's doc and
// design.md (deletion-sa-missing-stall) for the stall/orphan branches.
func handleModulePackageDeletion(ctx context.Context, params *ModulePackageParams, pkg *releasesv1alpha1.ModulePackage) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Running deletion cleanup for ModulePackage")

	patcher := patch.NewSerialPatcher(pkg, params.Client)

	if !pkg.Spec.Prune || pkg.Status.Inventory == nil || len(pkg.Status.Inventory.Entries) == 0 {
		if !pkg.Spec.Prune {
			log.Info("Prune disabled, orphaning managed resources on deletion")
		}
		if err := removeModulePackageFinalizer(ctx, params.Client, pkg); err != nil {
			return ctrl.Result{}, fmt.Errorf("removing finalizer: %w", err)
		}
		log.Info("Finalizer removed, deletion can proceed")
		return ctrl.Result{}, nil
	}

	effectiveSA, source := resolveEffectiveSA(pkg.Spec.ServiceAccountName, params.DefaultServiceAccount)
	deleteClient := params.Client
	if effectiveSA != "" && params.RestConfig != nil {
		impClient, impErr := apply.NewImpersonatedClient(ctx, params.RestConfig, params.APIReader, params.Client.Scheme(), pkg.Namespace, effectiveSA)
		if impErr != nil {
			return handleModulePackageDeletionImpersonationFailure(ctx, params, patcher, pkg, effectiveSA, source, impErr)
		}
		deleteClient = impClient
	}

	pruneResult, err := apply.Prune(ctx, deleteClient, "", pkg.Status.Inventory.Entries)
	if err != nil {
		if effectiveSA != "" && isForbidden(err) {
			log.Error(err, "Impersonation denied during deletion cleanup",
				"serviceAccount", effectiveSA,
				"serviceAccountSource", source)
			emit := !readyAlreadyStalledWith(pkg.Status.Conditions, status.ImpersonationFailedReason)
			status.MarkStalled(pkg, status.ImpersonationFailedReason, "%s", err)
			if emit {
				params.EventRecorder.Eventf(pkg, nil, corev1.EventTypeWarning,
					status.ImpersonationFailedReason, "Delete", "%s", err)
			}
			if patchErr := patchModulePackageDeletionStatus(ctx, patcher, pkg); patchErr != nil {
				log.Error(patchErr, "Failed to patch ModulePackage status on Forbidden deletion prune")
			}
			return ctrl.Result{RequeueAfter: StalledRecheckInterval}, nil
		}
		log.Error(err, "Partial failure during deletion cleanup, retaining finalizer")
		return ctrl.Result{}, err
	}
	log.Info("Deletion cleanup pruned resources",
		"deleted", pruneResult.Deleted, "skipped", pruneResult.Skipped)

	if err := removeModulePackageFinalizer(ctx, params.Client, pkg); err != nil {
		return ctrl.Result{}, fmt.Errorf("removing finalizer: %w", err)
	}
	log.Info("Finalizer removed, deletion can proceed")
	return ctrl.Result{}, nil
}

func handleModulePackageDeletionImpersonationFailure(
	ctx context.Context,
	params *ModulePackageParams,
	patcher *patch.SerialPatcher,
	pkg *releasesv1alpha1.ModulePackage,
	effectiveSA, source string,
	impErr error,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if apply.IsServiceAccountNotFound(impErr) {
		if pkg.GetAnnotations()[releasesv1alpha1.AnnotationForceDeleteOrphan] == "true" {
			orphanCount := int64(len(pkg.Status.Inventory.Entries))
			log.Info("Orphaning inventory and removing finalizer at operator request",
				"serviceAccount", effectiveSA,
				"serviceAccountSource", source,
				"inventoryCount", orphanCount)
			params.EventRecorder.Eventf(pkg, nil, corev1.EventTypeWarning,
				status.OrphanedOnDeletionReason, "Delete",
				"Orphaned %d managed resources; ServiceAccount %q missing and %s annotation set",
				orphanCount, effectiveSA, releasesv1alpha1.AnnotationForceDeleteOrphan)
			pkg.Status.Inventory = nil
			if err := patchModulePackageDeletionStatus(ctx, patcher, pkg); err != nil {
				log.Error(err, "Failed to patch ModulePackage status on orphan-exit")
			}
			if err := removeModulePackageFinalizer(ctx, params.Client, pkg); err != nil {
				return ctrl.Result{}, fmt.Errorf("removing finalizer: %w", err)
			}
			log.Info("Finalizer removed, deletion can proceed")
			return ctrl.Result{}, nil
		}

		log.Error(impErr, "Impersonation ServiceAccount missing during deletion; ModulePackage stalled pending operator action",
			"serviceAccount", effectiveSA,
			"serviceAccountSource", source,
			"annotation", releasesv1alpha1.AnnotationForceDeleteOrphan)
		msg := deletionSAMissingMessage(pkg.Namespace, effectiveSA)
		emit := !readyAlreadyStalledWith(pkg.Status.Conditions, status.DeletionSAMissingReason)
		status.MarkStalled(pkg, status.DeletionSAMissingReason, "%s", msg)
		if emit {
			params.EventRecorder.Eventf(pkg, nil, corev1.EventTypeWarning,
				status.DeletionSAMissingReason, "Delete", "%s", msg)
		}
		if err := patchModulePackageDeletionStatus(ctx, patcher, pkg); err != nil {
			log.Error(err, "Failed to patch ModulePackage status on DeletionSAMissing stall")
		}
		return ctrl.Result{RequeueAfter: StalledRecheckInterval}, nil
	}

	log.Error(impErr, "Impersonation failed during deletion cleanup",
		"serviceAccount", effectiveSA,
		"serviceAccountSource", source)
	emit := !readyAlreadyStalledWith(pkg.Status.Conditions, status.ImpersonationFailedReason)
	status.MarkStalled(pkg, status.ImpersonationFailedReason, "%s", impErr)
	if emit {
		params.EventRecorder.Eventf(pkg, nil, corev1.EventTypeWarning,
			status.ImpersonationFailedReason, "Delete", "%s", impErr)
	}
	if err := patchModulePackageDeletionStatus(ctx, patcher, pkg); err != nil {
		log.Error(err, "Failed to patch ModulePackage status on ImpersonationFailed stall")
	}
	return ctrl.Result{RequeueAfter: StalledRecheckInterval}, nil
}

func patchModulePackageDeletionStatus(ctx context.Context, patcher *patch.SerialPatcher, pkg *releasesv1alpha1.ModulePackage) error {
	return patcher.Patch(ctx, pkg,
		patch.WithOwnedConditions{
			Conditions: []string{
				status.ReadyCondition,
				status.ReconcilingCondition,
				status.StalledCondition,
				status.DriftedCondition,
			},
		},
	)
}

// buildModulePackageApplyClient returns the ResourceManager and client to use for
// apply and prune. Resolution order for the impersonation target:
//  1. spec.serviceAccountName (explicit, wins)
//  2. params.DefaultServiceAccount (the manager's --default-service-account flag)
//  3. empty → fall back to the controller's own client
//
// The effective SA is always resolved in the ModulePackage's own namespace.
func buildModulePackageApplyClient(
	ctx context.Context,
	params *ModulePackageParams,
	pkg *releasesv1alpha1.ModulePackage,
) (*fluxssa.ResourceManager, client.Client, error) {
	effectiveSA, source := resolveEffectiveSA(pkg.Spec.ServiceAccountName, params.DefaultServiceAccount)
	if effectiveSA == "" {
		return params.ResourceManager, params.Client, nil
	}
	log := logf.FromContext(ctx)
	log.Info("Building impersonated client",
		"serviceAccount", effectiveSA,
		"serviceAccountSource", source)
	impClient, err := apply.NewImpersonatedClient(ctx, params.RestConfig, params.APIReader, params.Client.Scheme(), pkg.Namespace, effectiveSA)
	if err != nil {
		return nil, nil, err
	}
	return apply.NewResourceManager(impClient, "opm-controller"), impClient, nil
}

func inventoryDigestModulePackage(inv *releasesv1alpha1.Inventory) string {
	if inv == nil {
		return ""
	}
	return inv.Digest
}

func isResolutionErrorMsg(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "loading synthesized instance") ||
		strings.Contains(msg, "loading package") ||
		strings.Contains(msg, "resolving")
}
