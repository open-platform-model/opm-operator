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
	"github.com/open-platform-model/opm-operator/pkg/provider"
)

// DefaultReleaseInterval is the fallback requeue interval when spec.interval
// is not set.
const DefaultReleaseInterval = 5 * time.Minute

// ReleaseParams holds the dependencies for the Release reconcile loop.
type ReleaseParams struct {
	Client client.Client
	// APIReader is an uncached reader used for one-off reads (e.g. ServiceAccount
	// existence checks for impersonation) that should not provision a cache informer.
	APIReader       client.Reader
	RestConfig      *rest.Config
	Provider        *provider.Provider
	ResourceManager *fluxssa.ResourceManager
	EventRecorder   events.EventRecorder

	// Fetcher downloads Flux source artifacts. Typically
	// &opmsource.ArtifactFetcher{} in production; tests inject a stub.
	Fetcher opmsource.Fetcher

	// Renderer loads and renders a CUE release package from a local directory.
	// Production wires render.PackageReleaseRenderer; tests inject a stub.
	Renderer render.ReleaseRenderer

	// DefaultServiceAccount is the fallback SA name used when a Release has
	// an empty spec.serviceAccountName. Empty disables the default and
	// preserves the controller-client fallback.
	DefaultServiceAccount string
}

// ReconcileRelease runs the full Release reconcile loop: source resolution,
// artifact fetch, path navigation, CUE load, kind detection, render, apply,
// prune, and status commit. Mirrors the ModuleRelease loop but sources the
// CUE package from a Flux artifact instead of synthesizing it.
func ReconcileRelease(
	ctx context.Context,
	params *ReleaseParams,
	req ctrl.Request,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var rel releasesv1alpha1.Release
	if err := params.Client.Get(ctx, req.NamespacedName, &rel); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	reconcileStart := time.Now()
	interval := rel.Spec.Interval.Duration
	if interval == 0 {
		interval = DefaultReleaseInterval
	}

	// Finalizer patches don't bump .metadata.generation, so
	// GenerationChangedPredicate filters the subsequent UPDATE event —
	// explicit Requeue re-enters the workqueue.
	if !controllerutil.ContainsFinalizer(&rel, FinalizerName) {
		log.Info("Adding finalizer to Release")
		if err := addReleaseFinalizer(ctx, params.Client, &rel); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
		return ctrl.Result{Requeue: true}, nil
	}

	if !rel.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, handleReleaseDeletion(ctx, params, &rel)
	}

	patcher := patch.NewSerialPatcher(&rel, params.Client)

	if rel.Spec.Suspend {
		log.Info("Reconciliation is suspended")
		status.MarkSuspended(&rel)
		rel.Status.ObservedGeneration = rel.Generation
		params.EventRecorder.Eventf(&rel, nil, corev1.EventTypeNormal, status.SuspendedReason, "Suspend", "Reconciliation is suspended")
		if err := patchReleaseStatus(ctx, patcher, &rel); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	if ready := apimeta.FindStatusCondition(rel.Status.Conditions, status.ReadyCondition); ready != nil && ready.Reason == status.SuspendedReason {
		log.Info("Reconciliation resumed")
		params.EventRecorder.Eventf(&rel, nil, corev1.EventTypeNormal, status.ResumedReason, "Resume", "Reconciliation resumed")
	}

	// Check dependsOn before any other work.
	if blocker, checkErr := checkDependsOn(ctx, params.Client, &rel); checkErr != nil {
		status.MarkNotReady(&rel, status.DependenciesNotReadyReason, "%s", checkErr)
		params.EventRecorder.Eventf(&rel, nil, corev1.EventTypeWarning, status.DependenciesNotReadyReason, "DependsOn", "%s", checkErr)
		rel.Status.ObservedGeneration = rel.Generation
		_ = patchReleaseStatus(ctx, patcher, &rel)
		return ctrl.Result{RequeueAfter: interval}, nil
	} else if blocker != "" {
		msg := fmt.Sprintf("waiting for dependency %s", blocker)
		status.MarkNotReady(&rel, status.DependenciesNotReadyReason, "%s", msg)
		params.EventRecorder.Eventf(&rel, nil, corev1.EventTypeNormal, status.DependenciesNotReadyReason, "DependsOn", "%s", msg)
		rel.Status.ObservedGeneration = rel.Generation
		_ = patchReleaseStatus(ctx, patcher, &rel)
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
		rel.Status.ObservedGeneration = rel.Generation

		if outcome == NoOp {
			status.MarkReady(&rel, "Reconciliation succeeded")
			updateReleaseFailureCounters(&rel.Status, outcome, phases)
			rel.Status.NextRetryAt = nil
			if err := patchReleaseStatus(ctx, patcher, &rel); err != nil {
				log.Error(err, "Failed to patch NoOp status")
			}
			return
		}

		rel.Status.LastAttemptedAction = "reconcile"
		rel.Status.LastAttemptedAt = &now
		duration := metav1.Duration{Duration: time.Since(reconcileStart)}
		rel.Status.LastAttemptedDuration = &duration
		rel.Status.LastAttemptedSourceDigest = digests.Source
		rel.Status.LastAttemptedConfigDigest = digests.Config
		rel.Status.LastAttemptedRenderDigest = digests.Render

		if reconciled {
			rel.Status.LastAppliedAt = &now
			rel.Status.LastAppliedSourceDigest = digests.Source
			rel.Status.LastAppliedConfigDigest = digests.Config
			rel.Status.LastAppliedRenderDigest = digests.Render

			invDigest := inventory.ComputeDigest(newEntries)
			rev := int64(1)
			if rel.Status.Inventory != nil {
				rev = rel.Status.Inventory.Revision + 1
			}
			rel.Status.Inventory = &releasesv1alpha1.Inventory{
				Revision: rev,
				Digest:   invDigest,
				Count:    int64(len(newEntries)),
				Entries:  newEntries,
			}
			digests.Inventory = invDigest

			status.RecordReleaseHistory(&rel.Status, status.NewSuccessEntry("reconcile", "complete", digests, int64(len(newEntries))))
		} else if errMsg != "" {
			status.RecordReleaseHistory(&rel.Status, status.NewFailureEntry("reconcile", errMsg, digests))
		}

		updateReleaseFailureCounters(&rel.Status, outcome, phases)

		if retryAfter > 0 {
			t := metav1.NewTime(time.Now().Add(retryAfter))
			rel.Status.NextRetryAt = &t
		} else {
			rel.Status.NextRetryAt = nil
		}

		if err := patchReleaseStatus(ctx, patcher, &rel); err != nil {
			log.Error(err, "Failed to patch Release status")
		}
	}()

	status.MarkReconciling(&rel, "Progressing", "Reconciliation in progress")

	applyFail := func(fail *phaseFail) {
		outcome = fail.outcome
		errMsg = fail.errMsg
		retryAfter = fail.retryAfter
	}

	// Phase 1: resolve source.
	artifactRef, fail := resolveReleaseSource(ctx, params, &rel, interval)
	if fail != nil {
		applyFail(fail)
		return ctrl.Result{RequeueAfter: retryAfter}, nil
	}
	rel.Status.Source = &releasesv1alpha1.SourceStatus{
		Ref:              &rel.Spec.SourceRef,
		ArtifactRevision: artifactRef.Revision,
		ArtifactDigest:   artifactRef.Digest,
		ArtifactURL:      artifactRef.URL,
	}
	digests.Source = artifactRef.Digest

	// Phase 2: fetch + extract artifact.
	extractDir, fail := fetchReleaseArtifact(ctx, params, &rel, artifactRef, interval)
	if fail != nil {
		applyFail(fail)
		return ctrl.Result{RequeueAfter: retryAfter}, nil
	}
	defer func() { _ = os.RemoveAll(extractDir) }()

	// Phase 3: navigate to spec.path.
	packageDir, fail := navigateReleasePath(&rel, extractDir, params.EventRecorder)
	if fail != nil {
		applyFail(fail)
		return ctrl.Result{RequeueAfter: retryAfter}, nil
	}

	// Phase 4+5: load CUE, detect kind, render.
	renderResult, fail := renderReleasePackage(ctx, params, &rel, packageDir)
	if fail != nil {
		applyFail(fail)
		return ctrl.Result{RequeueAfter: retryAfter}, nil
	}

	if fail := computeReleaseDigests(&rel, renderResult, &digests); fail != nil {
		applyFail(fail)
		return ctrl.Result{RequeueAfter: retryAfter}, nil
	}

	lastApplied := status.DigestSet{
		Source:    rel.Status.LastAppliedSourceDigest,
		Config:    rel.Status.LastAppliedConfigDigest,
		Render:    rel.Status.LastAppliedRenderDigest,
		Inventory: inventoryDigestRelease(rel.Status.Inventory),
	}
	if status.IsNoOp(digests, lastApplied) {
		log.Info("No changes detected, skipping apply")
		params.EventRecorder.Eventf(&rel, nil, corev1.EventTypeNormal, status.NoOpReason, "Reconcile", "No changes detected")
		outcome = NoOp
		return ctrl.Result{RequeueAfter: interval}, nil
	}

	applyedResult, fail := applyAndPruneRelease(ctx, params, &rel, renderResult, &phases)
	if fail != nil {
		applyFail(fail)
		return ctrl.Result{RequeueAfter: retryAfter}, nil
	}

	outcome = applyedResult.outcome
	newEntries = applyedResult.entries
	reconciled = true
	status.MarkReady(&rel, "Reconciliation succeeded")
	params.EventRecorder.Eventf(&rel, nil, corev1.EventTypeNormal, status.ReconciliationSucceededReason, "Reconcile", "Reconciliation succeeded")
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

func resolveReleaseSource(
	ctx context.Context,
	params *ReleaseParams,
	rel *releasesv1alpha1.Release,
	interval time.Duration,
) (*opmsource.ArtifactRef, *phaseFail) {
	artifactRef, err := opmsource.Resolve(ctx, params.Client, rel.Spec.SourceRef, rel.Namespace)
	if err == nil {
		return artifactRef, nil
	}
	reason := status.SourceNotReadyReason
	stalled := errors.Is(err, opmsource.ErrSourceNotFound) || errors.Is(err, opmsource.ErrUnsupportedSourceKind)
	params.EventRecorder.Eventf(rel, nil, corev1.EventTypeWarning, reason, "Resolve", "%s", err)
	if stalled {
		status.MarkStalled(rel, reason, "%s", err)
		return nil, &phaseFail{FailedStalled, err.Error(), StalledRecheckInterval}
	}
	status.MarkNotReady(rel, reason, "%s", err)
	return nil, &phaseFail{FailedTransient, err.Error(), interval}
}

func fetchReleaseArtifact(
	ctx context.Context,
	params *ReleaseParams,
	rel *releasesv1alpha1.Release,
	artifactRef *opmsource.ArtifactRef,
	interval time.Duration,
) (string, *phaseFail) {
	extractDir, err := os.MkdirTemp("", "opm-release-artifact-*")
	if err != nil {
		status.MarkNotReady(rel, status.FetchFailedReason, "creating temp dir: %s", err)
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
		params.EventRecorder.Eventf(rel, nil, corev1.EventTypeWarning, status.FetchFailedReason, "Fetch", "%s", err)
		status.MarkNotReady(rel, status.FetchFailedReason, "%s", err)
		return "", &phaseFail{FailedTransient, err.Error(), interval}
	}
	return extractDir, nil
}

func navigateReleasePath(
	rel *releasesv1alpha1.Release,
	extractDir string,
	recorder events.EventRecorder,
) (string, *phaseFail) {
	packageDir, err := resolvePackagePath(extractDir, rel.Spec.Path)
	if err == nil {
		return packageDir, nil
	}
	reason := status.PathNotFoundReason
	if errors.Is(err, errReleaseFileMissing) {
		reason = status.ReleaseFileNotFoundReason
	}
	status.MarkStalled(rel, reason, "%s", err)
	recorder.Eventf(rel, nil, corev1.EventTypeWarning, reason, "Load", "%s", err)
	return "", &phaseFail{FailedStalled, err.Error(), StalledRecheckInterval}
}

func renderReleasePackage(
	ctx context.Context,
	params *ReleaseParams,
	rel *releasesv1alpha1.Release,
	packageDir string,
) (*render.RenderResult, *phaseFail) {
	renderer := params.Renderer
	if renderer == nil {
		renderer = render.PackageReleaseRenderer{}
	}
	kind, result, err := renderer.Render(ctx, packageDir, params.Provider)
	if err != nil {
		reason := renderErrorReason(err)
		status.MarkStalled(rel, reason, "%s", err)
		params.EventRecorder.Eventf(rel, nil, corev1.EventTypeWarning, reason, "Render", "%s", err)
		return nil, &phaseFail{FailedStalled, err.Error(), StalledRecheckInterval}
	}
	if kind != render.KindModuleRelease {
		msg := fmt.Sprintf("unexpected release kind %q", kind)
		status.MarkStalled(rel, status.UnsupportedKindReason, "%s", msg)
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

func computeReleaseDigests(
	rel *releasesv1alpha1.Release,
	renderResult *render.RenderResult,
	digests *status.DigestSet,
) *phaseFail {
	renderDigest, err := status.RenderDigest(renderResult.Resources)
	if err != nil {
		status.MarkStalled(rel, status.RenderFailedReason, "computing render digest: %s", err)
		return &phaseFail{FailedStalled, err.Error(), StalledRecheckInterval}
	}
	digests.Render = renderDigest
	digests.Inventory = inventory.ComputeDigest(renderResult.InventoryEntries)
	// Release carries no user values — config digest hashes empty input so
	// NoOp detection stays consistent across reconciles.
	digests.Config = status.ConfigDigest(nil)
	return nil
}

// applyPruneResult captures the outputs of the apply+prune phase.
type applyPruneResult struct {
	outcome Outcome
	entries []releasesv1alpha1.InventoryEntry
}

func applyAndPruneRelease(
	ctx context.Context,
	params *ReleaseParams,
	rel *releasesv1alpha1.Release,
	renderResult *render.RenderResult,
	phases *phaseOutcomes,
) (*applyPruneResult, *phaseFail) {
	log := logf.FromContext(ctx)

	resources, err := toUnstructuredSlice(renderResult.Resources)
	if err != nil {
		status.MarkStalled(rel, status.ApplyFailedReason, "converting resources: %s", err)
		return nil, &phaseFail{FailedStalled, err.Error(), StalledRecheckInterval}
	}

	var previousEntries []releasesv1alpha1.InventoryEntry
	if rel.Status.Inventory != nil {
		previousEntries = rel.Status.Inventory.Entries
	}
	staleSet := inventory.ComputeStaleSet(previousEntries, renderResult.InventoryEntries)

	applyRM, applyClient, impErr := buildReleaseApplyClient(ctx, params, rel)
	if impErr != nil {
		status.MarkStalled(rel, status.ImpersonationFailedReason, "%s", impErr)
		return nil, &phaseFail{FailedStalled, impErr.Error(), StalledRecheckInterval}
	}

	// Apply.
	phases.applyRan = true
	force := rel.Spec.Rollout != nil && rel.Spec.Rollout.ForceConflicts
	applyResult, err := apply.Apply(ctx, applyRM, resources, force)
	if err != nil {
		phases.applyFailed = true
		params.EventRecorder.Eventf(rel, nil, corev1.EventTypeWarning, status.ApplyFailedReason, "Apply", "%s", err)
		status.MarkNotReady(rel, status.ApplyFailedReason, "%s", err)
		return nil, &phaseFail{FailedTransient, err.Error(), releaseBackoff(rel)}
	}
	total := applyResult.Created + applyResult.Updated + applyResult.Unchanged
	params.EventRecorder.Eventf(rel, nil, corev1.EventTypeNormal, status.AppliedReason, "Apply",
		"Applied %d resources (%d created, %d updated, %d unchanged)",
		total, applyResult.Created, applyResult.Updated, applyResult.Unchanged)
	log.Info("Applied resources",
		"created", applyResult.Created, "updated", applyResult.Updated, "unchanged", applyResult.Unchanged)
	status.ClearDrifted(rel)

	// Prune.
	phases.pruneRan = true
	outcome := Applied
	if rel.Spec.Prune && len(staleSet) > 0 {
		// Release does not persist a release UUID on Status; pass empty and rely
		// on the managed-by check in the prune guard.
		pruneResult, pruneErr := apply.Prune(ctx, applyClient, "", staleSet)
		if pruneErr != nil {
			phases.pruneFailed = true
			params.EventRecorder.Eventf(rel, nil, corev1.EventTypeWarning, status.PruneFailedReason, "Prune", "%s", pruneErr)
			status.MarkNotReady(rel, status.PruneFailedReason, "%s", pruneErr)
			return nil, &phaseFail{FailedTransient, pruneErr.Error(), releaseBackoff(rel)}
		}
		if pruneResult.Deleted > 0 {
			params.EventRecorder.Eventf(rel, nil, corev1.EventTypeNormal, status.PrunedReason, "Prune",
				"Pruned %d stale resources", pruneResult.Deleted)
		}
		outcome = AppliedAndPruned
	}

	return &applyPruneResult{outcome: outcome, entries: renderResult.InventoryEntries}, nil
}

func releaseBackoff(rel *releasesv1alpha1.Release) time.Duration {
	count := int64(0)
	if rel.Status.FailureCounters != nil {
		count = rel.Status.FailureCounters.Reconcile
	}
	return ComputeBackoff(count + 1)
}

// errReleaseFileMissing is returned by resolvePackagePath when the target
// directory exists but lacks release.cue.
var errReleaseFileMissing = errors.New("release.cue not found")

// resolvePackagePath joins root + relPath safely and verifies the directory
// contains release.cue. Returns errReleaseFileMissing when the directory
// exists but has no release.cue.
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
	if _, err := os.Stat(filepath.Join(target, "release.cue")); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%w at %q", errReleaseFileMissing, relPath)
		}
		return "", fmt.Errorf("stat release.cue: %w", err)
	}
	return target, nil
}

// checkDependsOn verifies all referenced Release CRs are Ready=True.
// Returns ("", nil) when dependencies satisfied or none declared.
// Returns (name, nil) with the first blocking dependency when not ready.
// Returns ("", err) when a dependency references a different namespace or
// another hard error occurs.
func checkDependsOn(
	ctx context.Context,
	c client.Client,
	rel *releasesv1alpha1.Release,
) (string, error) {
	if len(rel.Spec.DependsOn) == 0 {
		return "", nil
	}
	for _, dep := range rel.Spec.DependsOn {
		if dep.Namespace != "" && dep.Namespace != rel.Namespace {
			return "", fmt.Errorf("dependency %s/%s: cross-namespace dependencies are not supported", dep.Namespace, dep.Name)
		}
		var other releasesv1alpha1.Release
		key := types.NamespacedName{Name: dep.Name, Namespace: rel.Namespace}
		if err := c.Get(ctx, key, &other); err != nil {
			if client.IgnoreNotFound(err) == nil {
				return fmt.Sprintf("%s/%s (not found)", rel.Namespace, dep.Name), nil
			}
			return "", fmt.Errorf("getting dependency %s/%s: %w", rel.Namespace, dep.Name, err)
		}
		ready := apimeta.FindStatusCondition(other.Status.Conditions, status.ReadyCondition)
		if ready == nil || ready.Status != metav1.ConditionTrue {
			return fmt.Sprintf("%s/%s", rel.Namespace, dep.Name), nil
		}
	}
	return "", nil
}

// updateReleaseFailureCounters applies counter increments and resets for a
// Release based on phase outcomes and overall reconcile result.
func updateReleaseFailureCounters(
	rs *releasesv1alpha1.ReleaseStatus,
	outcome Outcome,
	phases phaseOutcomes,
) {
	counters := status.EnsureReleaseCounters(rs)

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

func patchReleaseStatus(ctx context.Context, patcher *patch.SerialPatcher, rel *releasesv1alpha1.Release) error {
	return patcher.Patch(ctx, rel,
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

func addReleaseFinalizer(ctx context.Context, c client.Client, rel *releasesv1alpha1.Release) error {
	mergePatch := client.MergeFrom(rel.DeepCopy())
	controllerutil.AddFinalizer(rel, FinalizerName)
	return c.Patch(ctx, rel, mergePatch)
}

func removeReleaseFinalizer(ctx context.Context, c client.Client, rel *releasesv1alpha1.Release) error {
	mergePatch := client.MergeFrom(rel.DeepCopy())
	controllerutil.RemoveFinalizer(rel, FinalizerName)
	return c.Patch(ctx, rel, mergePatch)
}

func handleReleaseDeletion(ctx context.Context, params *ReleaseParams, rel *releasesv1alpha1.Release) error {
	log := logf.FromContext(ctx)
	log.Info("Running deletion cleanup for Release")

	if rel.Spec.Prune && rel.Status.Inventory != nil && len(rel.Status.Inventory.Entries) > 0 {
		deleteClient := params.Client
		effectiveSA, source := resolveEffectiveSA(rel.Spec.ServiceAccountName, params.DefaultServiceAccount)
		if effectiveSA != "" && params.RestConfig != nil {
			impClient, impErr := apply.NewImpersonatedClient(ctx, params.RestConfig, params.APIReader, params.Client.Scheme(), rel.Namespace, effectiveSA)
			if impErr != nil {
				// Best-effort: deletion must not be blocked indefinitely by
				// a missing or unauthorized SA. Fall back to the controller
				// client so the finalizer can eventually clear.
				log.Info("ServiceAccount unavailable for deletion cleanup, using controller client",
					"serviceAccount", effectiveSA,
					"serviceAccountSource", source,
					"error", impErr)
			} else {
				deleteClient = impClient
			}
		}
		pruneResult, err := apply.Prune(ctx, deleteClient, "", rel.Status.Inventory.Entries)
		if err != nil {
			log.Error(err, "Partial failure during deletion cleanup, retaining finalizer")
			return err
		}
		log.Info("Deletion cleanup pruned resources",
			"deleted", pruneResult.Deleted, "skipped", pruneResult.Skipped)
	} else if !rel.Spec.Prune {
		log.Info("Prune disabled, orphaning managed resources on deletion")
	}

	if err := removeReleaseFinalizer(ctx, params.Client, rel); err != nil {
		return fmt.Errorf("removing finalizer: %w", err)
	}
	log.Info("Finalizer removed, deletion can proceed")
	return nil
}

// buildReleaseApplyClient returns the ResourceManager and client to use for
// apply and prune. Resolution order for the impersonation target:
//  1. spec.serviceAccountName (explicit, wins)
//  2. params.DefaultServiceAccount (the manager's --default-service-account flag)
//  3. empty → fall back to the controller's own client
//
// The effective SA is always resolved in the release's own namespace.
func buildReleaseApplyClient(
	ctx context.Context,
	params *ReleaseParams,
	rel *releasesv1alpha1.Release,
) (*fluxssa.ResourceManager, client.Client, error) {
	effectiveSA, source := resolveEffectiveSA(rel.Spec.ServiceAccountName, params.DefaultServiceAccount)
	if effectiveSA == "" {
		return params.ResourceManager, params.Client, nil
	}
	log := logf.FromContext(ctx)
	log.Info("Building impersonated client",
		"serviceAccount", effectiveSA,
		"serviceAccountSource", source)
	impClient, err := apply.NewImpersonatedClient(ctx, params.RestConfig, params.APIReader, params.Client.Scheme(), rel.Namespace, effectiveSA)
	if err != nil {
		return nil, nil, err
	}
	return apply.NewResourceManager(impClient, "opm-controller"), impClient, nil
}

func inventoryDigestRelease(inv *releasesv1alpha1.Inventory) string {
	if inv == nil {
		return ""
	}
	return inv.Digest
}

func isResolutionErrorMsg(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "loading synthesized release") ||
		strings.Contains(msg, "loading release package") ||
		strings.Contains(msg, "resolving")
}
