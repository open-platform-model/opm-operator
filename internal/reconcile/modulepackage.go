package reconcile

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/fluxcd/pkg/runtime/patch"
	fluxssa "github.com/fluxcd/pkg/ssa"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/apply"
	"github.com/open-platform-model/opm-operator/internal/render"
	opmsource "github.com/open-platform-model/opm-operator/internal/source"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// DefaultModulePackageInterval is the fallback requeue interval when spec.interval
// is not set.
const DefaultModulePackageInterval = 5 * time.Minute

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

	// RenderSlots is the process-wide render pool shared with the
	// ModuleInstance reconciler; every call to Renderer holds one slot until
	// its result is exported for apply. Nil leaves renders unbounded.
	RenderSlots *render.Slots
	// RenderTimeout is the manager's --render-timeout: the longest one render
	// may run once it holds its slot. A render past it is reported as
	// RenderTimedOut and keeps its slot until it returns. Zero disables the
	// deadline and keeps the render on the reconcile's goroutine.
	RenderTimeout time.Duration

	// DefaultServiceAccount is the fallback SA name used when a ModulePackage has
	// an empty spec.serviceAccountName. Empty disables the default and
	// preserves the controller-client fallback.
	DefaultServiceAccount string

	// Warnings remembers each package's last render warnings so RenderWarning
	// events are emitted on transition only. Nil emits every non-empty set.
	Warnings *WarningTracker

	// OperatorVersion and LibraryVersion are the running operator's
	// version.Full() and version.Library(), parts of the render input key.
	// They are injected so tests can set them; an empty one makes every key
	// incomplete, so nothing is recorded and nothing is skipped.
	OperatorVersion string
	LibraryVersion  string

	// DriftRenderInterval is the manager's --drift-render-interval: how long
	// a reconcile whose render inputs are unchanged may skip its render after
	// the render that recorded them. Zero disables the skip and the record
	// of the key on a NoOp.
	DriftRenderInterval time.Duration

	// convert exports a render result for apply. Nil, as in production,
	// means convertRender; tests in this package set it to observe the
	// conversion, for example that it runs while the render slot is held.
	convert func(*render.RenderResult) (*convertedRender, error)
}

// convertFn is the conversion this reconcile uses: convert when a test set
// it, convertRender otherwise.
func (p *ModulePackageParams) convertFn() func(*render.RenderResult) (*convertedRender, error) {
	if p.convert != nil {
		return p.convert
	}
	return convertRender
}

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
		params.Warnings.Forget(keyOf(&pkg))
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

	reportPackageResume(ctx, params.EventRecorder, &pkg)

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

		// skipCommit is set only when the wait for a render slot is cut
		// short: nothing was attempted, so there is nothing to record, and
		// the zero outcome (NoOp) would otherwise report a success. A panic
		// is caught before this flag and the NoOp branch are read.
		skipCommit bool

		// renderSkipped is set when the render inputs are unchanged and the
		// reconcile skips the render. A skip is not an attempt: every
		// condition it read is already final and nothing it could record has
		// moved, so the commit sends no patch (it would only write the
		// transient Reconciling set below). The skip patches its Healthy
		// judgement itself.
		renderSkipped bool

		// renderedVersion is the module version this attempt's render
		// reported, nil until a render result is in hand. A NoOp writes it
		// to lastAppliedVersion only when it is set.
		renderedVersion *string

		// renderedInputs is the render input key of this attempt's render,
		// nil until a render result is in hand. A success or a NoOp records
		// it as lastAppliedInputs; a failure or a panic does not.
		renderedInputs *status.RenderInputKey

		// adopted is the number of rendered objects the apply verdict let go
		// in this attempt (adopted by another instance). The message of
		// Ready=True states it.
		adopted int
	)

	// A panic is recovered first: the outcome is still its zero value, NoOp,
	// so without this branch the commit would mark a panicking reconcile
	// Ready. The attempt is recorded as failed and the panic re-raised with
	// its original value, so the controller runtime still logs it, counts it
	// and requeues the object on its rate limiter.
	defer func() {
		if r := recover(); r != nil {
			commitModulePackagePanicStatus(ctx, patcher, &pkg, r, digests, reconcileStart)
			panic(r)
		}
		if skipCommit || renderSkipped {
			return
		}
		now := metav1.Now()
		pkg.Status.ObservedGeneration = pkg.Generation

		if outcome == NoOp {
			status.MarkReady(&pkg, "%s", status.ReadyMessage(readySucceeded, adopted))
			updateModulePackageFailureCounters(&pkg.Status, outcome, phases)
			pkg.Status.NextRetryAt = nil
			recordNoOpVersion(&pkg.Status.LastAppliedVersion, renderedVersion)
			recordInputs(&pkg.Status.LastAppliedInputs, noOpInputs(params.DriftRenderInterval, renderedInputs), now)
			if err := patchModulePackageStatus(ctx, patcher, &pkg); err != nil {
				log.Error(err, "Failed to patch NoOp status")
			}
			return
		}

		pkg.Status.LastAttemptedAction = reconcileAction
		pkg.Status.LastAttemptedAt = &now
		duration := metav1.Duration{Duration: time.Since(reconcileStart)}
		pkg.Status.LastAttemptedDuration = &duration
		pkg.Status.LastAttemptedSourceDigest = digests.Source
		pkg.Status.LastAttemptedConfigDigest = digests.Config
		pkg.Status.LastAttemptedRenderDigest = digests.Render

		if reconciled {
			pkg.Status.LastAppliedAt = &now
			pkg.Status.LastAppliedSourceDigest = digests.Source
			pkg.Status.LastAppliedVersion = appliedVersion(renderedVersion)
			pkg.Status.LastAppliedConfigDigest = digests.Config
			pkg.Status.LastAppliedRenderDigest = digests.Render
			recordInputs(&pkg.Status.LastAppliedInputs, renderedInputs, now)

			// The digest stays that of the rendered set: the entries leave
			// out the objects another instance adopted, and the next no-op
			// check compares this digest with the next render.
			pkg.Status.Inventory = nextInventory(pkg.Status.Inventory, newEntries)
			pkg.Status.Inventory.Digest = digests.Inventory
			// The apply and the prune succeeded: no object of the earlier
			// identity is left to judge. The only place that clears it.
			pkg.Status.PreviousInstanceUUID = ""
			digests.Inventory = pkg.Status.Inventory.Digest

			status.RecordModulePackageHistory(&pkg.Status, status.NewSuccessEntry(reconcileAction, "complete", digests, int64(len(newEntries))))
		} else if errMsg != "" {
			status.RecordModulePackageHistory(&pkg.Status, status.NewFailureEntry(reconcileAction, errMsg, digests))
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

	// The skip below reads the conditions as the last attempt left them, not
	// the transient Reconciling this attempt sets now.
	conditionsAtStart := slices.Clone(pkg.Status.Conditions)
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
	resolved := &releasesv1alpha1.SourceStatus{
		Ref:              &pkg.Spec.SourceRef,
		ArtifactRevision: artifactRef.Revision,
		ArtifactDigest:   artifactRef.Digest,
		ArtifactURL:      artifactRef.URL,
	}
	// The key carries the artifact digest, not the revision: a revision that
	// moves without moving the digest must still render once, so the NoOp
	// patch records it in status.source. Compared before the overwrite.
	sourceRecorded := apiequality.Semantic.DeepEqual(pkg.Status.Source, resolved)
	pkg.Status.Source = resolved
	digests.Source = artifactRef.Digest

	// Skip the render, and the artifact fetch before it, when the inputs are
	// unchanged.
	if sourceRecorded && packageRenderSkippable(ctx, params, &pkg, conditionsAtStart, digests.Source) {
		renderSkipped = true
		// The transient Reconciling this attempt set is never written: the
		// skip patches the Healthy condition alone.
		pkg.Status.Conditions = conditionsAtStart
		return judgeSkippedPackage(ctx, params, patcher, &pkg, interval), nil
	}

	// Phase 2: fetch + extract artifact.
	extractDir, fail := fetchModulePackageArtifact(ctx, params, &pkg, artifactRef, interval)
	if fail != nil {
		applyFail(fail)
		return ctrl.Result{RequeueAfter: retryAfter}, nil
	}

	// Phase 3: navigate to spec.path. Phase 4+5: load CUE, detect kind,
	// render. extractDir is handed over here: the render may outlive this
	// reconcile, so it removes the directory when it is done with it.
	converted, fail, waitErr := renderExtractedPackage(ctx, params, &pkg, extractDir, interval)
	if waitErr != nil {
		skipCommit = true
		return ctrl.Result{}, waitErr
	}
	if fail != nil {
		applyFail(fail)
		return ctrl.Result{RequeueAfter: retryAfter}, nil
	}

	computeModulePackageDigests(converted, &digests)
	renderedVersion = &converted.result.ModuleVersion
	key := renderedKey(digests.Source, digests.Config, converted.result, params.OperatorVersion, params.LibraryVersion)
	renderedInputs = &key

	// Decide what this render's identity means for the recorded ones, as a
	// ModuleInstance does. A third identity while an earlier change is not
	// settled is refused before anything is applied or pruned.
	renderedUUID := extractInstanceUUID(converted.resources)
	identities := planIdentities(pkg.Status.InstanceUUID, pkg.Status.PreviousInstanceUUID, renderedUUID, true)
	if identities.Refused {
		already := readyAlreadyStalledWith(conditionsAtStart, status.IdentityChangeUnsettledReason)
		msg := refuseIdentityChange(params.EventRecorder, &pkg, already,
			pkg.Status.InstanceUUID, pkg.Status.PreviousInstanceUUID, renderedUUID)
		applyFail(&phaseFail{FailedStalled, msg, StalledRecheckInterval})
		return ctrl.Result{RequeueAfter: retryAfter}, nil
	}

	lastApplied := status.DigestSet{
		Source:    pkg.Status.LastAppliedSourceDigest,
		Config:    pkg.Status.LastAppliedConfigDigest,
		Render:    pkg.Status.LastAppliedRenderDigest,
		Inventory: inventoryDigestModulePackage(pkg.Status.Inventory),
	}
	// An identity change that is not settled is never a NoOp.
	digestsUnchanged := identities.keepsNoOp(status.IsNoOp(digests, lastApplied))

	// The apply guard (0012:D8:R1, R4), before the no-op decision and before
	// the first write: a package has no drift check to carry a verdict that
	// could not be asked, so a reconcile that renders and cannot judge its
	// objects fails, also when every digest matches.
	guarded, fail := guardModulePackageApply(ctx, params, &pkg, converted.resources,
		identities.InstanceUUID, adoptedBefore(conditionsAtStart), digestsUnchanged, &phases)
	if fail != nil {
		applyFail(fail)
		return ctrl.Result{RequeueAfter: retryAfter}, nil
	}
	guard := guarded.guard
	adopted = guard.adopted

	// Matching digests are a no-op unless the verdict changes what the
	// package holds: an object to take in, or an inventoried object another
	// instance adopted. Then the reconcile applies what the verdict allows
	// and records the inventory.
	if digestsUnchanged && !guard.changesInventory() {
		log.Info("No changes detected, skipping apply")
		params.EventRecorder.Eventf(&pkg, nil, corev1.EventTypeNormal, status.NoOpReason, "Reconcile", "No changes detected")
		outcome = NoOp
		identities.fillEmpty(&pkg.Status.InstanceUUID)
		reportAdoptedElsewhere(params.EventRecorder, &pkg, guard.letGo, adoptedBefore(conditionsAtStart))
		// Judged before the deferred NoOp commit, which patches it.
		v := judgePackageHealth(ctx, params, &pkg, inventoryEntries(pkg.Status.Inventory))
		applyHealth(&pkg, v)
		return ctrl.Result{RequeueAfter: packageRequeue(healthRequeue(v, pkg.Status.LastAppliedAt, time.Now()), interval)}, nil
	}

	applyedResult, fail := applyAndPruneModulePackage(ctx, params, patcher, &pkg, converted, identities, guarded, &phases)
	if fail != nil {
		applyFail(fail)
		return ctrl.Result{RequeueAfter: retryAfter}, nil
	}

	outcome = applyedResult.outcome
	newEntries = applyedResult.entries
	reconciled = true
	status.MarkReady(&pkg, "%s", status.ReadyMessage(readySucceeded, adopted))
	params.EventRecorder.Eventf(&pkg, nil, corev1.EventTypeNormal, status.ReconciliationSucceededReason, "Reconcile", readySucceeded)
	reportAdoptedElsewhere(params.EventRecorder, &pkg, guard.letGo, adoptedBefore(conditionsAtStart))
	log.Info("Reconciliation complete", "outcome", outcome.String())

	// Judge health after the apply and prune returned, through the identity
	// that applied. lastAppliedAt is about to be written as now.
	v := judgeHealth(ctx, applyedResult.healthReader, newEntries)
	applyHealth(&pkg, v)
	now := metav1.Now()
	return ctrl.Result{RequeueAfter: packageRequeue(healthRequeue(v, &now, now.Time), interval)}, nil
}

// judgePackageHealth is judgeInstanceHealth for a ModulePackage.
func judgePackageHealth(
	ctx context.Context,
	params *ModulePackageParams,
	pkg *releasesv1alpha1.ModulePackage,
	entries []releasesv1alpha1.InventoryEntry,
) healthVerdict {
	if sa, _ := resolveEffectiveSA(pkg.Spec.ServiceAccountName, params.DefaultServiceAccount); sa == "" {
		return judgeHealth(ctx, managerReader(params.APIReader, params.Client), entries)
	}
	_, impClient, err := buildModulePackageApplyClient(ctx, params, pkg)
	if err != nil {
		return unreadableVerdict(entries, err)
	}
	return judgeHealth(ctx, impClient, entries)
}

// judgeSkippedPackage is judgeSkippedInstance for a ModulePackage: it
// patches the Healthy condition alone, and requeues after spec.interval or
// sooner when health asks for it.
func judgeSkippedPackage(
	ctx context.Context,
	params *ModulePackageParams,
	patcher *patch.SerialPatcher,
	pkg *releasesv1alpha1.ModulePackage,
	interval time.Duration,
) ctrl.Result {
	v := judgePackageHealth(ctx, params, pkg, inventoryEntries(pkg.Status.Inventory))
	applyHealth(pkg, v)
	if err := patcher.Patch(ctx, pkg,
		patch.WithOwnedConditions{Conditions: []string{status.HealthyCondition}},
	); err != nil {
		logf.FromContext(ctx).Error(err, "Failed to patch the Healthy condition of a skipped render")
	}
	return ctrl.Result{RequeueAfter: packageRequeue(healthRequeue(v, pkg.Status.LastAppliedAt, time.Now()), interval)}
}

// packageRenderSkippable reports whether this reconcile of pkg may skip its
// render because the render input key computed from the resolved artifact
// digest, the cluster Platform and the running versions matches
// status.lastAppliedInputs, under the conditions of renderSkip.maySkip. A
// ModulePackage carries no values, so its config part is ConfigDigest(nil).
// conditions are the package's conditions before this attempt marked itself
// Reconciling.
// It logs the skip.
func packageRenderSkippable(
	ctx context.Context,
	params *ModulePackageParams,
	pkg *releasesv1alpha1.ModulePackage,
	conditions []metav1.Condition,
	sourceDigest string,
) bool {
	if params.DriftRenderInterval <= 0 || pkg.Status.LastAppliedInputs == nil {
		return false
	}
	key := func() status.RenderInputKey {
		identity, skew := platformKeyParts(ctx, params.Client)
		return status.RenderInputKey{
			Source:          sourceDigest,
			Config:          status.ConfigDigest(nil),
			PackageIdentity: identity,
			SkewPolicy:      skew,
			OperatorVersion: params.OperatorVersion,
			LibraryVersion:  params.LibraryVersion,
		}
	}
	skip := renderSkip{interval: params.DriftRenderInterval, now: time.Now()}
	if !skip.maySkip(conditions, pkg.Generation, pkg.Status.ObservedGeneration, pkg.Status.LastAppliedInputs, key) {
		return false
	}
	logRenderSkip(ctx, pkg.Status.LastAppliedInputs, params.DriftRenderInterval)
	return true
}

// commitModulePackagePanicStatus is the ModulePackage twin of
// commitPanicStatus: Ready=False with reason ReconcilePanic, a failure history
// entry, one more reconcile failure with the phase counters left alone,
// nextRetryAt cleared, and lastApplied* and the inventory kept. The
// ModulePackage commit records no reconcile metrics.
func commitModulePackagePanicStatus(
	ctx context.Context,
	patcher *patch.SerialPatcher,
	pkg *releasesv1alpha1.ModulePackage,
	recovered any,
	digests status.DigestSet,
	reconcileStart time.Time,
) {
	msg := fmt.Sprintf("reconcile panicked: %v", recovered)
	// The controller runtime's logger already carries the object's name and
	// namespace, and its panic handler logs the stack after the re-panic.
	logf.FromContext(ctx).Error(errors.New(msg), "Recording the panicking reconcile as failed")

	status.MarkReconcilePanic(pkg, "%s", msg)
	now := metav1.Now()
	pkg.Status.ObservedGeneration = pkg.Generation
	pkg.Status.LastAttemptedAction = reconcileAction
	pkg.Status.LastAttemptedAt = &now
	duration := metav1.Duration{Duration: time.Since(reconcileStart)}
	pkg.Status.LastAttemptedDuration = &duration
	pkg.Status.LastAttemptedSourceDigest = digests.Source
	pkg.Status.LastAttemptedConfigDigest = digests.Config
	pkg.Status.LastAttemptedRenderDigest = digests.Render
	status.RecordModulePackageHistory(&pkg.Status, status.NewFailureEntry(reconcileAction, msg, digests))
	updateModulePackageFailureCounters(&pkg.Status, FailedTransient, phaseOutcomes{})
	pkg.Status.NextRetryAt = nil

	if err := patchModulePackageStatus(ctx, patcher, pkg); err != nil {
		logf.FromContext(ctx).Error(err, "Failed to patch ModulePackage status after a panic")
	}
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

// renderExtractedPackage navigates to spec.path inside extractDir and
// renders the package there. It owns extractDir: it removes it when the
// navigation fails, and renderModulePackage removes it otherwise.
func renderExtractedPackage(
	ctx context.Context,
	params *ModulePackageParams,
	pkg *releasesv1alpha1.ModulePackage,
	extractDir string,
	interval time.Duration,
) (*convertedRender, *phaseFail, error) {
	packageDir, fail := navigateModulePackagePath(pkg, extractDir, params.EventRecorder)
	if fail != nil {
		_ = os.RemoveAll(extractDir)
		return nil, fail, nil
	}
	return renderModulePackage(ctx, params, pkg, extractDir, packageDir, interval)
}

// renderModulePackage renders the package at packageDir and classifies a
// failure. It owns extractDir, the artifact packageDir lies in, and removes
// it once the render is done with it, which may be after this function
// returns when the render timed out. A render that did not finish within
// RenderTimeout is a non-stalled RenderTimedOut on the bounded backoff,
// classified before anything else; PlatformNotReady is a non-stalled wait; a
// registry fetch failure the library typed (IsTransientFailure), in the
// package load or the render build, retries on the bounded backoff as a non-stalled ResolutionFailed;
// every other failure stalls on StalledRecheckInterval with the reason of
// renderErrorReason. An author defect in the package (a CUE syntax error,
// values that conflict with #config, non-concrete values) stalls, because no
// retry fixes it; a new artifact revision re-triggers the reconcile.
func renderModulePackage(
	ctx context.Context,
	params *ModulePackageParams,
	pkg *releasesv1alpha1.ModulePackage,
	extractDir, packageDir string,
	interval time.Duration,
) (*convertedRender, *phaseFail, error) {
	// The render holds one slot of the process-wide pool until its result is
	// exported for apply: the rendered CUE values pin the whole build until
	// then, and the export is where the heap peaks.
	var (
		kind      string
		result    *render.RenderResult
		converted *convertedRender
		err       error
	)
	convert := params.convertFn()
	waitErr := params.RenderSlots.Run(ctx, renderKey("ModulePackage", pkg.Namespace, pkg.Name), params.RenderTimeout, func(renderCtx context.Context) {
		// First, so the directory goes on a panic too.
		defer func() { _ = os.RemoveAll(extractDir) }()
		kind, result, err = params.Renderer.Render(renderCtx, packageDir)
		if err == nil && kind == render.KindModuleInstance {
			converted, err = convert(result)
		}
	})
	if waitErr != nil && !errors.Is(waitErr, render.ErrRenderTimedOut) {
		// The body was never called, so it did not take the directory.
		_ = os.RemoveAll(extractDir)
	}
	if waitErr != nil {
		// A render timeout is classified before anything else, and the
		// closure's variables are never read: the render may still be
		// writing them.
		if msg, ok := renderTimeoutMessage(waitErr, params.RenderTimeout); ok {
			logf.FromContext(ctx).Info("Render timed out", "timeout", params.RenderTimeout.String(), "reason", waitErr.Error())
			status.MarkRenderTimedOut(pkg, "%s", msg)
			params.EventRecorder.Eventf(pkg, nil, corev1.EventTypeWarning, status.RenderTimedOutReason, "Render", "%s", msg)
			return nil, &phaseFail{FailedTransient, msg, modulePackageBackoff(pkg)}, nil
		}
		// The context ended while waiting for a slot (manager shutdown).
		// Nothing was rendered: the caller returns this error as is and
		// commits nothing, and no classifier sees it.
		return nil, nil, fmt.Errorf("waiting for a render slot: %w", waitErr)
	}
	var convErr *conversionError
	if errors.As(err, &convErr) {
		reportRenderDiagnostics(ctx, params.Warnings, params.EventRecorder, pkg, result)
		status.MarkStalled(pkg, convErr.reason, "%s", convErr)
		return nil, &phaseFail{FailedStalled, convErr.Error(), StalledRecheckInterval}, nil
	}
	if err != nil {
		// PlatformNotReady is a blocked-on-dependency state, not a stall: the
		// store holds no generated platform module yet. Mark Ready=False/
		// PlatformNotReady (non-stalled), apply and prune nothing, and requeue.
		// The Platform watch (mapPlatformToModulePackages) re-enqueues promptly
		// when the platform is generated; the interval requeue is the safety net.
		if errors.Is(err, render.ErrPlatformNotReady) {
			status.MarkNotReady(pkg, status.PlatformNotReadyReason, "%s", err)
			params.EventRecorder.Eventf(pkg, nil, corev1.EventTypeWarning, status.PlatformNotReadyReason, "Render", "%s", err)
			return nil, &phaseFail{FailedTransient, err.Error(), interval}, nil
		}
		// A registry fetch failure the library typed (a CUE dependency the
		// registry did not serve), in the package load or the render build,
		// retries on the bounded backoff as a non-stalled ResolutionFailed,
		// the same shared classification the ModuleInstance loop uses. A
		// package load failure the library did not classify is an author
		// defect and stalls below.
		if IsTransientFailure(err) {
			status.MarkNotReady(pkg, status.ResolutionFailedReason, "%s", err)
			params.EventRecorder.Eventf(pkg, nil, corev1.EventTypeWarning, status.ResolutionFailedReason, "Render", "%s", err)
			return nil, &phaseFail{FailedTransient, err.Error(), modulePackageBackoff(pkg)}, nil
		}
		reason := renderErrorReason(err)
		status.MarkStalled(pkg, reason, "%s", err)
		params.EventRecorder.Eventf(pkg, nil, corev1.EventTypeWarning, reason, "Render", "%s", err)
		return nil, &phaseFail{FailedStalled, err.Error(), StalledRecheckInterval}, nil
	}
	if kind != render.KindModuleInstance {
		msg := fmt.Sprintf("unexpected instance kind %q", kind)
		status.MarkStalled(pkg, status.UnsupportedKindReason, "%s", msg)
		return nil, &phaseFail{FailedStalled, msg, StalledRecheckInterval}, nil
	}
	reportRenderDiagnostics(ctx, params.Warnings, params.EventRecorder, pkg, result)
	return converted, nil, nil
}

// renderErrorReason maps a failed package render to its reason: an
// unsupported kind first, then the typed cause (renderFailureReason), which
// reports every acquisition failure as ResolutionFailed.
func renderErrorReason(err error) string {
	if errors.Is(err, render.ErrUnsupportedKind) {
		return status.UnsupportedKindReason
	}
	return renderFailureReason(err)
}

func computeModulePackageDigests(converted *convertedRender, digests *status.DigestSet) {
	digests.Render = converted.digest
	digests.Inventory = inventoryDigestOf(converted.entries)
	// A ModulePackage carries no user values — config digest hashes empty input so
	// NoOp detection stays consistent across reconciles.
	digests.Config = status.ConfigDigest(nil)
}

// applyPruneResult captures the outputs of the apply+prune phase.
type applyPruneResult struct {
	outcome Outcome
	entries []releasesv1alpha1.InventoryEntry
	// healthReader reads as the identity that applied: the impersonated
	// client, or the manager's uncached reader.
	healthReader client.Reader
}

func applyAndPruneModulePackage(
	ctx context.Context,
	params *ModulePackageParams,
	patcher *patch.SerialPatcher,
	pkg *releasesv1alpha1.ModulePackage,
	converted *convertedRender,
	identities identityPlan,
	guarded *packageApply,
	phases *phaseOutcomes,
) (*applyPruneResult, *phaseFail) {
	log := logf.FromContext(ctx)

	// The apply list is what the apply verdict allows of the rendered set.
	// The stale set below is computed from the full render, so an object
	// another instance adopted is never pruned.
	guard := guarded.guard
	resources := guard.allowed

	var previousEntries []releasesv1alpha1.InventoryEntry
	if pkg.Status.Inventory != nil {
		previousEntries = pkg.Status.Inventory.Entries
	}
	staleSet := staleEntries(previousEntries, converted.entries)

	applyRM, applyClient := guarded.rm, guarded.client

	// A changed identity is stored before the first write of the apply: no
	// apply without the record.
	if err := identities.store(&pkg.Status.InstanceUUID, &pkg.Status.PreviousInstanceUUID,
		func() error { return patchModulePackageIdentityStatus(ctx, patcher, pkg) }); err != nil {
		status.MarkNotReady(pkg, status.ApplyFailedReason, "%s", err)
		return nil, &phaseFail{FailedTransient, err.Error(), modulePackageBackoff(pkg)}
	}

	// Apply.
	phases.applyRan = true
	applyResult, err := apply.Apply(ctx, applyRM, resources, apply.ApplyOptions{
		Force:      forcesConflicts(pkg.Spec.Rollout),
		DeleteData: pkg.Spec.DataPolicy.DeletesClaims(),
		TakenIn:    guard.pins,
	})
	if err != nil {
		phases.applyFailed = true
		reason, msg := status.ApplyFailedReason, err.Error()
		if note, kept := claimConflictNote(err); kept {
			reason, msg = status.ClaimConflictReason, note
		}
		params.EventRecorder.Eventf(pkg, nil, corev1.EventTypeWarning, reason, "Apply", "%s", msg)
		status.MarkNotReady(pkg, reason, "%s", msg)
		return nil, &phaseFail{FailedTransient, msg, modulePackageBackoff(pkg)}
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
		pruneResult, pruneErr := apply.Prune(ctx, applyClient, identities.Prune, staleSet,
			apply.PruneOptions{DeleteData: pkg.Spec.DataPolicy.DeletesClaims()})
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
		reportKeptClaims(params.EventRecorder, pkg, "Prune", pruneResult.Kept)
		reportLeftBehind(params.EventRecorder, pkg, "Prune", pruneResult.Left)
		outcome = pruneOutcome(pruneResult)
	}

	sa, _ := resolveEffectiveSA(pkg.Spec.ServiceAccountName, params.DefaultServiceAccount)
	reader := appliedReader(sa, applyClient, params.APIReader, params.Client)
	// An object another instance adopted is in no inventory this package
	// records (0012:D8:R8); it stays in the cluster.
	entries := withoutEntries(converted.entries, judgedObjects(guard.letGo))
	return &applyPruneResult{outcome: outcome, entries: entries, healthReader: reader}, nil
}

// reportPackageResume logs and emits the Resumed event when pkg was suspended
// until this reconcile.
func reportPackageResume(ctx context.Context, recorder events.EventRecorder, pkg *releasesv1alpha1.ModulePackage) {
	ready := apimeta.FindStatusCondition(pkg.Status.Conditions, status.ReadyCondition)
	if ready == nil || ready.Reason != status.SuspendedReason {
		return
	}
	logf.FromContext(ctx).Info("Reconciliation resumed")
	recorder.Eventf(pkg, nil, corev1.EventTypeNormal, status.ResumedReason, "Resume", "Reconciliation resumed")
}

// packageApply is the client that applies a ModulePackage and what the apply
// guard judged through it.
type packageApply struct {
	rm     *fluxssa.ResourceManager
	client client.Client
	guard  guardedApply
}

// guardModulePackageApply builds the client that applies pkg and runs the
// apply guard over the rendered resources with identity, the instance's own
// (guardApply). It returns a failure, with nothing written, when:
//
//   - the client cannot be built: Stalled with ImpersonationFailed;
//   - an object cannot be read, or there is no identity to ask with: Stalled
//     with ImpersonationFailed when the read is Forbidden under an effective
//     ServiceAccount, Ready=False with ApplyFailed on the backoff otherwise;
//   - the verdict refuses an object the reconcile would write, or one that
//     exists outside the inventory: Ready=False with ApplyRefused, not
//     stalled, on the backoff. A package writes every rendered object unless
//     the reconcile is a no-op.
//
// The last two count as a failed apply.
func guardModulePackageApply(
	ctx context.Context,
	params *ModulePackageParams,
	pkg *releasesv1alpha1.ModulePackage,
	resources []*unstructured.Unstructured,
	identity string,
	adoptedAtStart int,
	digestsUnchanged bool,
	phases *phaseOutcomes,
) (*packageApply, *phaseFail) {
	applyRM, applyClient, impErr := buildModulePackageApplyClient(ctx, params, pkg)
	if impErr != nil {
		status.MarkStalled(pkg, status.ImpersonationFailedReason, "%s", impErr)
		return nil, &phaseFail{FailedStalled, impErr.Error(), StalledRecheckInterval}
	}
	sa, _ := resolveEffectiveSA(pkg.Spec.ServiceAccountName, params.DefaultServiceAccount)
	guard := guardApply(ctx, nil, appliedReader(sa, applyClient, params.APIReader, params.Client),
		resources, inventoryEntries(pkg.Status.Inventory), identity, adoptedAtStart)

	if guard.err != nil {
		phases.applyRan, phases.applyFailed = true, true
		params.EventRecorder.Eventf(pkg, nil, corev1.EventTypeWarning, status.ApplyFailedReason, "Apply", "%s", guard.err)
		if sa != "" && isForbidden(guard.err) {
			status.MarkStalled(pkg, status.ImpersonationFailedReason, "%s", guard.err)
			return nil, &phaseFail{FailedStalled, guard.err.Error(), StalledRecheckInterval}
		}
		status.MarkNotReady(pkg, status.ApplyFailedReason, "%s", guard.err)
		return nil, &phaseFail{FailedTransient, guard.err.Error(), modulePackageBackoff(pkg)}
	}
	// A package that applies on matching digests (it takes an object in, or
	// lets an inventoried one go) applies the rendered set: it has no restore
	// step that would leave its inventoried objects alone. So it would write
	// an inventoried object that is being deleted, and that refuses it. Only
	// a reconcile that stays a no-op writes nothing over such an object.
	writesNothing := digestsUnchanged && !guard.changesInventory()
	if refusing := refusedToWrite(guard.refused, writesNothing); len(refusing) > 0 {
		phases.applyRan, phases.applyFailed = true, true
		msg := refuseApply(ctx, params.EventRecorder, pkg, refusing)
		return nil, &phaseFail{FailedTransient, msg, modulePackageBackoff(pkg)}
	}
	return &packageApply{rm: applyRM, client: applyClient, guard: guard}, nil
}

// patchModulePackageIdentityStatus commits the two identity fields before an
// apply, without marking the generation as observed.
func patchModulePackageIdentityStatus(
	ctx context.Context, patcher *patch.SerialPatcher, pkg *releasesv1alpha1.ModulePackage,
) error {
	return patchModulePackageDeletionStatus(ctx, patcher, pkg)
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
				status.HealthyCondition,
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

	// An object that carries either recorded identity is the package's own.
	// With none recorded the verdict is asked with no identity.
	identities := recordedIdentities(pkg.Status.InstanceUUID, pkg.Status.PreviousInstanceUUID)
	pruneResult, err := apply.Prune(ctx, deleteClient, identities, pkg.Status.Inventory.Entries,
		apply.PruneOptions{DeleteData: pkg.Spec.DataPolicy.DeletesClaims()})
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
		"deleted", pruneResult.Deleted, "skipped", pruneResult.Skipped, "keptClaims", len(pruneResult.Kept))
	reportKeptClaims(params.EventRecorder, pkg, "Delete", pruneResult.Kept)
	reportLeftBehind(params.EventRecorder, pkg, "Delete", pruneResult.Left)

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
				status.HealthyCondition,
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
	log.V(1).Info("Building impersonated client",
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
