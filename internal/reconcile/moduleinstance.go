package reconcile

import (
	"context"
	"errors"
	"fmt"
	"time"

	fluxssa "github.com/fluxcd/pkg/ssa"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/fluxcd/pkg/runtime/patch"

	"github.com/open-platform-model/library/opm/k8s/labels"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/apply"
	"github.com/open-platform-model/opm-operator/internal/inventory"
	opmmetrics "github.com/open-platform-model/opm-operator/internal/metrics"
	"github.com/open-platform-model/opm-operator/internal/render"
	"github.com/open-platform-model/opm-operator/internal/shrink"
	"github.com/open-platform-model/opm-operator/internal/status"
)

const (
	// reconcileAction names a reconcile attempt in lastAttemptedAction and
	// in history entries.
	reconcileAction = "reconcile"

	// FinalizerName is the finalizer registered on ModuleInstance resources
	// to ensure owned resources are cleaned up before deletion completes.
	FinalizerName = "opmodel.dev/cleanup"
)

// ModuleInstanceParams holds the dependencies injected into the reconcile loop.
type ModuleInstanceParams struct {
	Client client.Client
	// APIReader is an uncached reader used for one-off reads (e.g. ServiceAccount
	// existence checks for impersonation) that should not provision a cache informer.
	APIReader       client.Reader
	RestConfig      *rest.Config
	ResourceManager *fluxssa.ResourceManager
	EventRecorder   events.EventRecorder
	// Renderer produces the render result for a ModuleInstance. Must be non-nil;
	// production wires render.KernelModuleRenderer, tests wire a stub.
	Renderer render.ModuleRenderer
	// RenderSlots is the process-wide render pool shared with the
	// ModulePackage reconciler; every call to Renderer holds one slot until
	// its result is exported for apply. Nil leaves renders unbounded.
	RenderSlots *render.Slots
	// RenderTimeout is the manager's --render-timeout: the longest one render
	// may run once it holds its slot. A render past it is reported as
	// RenderTimedOut and keeps its slot until it returns. Zero disables the
	// deadline and keeps the render on the reconcile's goroutine.
	RenderTimeout time.Duration
	// DefaultServiceAccount is the fallback SA name used when a
	// ModuleInstance has an empty spec.serviceAccountName. Empty disables
	// the default and preserves the controller-client fallback.
	DefaultServiceAccount string
	// Warnings remembers each instance's last render warnings so RenderWarning
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
func (p *ModuleInstanceParams) convertFn() func(*render.RenderResult) (*convertedRender, error) {
	if p.convert != nil {
		return p.convert
	}
	return convertRender
}

// commitNoOpStatus is the deferred status commit of a NoOp reconcile. Drift
// detection ran and may have set or cleared the Drifted condition, and phase
// counters may need an increment or reset, so they are persisted through a
// bounded patch; lastAttempted, history and inventory are not touched. A
// non-nil renderedInputs is recorded as lastAppliedInputs: the NoOp re-proves
// that the cluster holds what those inputs produce. The caller passes nil
// when the skip is disabled (noOpInputs).
//
// NoOp implies the digests match LastApplied (a previous reconcile applied
// successfully), so Ready=True is the correct state. MarkReconciling at the
// start of this reconcile transiently set Ready=Unknown; it is reset before
// patching.
func commitNoOpStatus(
	ctx context.Context,
	patcher *patch.SerialPatcher,
	mi *releasesv1alpha1.ModuleInstance,
	phases phaseOutcomes,
	renderedVersion *string,
	renderedInputs *status.RenderInputKey,
	reconcileStart time.Time,
) {
	status.MarkReady(mi, "Reconciliation succeeded")
	updateFailureCounters(&mi.Status, NoOp, phases)
	mi.Status.NextRetryAt = nil
	recordNoOpVersion(&mi.Status.LastAppliedVersion, renderedVersion)
	recordInputs(&mi.Status.LastAppliedInputs, renderedInputs, metav1.Now())
	if patchErr := patcher.Patch(ctx, mi,
		patch.WithOwnedConditions{
			Conditions: []string{
				status.ReadyCondition,
				status.ReconcilingCondition,
				status.StalledCondition,
				status.ModuleResolvedCondition,
				status.DriftedCondition,
			},
		},
		patch.WithStatusObservedGeneration{},
	); patchErr != nil {
		logf.FromContext(ctx).Error(patchErr, "Failed to patch NoOp status")
	}
	recordReconcileMetrics(mi.Name, mi.Namespace, NoOp, time.Since(reconcileStart), false, 0)
	opmmetrics.RecordDuration(mi.Name, mi.Namespace, time.Since(reconcileStart))
}

// commitPanicStatus is the deferred status commit of a reconcile that
// panicked. It records the attempt as a transient failure: Ready=False with
// reason ReconcilePanic, a failure history entry and one more reconcile
// failure. The phase counters are left alone: a phase that was in flight when
// the panic hit set its Ran flag but never its Failed flag, so handing over
// the in-flight phases would reset its counter as if it had succeeded.
// nextRetryAt is cleared because the controller runtime's rate limiter, not
// the operator's backoff, schedules the retry. lastApplied* and the inventory
// keep the last success. The caller re-panics afterwards, and the controller
// runtime logs the stack.
func commitPanicStatus(
	ctx context.Context,
	patcher *patch.SerialPatcher,
	mi *releasesv1alpha1.ModuleInstance,
	recovered any,
	digests status.DigestSet,
	reconcileStart time.Time,
) {
	msg := fmt.Sprintf("reconcile panicked: %v", recovered)
	// The controller runtime's logger already carries the object's name and
	// namespace, and its panic handler logs the stack after the re-panic.
	logf.FromContext(ctx).Error(errors.New(msg), "Recording the panicking reconcile as failed")

	status.MarkReconcilePanic(mi, "%s", msg)
	now := metav1.Now()
	mi.Status.ObservedGeneration = mi.Generation
	mi.Status.LastAttemptedAction = reconcileAction
	mi.Status.LastAttemptedAt = &now
	duration := metav1.Duration{Duration: time.Since(reconcileStart)}
	mi.Status.LastAttemptedDuration = &duration
	mi.Status.LastAttemptedSourceDigest = digests.Source
	mi.Status.LastAttemptedConfigDigest = digests.Config
	mi.Status.LastAttemptedRenderDigest = digests.Render
	status.RecordHistory(&mi.Status, status.NewFailureEntry(reconcileAction, msg, digests))
	updateFailureCounters(&mi.Status, FailedTransient, phaseOutcomes{})
	mi.Status.NextRetryAt = nil

	recordReconcileMetrics(mi.Name, mi.Namespace, FailedTransient, time.Since(reconcileStart), false, 0)
	opmmetrics.RecordDuration(mi.Name, mi.Namespace, time.Since(reconcileStart))

	if patchErr := patcher.Patch(ctx, mi,
		patch.WithOwnedConditions{
			Conditions: []string{
				status.ReadyCondition,
				status.ReconcilingCondition,
				status.StalledCondition,
				status.ModuleResolvedCondition,
				status.DriftedCondition,
			},
		},
		patch.WithStatusObservedGeneration{},
	); patchErr != nil {
		logf.FromContext(ctx).Error(patchErr, "Failed to patch ModuleInstance status after a panic")
	}
}

// ReconcileModuleInstance orchestrates all phases of the reconcile loop.
// Phases run sequentially; errors halt progression.
// Status is always patched at the end via deferred function.
func ReconcileModuleInstance(
	ctx context.Context,
	params *ModuleInstanceParams,
	req ctrl.Request,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Phase 0: Load ModuleInstance, check deletion, check suspend, create patch helper.
	var mi releasesv1alpha1.ModuleInstance
	if err := params.Client.Get(ctx, req.NamespacedName, &mi); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Instances the operator does not reconcile: CLI-owned ones and the
	// operator's own. Both are decided before finalizer registration.
	if handled, err := handleNotReconciled(ctx, params, &mi); handled {
		return ctrl.Result{}, err
	}

	// Track reconcile start time for duration calculation.
	// Set after the gates above (which record no metrics) and before the
	// suspend/deletion checks so all operator-managed paths are measured.
	reconcileStart := time.Now()

	// Register finalizer if not present. Finalizer patches don't bump
	// .metadata.generation, so GenerationChangedPredicate filters the
	// subsequent UPDATE event — explicit Requeue re-enters the workqueue.
	if !controllerutil.ContainsFinalizer(&mi, FinalizerName) {
		log.Info("Adding finalizer to ModuleInstance")
		if err := addFinalizer(ctx, params.Client, &mi); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Deletion branch: if DeletionTimestamp is set, run cleanup and return.
	if !mi.DeletionTimestamp.IsZero() {
		params.Warnings.Forget(keyOf(&mi))
		result, err := handleDeletion(ctx, params, &mi)
		opmmetrics.RecordDuration(mi.Name, mi.Namespace, time.Since(reconcileStart))
		return result, err
	}

	// Create serial patcher for status patching.
	patcher := patch.NewSerialPatcher(&mi, params.Client)

	// Suspend check — runs before deferred status commit to preserve existing status fields.
	if mi.Spec.Suspend {
		return ctrl.Result{}, handleSuspend(ctx, params, patcher, &mi, reconcileStart)
	}

	reportResume(ctx, params.EventRecorder, &mi)

	// Skip the render when its inputs are unchanged. It sits before the
	// deferred status commit is armed: a skip is not an attempt, patches
	// nothing, emits no event and records no reconcile metric.
	if instanceRenderSkippable(ctx, params, &mi) {
		return ctrl.Result{}, nil
	}

	// Track digests and outcome across phases for deferred status commit.
	var (
		outcome    Outcome
		digests    status.DigestSet
		reconciled bool // true if apply (and optional prune) succeeded
		newEntries []releasesv1alpha1.InventoryEntry
		errMsg     string
		retryAfter time.Duration // explicit backoff for failed outcomes

		// Phase outcome tracking for failure counters (updated in Phase 7).
		phases phaseOutcomes

		// skipCommit is set only when the wait for a render slot is cut
		// short: nothing was attempted, so there is nothing to record, and
		// the zero outcome (NoOp) would otherwise report a success. A panic
		// is caught before this flag and the NoOp branch are read.
		skipCommit bool

		// renderedVersion is the module version this attempt's render
		// reported, nil until a render result is in hand. A NoOp writes it
		// to lastAppliedVersion only when it is set, so a reconcile that did
		// not render never touches the field.
		renderedVersion *string

		// renderedInputs is the render input key of this attempt's render,
		// nil until a render result is in hand. A success or a NoOp records
		// it as lastAppliedInputs; a failure, a refusal or a panic does not.
		renderedInputs *status.RenderInputKey
	)

	// Deferred status commit — patches status on every reconcile attempt,
	// including NoOp. On NoOp, the patch is bounded to drift condition,
	// failure counter deltas, clearing nextRetryAt, requiredContracts and,
	// when this attempt rendered, lastAppliedVersion and (while the skip is
	// enabled) lastAppliedInputs; lastAttempted/history/inventory are not
	// touched (they describe meaningful outcomes).
	// Storm-safe: GenerationChangedPredicate on the controller's event filter
	// prevents status-only patches from triggering watch-driven reconciles.
	//
	// A panic is recovered first: the outcome is still its zero value, NoOp,
	// so without this branch the commit would mark a panicking reconcile
	// Ready. The attempt is recorded as failed and the panic re-raised with
	// its original value, so the controller runtime still logs it, counts it
	// and requeues the object on its rate limiter.
	defer func() {
		if r := recover(); r != nil {
			commitPanicStatus(ctx, patcher, &mi, r, digests, reconcileStart)
			panic(r)
		}
		if skipCommit {
			return
		}
		if outcome == NoOp {
			commitNoOpStatus(ctx, patcher, &mi, phases, renderedVersion,
				noOpInputs(params.DriftRenderInterval, renderedInputs), reconcileStart)
			return
		}

		now := metav1.Now()
		mi.Status.ObservedGeneration = mi.Generation
		mi.Status.LastAttemptedAction = reconcileAction
		mi.Status.LastAttemptedAt = &now
		duration := metav1.Duration{Duration: time.Since(reconcileStart)}
		mi.Status.LastAttemptedDuration = &duration
		mi.Status.LastAttemptedSourceDigest = digests.Source
		mi.Status.LastAttemptedConfigDigest = digests.Config
		mi.Status.LastAttemptedRenderDigest = digests.Render

		if reconciled {
			mi.Status.LastAppliedAt = &now
			mi.Status.LastAppliedSourceDigest = digests.Source
			mi.Status.LastAppliedVersion = appliedVersion(renderedVersion)
			mi.Status.LastAppliedConfigDigest = digests.Config
			mi.Status.LastAppliedRenderDigest = digests.Render
			recordInputs(&mi.Status.LastAppliedInputs, renderedInputs, now)

			mi.Status.Inventory = nextInventory(mi.Status.Inventory, newEntries)
			digests.Inventory = mi.Status.Inventory.Digest

			entry := status.NewSuccessEntry(reconcileAction, "complete", digests, int64(len(newEntries)))
			status.RecordHistory(&mi.Status, entry)
		} else if errMsg != "" {
			entry := status.NewFailureEntry(reconcileAction, errMsg, digests)
			status.RecordHistory(&mi.Status, entry)
		}
		// NoOp does not record history (per design doc).

		// Update failure counters based on phase outcomes.
		updateFailureCounters(&mi.Status, outcome, phases)

		// Set or clear NextRetryAt based on outcome.
		if retryAfter > 0 {
			retryTime := metav1.NewTime(time.Now().Add(retryAfter))
			mi.Status.NextRetryAt = &retryTime
		} else {
			mi.Status.NextRetryAt = nil
		}

		// Record reconcile metrics.
		recordReconcileMetrics(mi.Name, mi.Namespace, outcome, time.Since(reconcileStart), reconciled, len(newEntries))

		if patchErr := patcher.Patch(ctx, &mi,
			patch.WithOwnedConditions{
				Conditions: []string{
					status.ReadyCondition,
					status.ReconcilingCondition,
					status.StalledCondition,
					status.ModuleResolvedCondition,
					status.DriftedCondition,
				},
			},
			patch.WithStatusObservedGeneration{},
		); patchErr != nil {
			log.Error(patchErr, "Failed to patch ModuleInstance status")
		}
	}()

	// Mark reconciling at the start.
	status.MarkReconciling(&mi, "Progressing", "Reconciliation in progress")

	// Compute source and config digests early for no-op detection.
	// Source digest is derived from the module path + version (replaces Flux artifact digest).
	digests.Source = status.ModuleSourceDigest(mi.Spec.Module.Path, mi.Spec.Module.Version)
	digests.Config = status.ConfigDigest(mi.Spec.Values)

	// Phase 1: Synthesize, resolve, and render module from OCI registry, then
	// export the rendered set for apply. One slot of the process-wide pool is
	// held until the export is done: the rendered CUE values pin the whole
	// build until then, and the export is where the heap peaks.
	var (
		renderResult *render.RenderResult
		converted    *convertedRender
		err          error
		convErr      *conversionError
	)
	// The render may outlive this reconcile when it times out, so it reads a
	// copy of its inputs: the deferred status patch rewrites mi.
	inputs := instanceInputsOf(&mi)
	convert := params.convertFn()
	if waitErr := params.RenderSlots.Run(ctx, renderKey("ModuleInstance", mi.Namespace, mi.Name), params.RenderTimeout, func(renderCtx context.Context) {
		renderResult, converted, err = renderAndConvertInstance(renderCtx, params.Renderer, convert, inputs)
	}); waitErr != nil {
		// A render timeout is classified before anything else, and the
		// closure's variables are never read: the render may still be
		// writing them.
		if msg, ok := renderTimeoutMessage(waitErr, params.RenderTimeout); ok {
			log.Info("Render timed out", "timeout", params.RenderTimeout.String(), "reason", waitErr.Error())
			params.EventRecorder.Eventf(&mi, nil, corev1.EventTypeWarning, status.RenderTimedOutReason, "Render", "%s", msg)
			status.MarkRenderTimedOut(&mi, "%s", msg)
			outcome, errMsg = FailedTransient, msg
			retryAfter = retryIntervalFor(outcome, reconcileFailureCount(mi.Status.FailureCounters))
			return ctrl.Result{RequeueAfter: retryAfter}, nil
		}
		// The context ended while waiting for a slot (manager shutdown).
		// Nothing was rendered, so commit nothing and classify nothing.
		skipCommit = true
		return ctrl.Result{}, fmt.Errorf("waiting for a render slot: %w", waitErr)
	}
	if err != nil && !errors.As(err, &convErr) {
		outcome, errMsg = classifyRenderError(&mi, params.EventRecorder, err)
		// PlatformNotReady is a transient blocked-on-dependency state (the
		// platform store holds no generated platform module yet), so it must
		// retry on the fast exponential backoff like other transient failures
		// — not the 30-minute stalled recheck. The Platform watch normally
		// re-enqueues promptly when the platform is generated, but that edge
		// is missed when the controller restarts into an already-Ready
		// Platform (the regeneration emits no status event), leaving the
		// bounded backoff as the real recovery path. A registry fetch failure
		// the library typed, in any phase of the render, joins it on the
		// bounded backoff: nothing else re-triggers the instance, and the
		// registry may answer a minute later. Stalled render/resolution
		// errors, an acquisition failure the library did not classify among
		// them, keep the long recheck.
		retryAfter = retryIntervalFor(outcome, reconcileFailureCount(mi.Status.FailureCounters))
		return ctrl.Result{RequeueAfter: retryAfter}, nil
	}

	status.MarkModuleResolved(&mi, fmt.Sprintf("%s@%s", mi.Spec.Module.Path, mi.Spec.Module.Version))
	reportRenderDiagnostics(ctx, params.Warnings, params.EventRecorder, &mi, renderResult)

	if convErr != nil {
		status.MarkStalled(&mi, convErr.reason, "%s", convErr)
		outcome = FailedStalled
		errMsg = convErr.Error()
		retryAfter = StalledRecheckInterval
		return ctrl.Result{RequeueAfter: retryAfter}, nil
	}
	digests.Render = converted.digest
	digests.Inventory = inventory.ComputeDigest(converted.entries)
	renderedVersion = &renderResult.ModuleVersion
	key := renderedKey(digests.Source, digests.Config, renderResult, params.OperatorVersion, params.LibraryVersion)
	renderedInputs = &key

	// Phase 4: Plan actions — no-op detection, drift detection, compute stale set.
	//
	// The full rendered set was converted under the slot: the instance UUID
	// and the shrink verdict below are read from it, and the apply list every
	// later phase uses is derived from it.
	resources := converted.resources

	// Persist the rendered instance UUID on Status. All rendered resources
	// carry the same UUID (stamped by the CUE catalog's moduleLabels merge);
	// reading the first non-empty one is sufficient. Consumed by the prune
	// ownership guard in both apply→prune and deletion paths.
	if uuid := extractInstanceUUID(resources); uuid != "" {
		mi.Status.InstanceUUID = uuid
	}

	// Persist the contracts this instance's components demand (enhancement
	// 0015:D3/D16), as the kernel's render reported them. Written here
	// rather than in the success path because the demand is a fact about
	// the render, not about the apply: a regenerated platform re-enqueues
	// every instance, and the render that follows is frequently a no-op for
	// apply while being the only evidence that the demand moved. The no-op
	// branch of the deferred patch commits it along with everything else on
	// Status.
	//
	// Every path that returns before this point leaves the previous value,
	// which over-reports demand and so blocks a claim deletion that could
	// have proceeded. That is the direction a guard should fail in.
	mi.Status.RequiredContracts = renderResult.RequiredContracts

	// Phase 4a: judge the rendered claims before anything reaches the cluster
	// (0015:D16). A provider upgrade whose re-rendered
	// TransformerRegistration drops a contract instances still demand is
	// withheld from the apply list, so the accepted claim keeps serving its
	// dependents. Every other rendered resource applies as usual, and the
	// inventory below is still built from the full rendered set.
	//
	// This runs before drift detection and before the no-op return: what was
	// withheld is a fact the phases after it read.
	applyList, refused, err := withholdRefused(ctx, shrinkDecider(params), resources)
	if err != nil {
		// The verdict could not be reached, so the apply cannot proceed:
		// applying on an unreadable answer is the abandonment the refusal
		// exists to prevent. Transient — it is an API read that failed.
		status.MarkNotReady(&mi, status.ApplyFailedReason, "%s", err)
		outcome = FailedTransient
		errMsg = err.Error()
		retryAfter = ComputeBackoff(reconcileFailureCount(mi.Status.FailureCounters) + 1)
		return ctrl.Result{RequeueAfter: retryAfter}, nil
	}

	lastApplied := status.DigestSet{
		Source:    mi.Status.LastAppliedSourceDigest,
		Config:    mi.Status.LastAppliedConfigDigest,
		Render:    mi.Status.LastAppliedRenderDigest,
		Inventory: inventoryDigest(mi.Status.Inventory),
	}

	isNoOp := noOp(digests, lastApplied, refused)

	// Drift detection runs on every reconcile, including no-ops.
	// Uses SSA dry-run to compare desired state against live cluster state.
	//
	// It compares the apply list, not the full rendered set, so a withheld
	// resource is excluded. Drift reports that the cluster diverged from what
	// the operator asserts; a withheld resource is one the operator is
	// deliberately not asserting, so reporting it would name a difference the
	// operator created on purpose and intends not to close — a condition that
	// never clears, burying real drift on the same instance behind it. The
	// refusal carries that signal instead. A resource that stops being
	// withheld re-enters the apply list and is compared again from then on.
	phases.driftRan = true
	phases.driftFailed = detectDrift(ctx, params.ResourceManager, &mi, applyList)

	if isNoOp {
		log.Info("No changes detected, skipping apply")
		params.EventRecorder.Eventf(&mi, nil, corev1.EventTypeNormal, status.NoOpReason, "Reconcile", "No changes detected")
		outcome = NoOp
		return ctrl.Result{}, nil
	}

	var previousEntries []releasesv1alpha1.InventoryEntry
	if mi.Status.Inventory != nil {
		previousEntries = mi.Status.Inventory.Entries
	}
	staleSet := inventory.ComputeStaleSet(previousEntries, converted.entries)

	// Build impersonated client and resource manager if serviceAccountName is set.
	// Apply and prune use the impersonated identity; all other phases use the controller's own client.
	applyRM, applyClient, impErr := buildApplyClient(ctx, params, &mi)
	if impErr != nil {
		status.MarkStalled(&mi, status.ImpersonationFailedReason, "%s", impErr)
		outcome = FailedStalled
		errMsg = impErr.Error()
		retryAfter = StalledRecheckInterval
		return ctrl.Result{RequeueAfter: retryAfter}, nil
	}

	// Phase 5: Apply resources.
	phases.applyRan = true
	force := mi.Spec.Rollout != nil && mi.Spec.Rollout.ForceConflicts
	effectiveSA, _ := resolveEffectiveSA(mi.Spec.ServiceAccountName, params.DefaultServiceAccount)
	applyResult, err := apply.Apply(ctx, applyRM, applyList, force)
	if err != nil {
		phases.applyFailed = true
		params.EventRecorder.Eventf(&mi, nil, corev1.EventTypeWarning, status.ApplyFailedReason, "Apply", "%s", err)
		outcome = markApplyFailure(&mi, err, effectiveSA)
		errMsg = err.Error()
		retryAfter = retryIntervalFor(outcome, reconcileFailureCount(mi.Status.FailureCounters))
		return ctrl.Result{RequeueAfter: retryAfter}, nil
	}

	total := applyResult.Created + applyResult.Updated + applyResult.Unchanged
	params.EventRecorder.Eventf(&mi, nil, corev1.EventTypeNormal, status.AppliedReason, "Apply",
		"Applied %d resources (%d created, %d updated, %d unchanged)",
		total, applyResult.Created, applyResult.Updated, applyResult.Unchanged)

	log.Info("Applied resources",
		"created", applyResult.Created, "updated", applyResult.Updated, "unchanged", applyResult.Unchanged)

	// Record apply metrics.
	opmmetrics.RecordApply(mi.Name, mi.Namespace, applyResult.Created, applyResult.Updated, applyResult.Unchanged)

	// A refused upgrade is reported here, on the instance whose render
	// produced the claim, because the instance's apply is what was refused
	// (0015:D16). The claim's own conditions are left alone:
	// acceptance owns them, and a claim whose stored spec was never replaced
	// has nothing new to report.
	//
	// The return sits before ClearDrifted and before the prune on purpose.
	// Clearing drift would claim an apply resolved a divergence this
	// reconcile deliberately left standing, and pruning while refusing would
	// delete on the strength of a render the operator just declined to
	// assert. Leaving reconciled false is what keeps the refusal alive: the
	// applied digests stay behind the rendered ones, so the next reconcile is
	// not a no-op and the refusal is re-decided rather than forgotten.
	//
	// Transient, not stalled: the block clears the moment the dependents stop
	// demanding the contract, with no action on this object, and no watch
	// re-enqueues the provider when a dependent's demand moves. The bounded
	// backoff (capped at five minutes) is the real recovery path, the same
	// reasoning PlatformNotReady above is classified by.
	if len(refused) > 0 {
		msg := refusalMessage(refused)
		params.EventRecorder.Eventf(&mi, nil, corev1.EventTypeWarning,
			status.DependentsRemainReason, "Apply", "%s", msg)
		status.MarkNotReady(&mi, status.DependentsRemainReason, "%s", msg)
		log.Info("Refused a provides shrink that would abandon dependents",
			"claims", len(refused), "message", msg)
		outcome = FailedTransient
		errMsg = msg
		retryAfter = ComputeBackoff(reconcileFailureCount(mi.Status.FailureCounters) + 1)
		return ctrl.Result{RequeueAfter: retryAfter}, nil
	}

	// Successful apply resolves any drift.
	status.ClearDrifted(&mi)

	newEntries = converted.entries

	// Phase 6: Prune stale resources (only if spec.prune=true and apply succeeded).
	phases.pruneRan = true
	var pruneDeleted int
	outcome, reconciled, pruneDeleted, err = pruneStaleResources(ctx, &mi, applyClient, staleSet, effectiveSA, params.EventRecorder)
	if err != nil {
		phases.pruneFailed = true
		errMsg = err.Error()
		retryAfter = ComputeBackoff(reconcileFailureCount(mi.Status.FailureCounters) + 1)
		return ctrl.Result{RequeueAfter: retryAfter}, nil
	}
	if !reconciled {
		phases.pruneFailed = true
		retryAfter = StalledRecheckInterval
		return ctrl.Result{RequeueAfter: retryAfter}, nil
	}

	// Record prune metrics.
	opmmetrics.RecordPrune(mi.Name, mi.Namespace, pruneDeleted)

	// Phase 7: Commit status (handled by deferred function).
	status.MarkReady(&mi, "Reconciliation succeeded")
	params.EventRecorder.Eventf(&mi, nil, corev1.EventTypeNormal, status.ReconciliationSucceededReason, "Reconcile", "Reconciliation succeeded")
	log.Info("Reconciliation complete", "outcome", outcome.String())

	return ctrl.Result{}, nil
}

// markApplyFailure records a failed apply on the instance and returns the
// outcome it should be reported as.
//
// A forbidden error under an impersonated identity is an RBAC problem the
// tenant's ServiceAccount cannot retry its way out of, so it stalls and names
// impersonation; every other apply error is transient and retries.
func markApplyFailure(mi *releasesv1alpha1.ModuleInstance, err error, effectiveSA string) Outcome {
	if effectiveSA != "" && isForbidden(err) {
		status.MarkStalled(mi, status.ImpersonationFailedReason, "%s", err)
		return FailedStalled
	}
	status.MarkNotReady(mi, status.ApplyFailedReason, "%s", err)
	return FailedTransient
}

// retryIntervalFor maps a failed outcome to the interval it should be retried
// on: a transient failure walks the bounded exponential backoff, a stalled one
// waits for the long safety recheck that guards against misclassification.
func retryIntervalFor(outcome Outcome, failures int64) time.Duration {
	if outcome == FailedTransient {
		return ComputeBackoff(failures + 1)
	}
	return StalledRecheckInterval
}

// noOp reports whether this reconcile has nothing to do.
//
// A reconcile that withheld a resource never has nothing to do: the cluster
// does not hold what the render produced. The digests say the same thing on
// their own — a refusal commits none of them — but stating it here keeps the
// requirement readable instead of leaving it to be re-derived from where the
// refusal returns.
func noOp(digests, lastApplied status.DigestSet, refused []shrink.Decision) bool {
	return len(refused) == 0 && status.IsNoOp(digests, lastApplied)
}

// phaseOutcomes tracks which phases ran and whether they failed,
// for deferred failure counter updates in Phase 7.
type phaseOutcomes struct {
	driftRan    bool
	driftFailed bool
	applyRan    bool
	applyFailed bool
	pruneRan    bool
	pruneFailed bool
}

// updateFailureCounters applies failure counter increments and resets
// based on which phases ran and the overall reconcile outcome.
func updateFailureCounters(
	mrStatus *releasesv1alpha1.ModuleInstanceStatus,
	outcome Outcome,
	phases phaseOutcomes,
) {
	counters := status.EnsureCounters(mrStatus)

	if phases.driftRan {
		if phases.driftFailed {
			status.IncrementCounter(counters, status.CounterDrift)
		} else {
			status.ResetCounter(counters, status.CounterDrift)
		}
	}

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

// detectDrift runs SSA dry-run drift detection and updates status accordingly.
// Returns true if drift detection failed (API error).
// On drift: sets Drifted=True. On no drift: clears Drifted condition.
// Counter updates are deferred to Phase 7 based on the returned bool.
// Drift detection failure is non-blocking.
func detectDrift(
	ctx context.Context,
	rm *fluxssa.ResourceManager,
	mi *releasesv1alpha1.ModuleInstance,
	resources []*unstructured.Unstructured,
) bool {
	log := logf.FromContext(ctx)
	driftResult, err := apply.DetectDrift(ctx, rm, resources)
	if err != nil {
		log.Error(err, "Drift detection failed, continuing reconcile")
		return true
	}
	if driftResult.Drifted {
		log.Info("Drift detected", "driftedResources", len(driftResult.Resources))
		status.MarkDrifted(mi, len(driftResult.Resources))
	} else {
		status.ClearDrifted(mi)
	}
	return false
}

// reconcileFailureCount returns the current reconcile failure count, or 0 if counters are nil.
func reconcileFailureCount(counters *releasesv1alpha1.FailureCounters) int64 {
	if counters == nil {
		return 0
	}
	return counters.Reconcile
}

// nextInventory is the inventory recorded after a successful apply of
// entries: the revision after prev's (1 when there is none) and the digest of
// entries.
func nextInventory(prev *releasesv1alpha1.Inventory, entries []releasesv1alpha1.InventoryEntry) *releasesv1alpha1.Inventory {
	rev := int64(1)
	if prev != nil {
		rev = prev.Revision + 1
	}
	return &releasesv1alpha1.Inventory{
		Revision: rev,
		Digest:   inventory.ComputeDigest(entries),
		Count:    int64(len(entries)),
		Entries:  entries,
	}
}

// inventoryDigest returns the digest from the inventory, or empty string if nil.
func inventoryDigest(inv *releasesv1alpha1.Inventory) string {
	if inv == nil {
		return ""
	}
	return inv.Digest
}

// handleNotReconciled runs the two gates that keep the operator away from an
// instance, in order, and reports whether one of them handled it.
func handleNotReconciled(
	ctx context.Context,
	params *ModuleInstanceParams,
	mi *releasesv1alpha1.ModuleInstance,
) (bool, error) {
	// Owner-skip gate: CLI-owned instances are managed externally. The operator
	// stays entirely hands-off — no render, apply, prune, deletion cleanup, and
	// crucially no finalizer. The check sits before finalizer registration so a
	// CLI-owned CR never carries opmodel.dev/cleanup (whose deletion path would
	// prune resources the CLI owns). Only an explicit owner == cli skips; absent,
	// empty, and operator all fall through to the normal operator-managed path,
	// except on the operator's own instance, which is refused below.
	if mi.Spec.Owner == releasesv1alpha1.OwnerCLI {
		return true, handleCLIOwned(ctx, params, mi)
	}

	// The operator never reconciles the instance that deploys it, whatever its
	// owner says: install must be able to repair the operator without it.
	if signal, own := isOwnInstance(mi); own {
		return true, handleOwnInstance(ctx, params, mi, signal)
	}
	return false, nil
}

// handleCLIOwned implements the owner-skip gate for CLI-owned instances. The
// operator is hands-off: no render, apply, prune, deletion cleanup, or
// finalizer. For a deleting instance it returns immediately — no finalizer was
// ever added, so there is nothing to clean up or unblock. Otherwise it records
// a single Ready=Unknown/ManagedExternally acknowledgement and nothing else: no
// observedGeneration (no reconcile happened) and no CLI-written status
// (inventory, lastApplied*, instanceUUID). The patcher snapshots the object
// before MarkManagedExternally mutates only the conditions, so CLI-written
// fields are identical in snapshot and current and are never patched. The
// static message makes the write idempotent across repeated wake-ups.
func handleCLIOwned(
	ctx context.Context,
	params *ModuleInstanceParams,
	mi *releasesv1alpha1.ModuleInstance,
) error {
	log := logf.FromContext(ctx)

	if !mi.DeletionTimestamp.IsZero() {
		return nil
	}

	log.Info("ModuleInstance is managed externally by the CLI, skipping reconciliation")
	patcher := patch.NewSerialPatcher(mi, params.Client)
	status.MarkManagedExternally(mi)
	params.EventRecorder.Eventf(mi, nil, corev1.EventTypeNormal, status.ManagedExternallyReason, "Reconcile", "ModuleInstance is managed externally by the CLI")
	return patcher.Patch(ctx, mi,
		patch.WithOwnedConditions{
			Conditions: []string{
				status.ReadyCondition,
				status.ReconcilingCondition,
				status.StalledCondition,
				status.ModuleResolvedCondition,
				status.DriftedCondition,
			},
		},
	)
}

// handleOwnInstance refuses the operator's own instance when its owner is
// absent or operator. It never renders, applies or prunes, and it releases a
// cleanup finalizer an earlier operator release may have added, without
// pruning, so neither uninstall nor a reinstall of the operator waits on a
// finalizer only a running operator could clear. On a live instance it
// records Ready=False and Stalled=True (SelfManagementRefused) with the
// observed generation, so a client waiting on that generation reads a final
// verdict; it leaves inventory, digests, instanceUUID and history alone. No
// requeue: the refusal is re-evaluated when the instance changes.
func handleOwnInstance(
	ctx context.Context,
	params *ModuleInstanceParams,
	mi *releasesv1alpha1.ModuleInstance,
	signal string,
) error {
	log := logf.FromContext(ctx)

	if controllerutil.ContainsFinalizer(mi, FinalizerName) {
		log.Info("Releasing the cleanup finalizer from the operator's own instance without pruning")
		if err := removeFinalizer(ctx, params.Client, mi); err != nil {
			if apierrors.IsNotFound(err) {
				params.Warnings.Forget(keyOf(mi))
				return nil
			}
			return fmt.Errorf("removing finalizer: %w", err)
		}
	}

	if !mi.DeletionTimestamp.IsZero() {
		params.Warnings.Forget(keyOf(mi))
		return nil
	}

	msg := fmt.Sprintf("this ModuleInstance deploys the operator (%s); "+
		"the operator never applies or prunes its own instance. Set spec.owner to cli", signal)
	ready := apimeta.FindStatusCondition(mi.Status.Conditions, status.ReadyCondition)
	already := ready != nil && ready.Reason == status.SelfManagementRefusedReason &&
		mi.Status.ObservedGeneration == mi.Generation

	log.Info("Refusing to reconcile the operator's own instance", "signal", signal)
	patcher := patch.NewSerialPatcher(mi, params.Client)
	status.MarkSelfManagementRefused(mi, msg)
	mi.Status.ObservedGeneration = mi.Generation
	if !already {
		params.EventRecorder.Eventf(mi, nil, corev1.EventTypeWarning,
			status.SelfManagementRefusedReason, "Reconcile", "%s", msg)
	}
	return patcher.Patch(ctx, mi,
		patch.WithOwnedConditions{
			Conditions: []string{
				status.ReadyCondition,
				status.ReconcilingCondition,
				status.StalledCondition,
				status.ModuleResolvedCondition,
				status.DriftedCondition,
			},
		},
		patch.WithStatusObservedGeneration{},
	)
}

// handleSuspend records the suspended state for an operator-owned instance:
// Ready=False/Suspended (clearing Reconciling/Stalled), the observed generation,
// and a Suspend event. It runs before the deferred status commit so the existing
// status fields (inventory, lastApplied*, history) are preserved untouched.
func handleSuspend(
	ctx context.Context,
	params *ModuleInstanceParams,
	patcher *patch.SerialPatcher,
	mi *releasesv1alpha1.ModuleInstance,
	reconcileStart time.Time,
) error {
	log := logf.FromContext(ctx)
	log.Info("Reconciliation is suspended")
	status.MarkSuspended(mi)
	mi.Status.ObservedGeneration = mi.Generation
	params.EventRecorder.Eventf(mi, nil, corev1.EventTypeNormal, status.SuspendedReason, "Suspend", "Reconciliation is suspended")
	if patchErr := patcher.Patch(ctx, mi,
		patch.WithOwnedConditions{
			Conditions: []string{
				status.ReadyCondition,
				status.ReconcilingCondition,
				status.StalledCondition,
				status.ModuleResolvedCondition,
				status.DriftedCondition,
			},
		},
		patch.WithStatusObservedGeneration{},
	); patchErr != nil {
		return patchErr
	}
	opmmetrics.RecordDuration(mi.Name, mi.Namespace, time.Since(reconcileStart))
	return nil
}

// handleDeletion runs the deletion cleanup path.
// If spec.prune is true, all inventory entries are pruned (respecting safety exclusions).
// On success (or prune disabled), the finalizer is removed.
// On partial failure, the finalizer is retained and the error is returned for requeue.
//
// If the impersonation ServiceAccount is missing, the instance stalls with
// DeletionSAMissingReason and the finalizer is retained until either the SA
// is restored or the orphan annotation
// (v1alpha1.AnnotationForceDeleteOrphan = "true") is set to release it.
func handleDeletion(
	ctx context.Context,
	params *ModuleInstanceParams,
	mi *releasesv1alpha1.ModuleInstance,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Running deletion cleanup for ModuleInstance")

	patcher := patch.NewSerialPatcher(mi, params.Client)

	if !mi.Spec.Prune || mi.Status.Inventory == nil || len(mi.Status.Inventory.Entries) == 0 {
		if !mi.Spec.Prune {
			log.Info("Prune disabled, orphaning managed resources on deletion")
		}
		if err := removeFinalizer(ctx, params.Client, mi); err != nil {
			return ctrl.Result{}, fmt.Errorf("removing finalizer: %w", err)
		}
		log.Info("Finalizer removed, deletion can proceed")
		return ctrl.Result{}, nil
	}

	effectiveSA, source := resolveEffectiveSA(mi.Spec.ServiceAccountName, params.DefaultServiceAccount)
	deleteClient := params.Client
	if effectiveSA != "" && params.RestConfig != nil {
		impClient, impErr := apply.NewImpersonatedClient(ctx, params.RestConfig, params.APIReader, params.Client.Scheme(), mi.Namespace, effectiveSA)
		if impErr != nil {
			return handleDeletionImpersonationFailure(ctx, params, patcher, mi, effectiveSA, source, impErr)
		}
		deleteClient = impClient
	}

	pruneResult, err := apply.Prune(ctx, deleteClient, mi.Status.InstanceUUID, mi.Status.Inventory.Entries)
	if err != nil {
		if effectiveSA != "" && isForbidden(err) {
			log.Error(err, "Impersonation denied during deletion cleanup",
				"serviceAccount", effectiveSA,
				"serviceAccountSource", source)
			emit := !readyAlreadyStalledWith(mi.Status.Conditions, status.ImpersonationFailedReason)
			status.MarkStalled(mi, status.ImpersonationFailedReason, "%s", err)
			if emit {
				params.EventRecorder.Eventf(mi, nil, corev1.EventTypeWarning,
					status.ImpersonationFailedReason, "Delete", "%s", err)
			}
			if patchErr := patchDeletionStatus(ctx, patcher, mi); patchErr != nil {
				log.Error(patchErr, "Failed to patch ModuleInstance status on Forbidden deletion prune")
			}
			return ctrl.Result{RequeueAfter: StalledRecheckInterval}, nil
		}
		log.Error(err, "Partial failure during deletion cleanup, retaining finalizer")
		return ctrl.Result{}, err
	}
	log.Info("Deletion cleanup pruned resources",
		"deleted", pruneResult.Deleted, "skipped", pruneResult.Skipped)

	if err := removeFinalizer(ctx, params.Client, mi); err != nil {
		return ctrl.Result{}, fmt.Errorf("removing finalizer: %w", err)
	}
	log.Info("Finalizer removed, deletion can proceed")

	return ctrl.Result{}, nil
}

// handleDeletionImpersonationFailure branches on the impersonation error type:
//   - SA NotFound + orphan annotation set: clear inventory, emit event, remove finalizer.
//   - SA NotFound, no annotation: stall with DeletionSAMissingReason, retain finalizer.
//   - Other impersonation error: stall with the generic ImpersonationFailedReason.
func handleDeletionImpersonationFailure(
	ctx context.Context,
	params *ModuleInstanceParams,
	patcher *patch.SerialPatcher,
	mi *releasesv1alpha1.ModuleInstance,
	effectiveSA, source string,
	impErr error,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if apply.IsServiceAccountNotFound(impErr) {
		if mi.GetAnnotations()[releasesv1alpha1.AnnotationForceDeleteOrphan] == "true" {
			orphanCount := int64(len(mi.Status.Inventory.Entries))
			log.Info("Orphaning inventory and removing finalizer at operator request",
				"serviceAccount", effectiveSA,
				"serviceAccountSource", source,
				"inventoryCount", orphanCount)
			params.EventRecorder.Eventf(mi, nil, corev1.EventTypeWarning,
				status.OrphanedOnDeletionReason, "Delete",
				"Orphaned %d managed resources; ServiceAccount %q missing and %s annotation set",
				orphanCount, effectiveSA, releasesv1alpha1.AnnotationForceDeleteOrphan)
			mi.Status.Inventory = nil
			if err := patchDeletionStatus(ctx, patcher, mi); err != nil {
				log.Error(err, "Failed to patch ModuleInstance status on orphan-exit")
			}
			if err := removeFinalizer(ctx, params.Client, mi); err != nil {
				return ctrl.Result{}, fmt.Errorf("removing finalizer: %w", err)
			}
			log.Info("Finalizer removed, deletion can proceed")
			return ctrl.Result{}, nil
		}

		log.Error(impErr, "Impersonation ServiceAccount missing during deletion; release stalled pending operator action",
			"serviceAccount", effectiveSA,
			"serviceAccountSource", source,
			"annotation", releasesv1alpha1.AnnotationForceDeleteOrphan)
		msg := deletionSAMissingMessage(mi.Namespace, effectiveSA)
		emit := !readyAlreadyStalledWith(mi.Status.Conditions, status.DeletionSAMissingReason)
		status.MarkStalled(mi, status.DeletionSAMissingReason, "%s", msg)
		if emit {
			params.EventRecorder.Eventf(mi, nil, corev1.EventTypeWarning,
				status.DeletionSAMissingReason, "Delete", "%s", msg)
		}
		if err := patchDeletionStatus(ctx, patcher, mi); err != nil {
			log.Error(err, "Failed to patch ModuleInstance status on DeletionSAMissing stall")
		}
		return ctrl.Result{RequeueAfter: StalledRecheckInterval}, nil
	}

	log.Error(impErr, "Impersonation failed during deletion cleanup",
		"serviceAccount", effectiveSA,
		"serviceAccountSource", source)
	emit := !readyAlreadyStalledWith(mi.Status.Conditions, status.ImpersonationFailedReason)
	status.MarkStalled(mi, status.ImpersonationFailedReason, "%s", impErr)
	if emit {
		params.EventRecorder.Eventf(mi, nil, corev1.EventTypeWarning,
			status.ImpersonationFailedReason, "Delete", "%s", impErr)
	}
	if err := patchDeletionStatus(ctx, patcher, mi); err != nil {
		log.Error(err, "Failed to patch ModuleInstance status on ImpersonationFailed stall")
	}
	return ctrl.Result{RequeueAfter: StalledRecheckInterval}, nil
}

// deletionSAMissingMessage formats the stall-condition message shown to
// operators when the impersonation ServiceAccount is missing on delete.
// Verbose by status-message standards — the goal is to replace "read the
// controller logs" with "read the status message".
func deletionSAMissingMessage(namespace, saName string) string {
	return fmt.Sprintf(
		"ServiceAccount %q not found; cannot prune owned resources during deletion. "+
			"Recovery options: "+
			"(1) Restore the ServiceAccount and its RBAC; "+
			"(2) Set spec.prune=false on the release and delete again to orphan resources without prune; "+
			"(3) Add annotation %q=%q to the release to remove the finalizer and leave resources behind "+
			"(operator is responsible for cleanup).",
		namespace+"/"+saName,
		releasesv1alpha1.AnnotationForceDeleteOrphan,
		"true",
	)
}

// readyAlreadyStalledWith reports whether Ready is already False with the
// given reason. Used to suppress duplicate events across requeues of the
// same stall condition.
func readyAlreadyStalledWith(conds []metav1.Condition, reason string) bool {
	ready := apimeta.FindStatusCondition(conds, status.ReadyCondition)
	return ready != nil && ready.Status == metav1.ConditionFalse && ready.Reason == reason
}

// patchDeletionStatus commits the deletion-path status transitions (owned
// conditions + non-condition status fields like cleared inventory) without
// bumping observed-generation semantics meant for the apply path.
func patchDeletionStatus(ctx context.Context, patcher *patch.SerialPatcher, mi *releasesv1alpha1.ModuleInstance) error {
	return patcher.Patch(ctx, mi,
		patch.WithOwnedConditions{
			Conditions: []string{
				status.ReadyCondition,
				status.ReconcilingCondition,
				status.StalledCondition,
				status.ModuleResolvedCondition,
				status.DriftedCondition,
			},
		},
	)
}

// addFinalizer adds the cleanup finalizer to the ModuleInstance and patches it.
func addFinalizer(ctx context.Context, c client.Client, mi *releasesv1alpha1.ModuleInstance) error {
	mergePatch := client.MergeFrom(mi.DeepCopy())
	controllerutil.AddFinalizer(mi, FinalizerName)
	return c.Patch(ctx, mi, mergePatch)
}

// removeFinalizer removes the cleanup finalizer from the ModuleInstance and patches it.
func removeFinalizer(ctx context.Context, c client.Client, mi *releasesv1alpha1.ModuleInstance) error {
	mergePatch := client.MergeFrom(mi.DeepCopy())
	controllerutil.RemoveFinalizer(mi, FinalizerName)
	return c.Patch(ctx, mi, mergePatch)
}

// pruneStaleResources runs Phase 6: prune stale resources if spec.prune is true and stale resources exist.
// Emits prune events via the provided recorder. Returns the outcome, whether reconcile succeeded,
// the number of resources deleted, and any error.
func pruneStaleResources(
	ctx context.Context,
	mi *releasesv1alpha1.ModuleInstance,
	c client.Client,
	staleSet []releasesv1alpha1.InventoryEntry,
	effectiveSA string,
	recorder events.EventRecorder,
) (Outcome, bool, int, error) {
	if !mi.Spec.Prune || len(staleSet) == 0 {
		return Applied, true, 0, nil
	}
	log := logf.FromContext(ctx)
	pruneResult, err := apply.Prune(ctx, c, mi.Status.InstanceUUID, staleSet)
	if err != nil {
		recorder.Eventf(mi, nil, corev1.EventTypeWarning, status.PruneFailedReason, "Prune", "%s", err)
		if effectiveSA != "" && isForbidden(err) {
			status.MarkStalled(mi, status.ImpersonationFailedReason, "%s", err)
			return FailedStalled, false, 0, nil
		}
		status.MarkNotReady(mi, status.PruneFailedReason, "%s", err)
		return FailedTransient, false, 0, err
	}
	if pruneResult.Deleted > 0 {
		recorder.Eventf(mi, nil, corev1.EventTypeNormal, status.PrunedReason, "Prune",
			"Pruned %d stale resources", pruneResult.Deleted)
	}
	log.Info("Pruned stale resources", "deleted", pruneResult.Deleted, "skipped", pruneResult.Skipped)
	return AppliedAndPruned, true, pruneResult.Deleted, nil
}

// classifyRenderError maps a render error to its status condition and event,
// returning the reconcile outcome and error message for the deferred status
// commit. The stalled classifications requeue on StalledRecheckInterval (set
// by the caller).
//
// render.ErrPlatformNotReady is a blocked-on-dependency state: the platform
// store holds no generated platform module yet. The instance is healthy but
// waiting for the cluster Platform, so it is marked Ready=False/PlatformNotReady
// (not Stalled), applies and prunes nothing, and requeues. The Platform watch
// (mapPlatformToModuleInstances) re-enqueues it promptly when the platform is
// generated; the bounded backoff is the safety net.
//
// A registry fetch failure the library typed (IsTransientFailure), in any
// phase (module acquisition, values compile, synthesis, the render build),
// with no typed terminal cause is transient too: Ready=False/ResolutionFailed,
// not Stalled, retried on the bounded backoff. All other errors are terminal
// render/resolution stalls, classified by their typed cause
// (renderFailureReason): ResolutionFailed (which includes an acquisition
// failure the library did not classify, such as an unparsable version),
// SkewRefused, DuplicateIdentities or RenderFailed. No error is classified by
// its message text.
func classifyRenderError(
	mi *releasesv1alpha1.ModuleInstance,
	recorder events.EventRecorder,
	err error,
) (Outcome, string) {
	if errors.Is(err, render.ErrPlatformNotReady) {
		recorder.Eventf(mi, nil, corev1.EventTypeWarning, status.PlatformNotReadyReason, "Render", "%s", err)
		status.MarkNotReady(mi, status.PlatformNotReadyReason, "%s", err)
		return FailedTransient, err.Error()
	}
	if IsTransientFailure(err) {
		recorder.Eventf(mi, nil, corev1.EventTypeWarning, status.ResolutionFailedReason, "Render", "%s", err)
		status.MarkNotReady(mi, status.ResolutionFailedReason, "%s", err)
		return FailedTransient, err.Error()
	}
	reason := renderFailureReason(err)
	recorder.Eventf(mi, nil, corev1.EventTypeWarning, reason, "Render", "%s", err)
	status.MarkStalled(mi, reason, "%s", err)
	return FailedStalled, err.Error()
}

// isForbidden returns true if the error chain contains a Kubernetes Forbidden (403) status error.
// Flux SSA wraps API errors, so this unwraps through the chain.
func isForbidden(err error) bool {
	var statusErr *apierrors.StatusError
	if errors.As(err, &statusErr) {
		return apierrors.IsForbidden(statusErr)
	}
	return false
}

// buildApplyClient returns the ResourceManager and client to use for apply and prune.
// Resolution order for the impersonation target:
//  1. spec.serviceAccountName (explicit, wins)
//  2. params.DefaultServiceAccount (the manager's --default-service-account flag)
//  3. empty → fall back to the controller's own client
//
// The effective SA is always resolved in the instance's own namespace; the flag
// never introduces a cross-namespace reference.
func buildApplyClient(
	ctx context.Context,
	params *ModuleInstanceParams,
	mi *releasesv1alpha1.ModuleInstance,
) (*fluxssa.ResourceManager, client.Client, error) {
	effectiveSA, source := resolveEffectiveSA(mi.Spec.ServiceAccountName, params.DefaultServiceAccount)
	if effectiveSA == "" {
		return params.ResourceManager, params.Client, nil
	}
	log := logf.FromContext(ctx)
	log.Info("Building impersonated client",
		"serviceAccount", effectiveSA,
		"serviceAccountSource", source)
	impClient, err := apply.NewImpersonatedClient(ctx, params.RestConfig, params.APIReader, params.Client.Scheme(), mi.Namespace, effectiveSA)
	if err != nil {
		return nil, nil, err
	}
	return apply.NewResourceManager(impClient, "opm-controller"), impClient, nil
}

// resolveEffectiveSA applies the spec > flag > empty precedence and returns
// the effective SA name plus a source tag ("spec", "default", or "") for
// logging.
func resolveEffectiveSA(specSA, defaultSA string) (string, string) {
	if specSA != "" {
		return specSA, "spec"
	}
	if defaultSA != "" {
		return defaultSA, "default"
	}
	return "", ""
}

// recordReconcileMetrics records outcome, duration, and inventory size metrics.
func recordReconcileMetrics(name, namespace string, outcome Outcome, duration time.Duration, reconciled bool, inventoryCount int) {
	opmmetrics.RecordReconcile(name, namespace, outcome.MetricLabel(), duration)
	if reconciled {
		opmmetrics.SetInventorySize(name, namespace, inventoryCount)
	}
}

// extractInstanceUUID returns the instance UUID carried by the rendered
// resources via the `module-instance.opmodel.dev/uuid` label. All rendered
// resources carry the same UUID (stamped by the catalog's moduleLabels
// merge), so the first non-empty value wins. Returns "" if no resource
// carries the label.
func extractInstanceUUID(resources []*unstructured.Unstructured) string {
	for _, r := range resources {
		if uuid := r.GetLabels()[labels.ModuleInstanceUUID]; uuid != "" {
			return uuid
		}
	}
	return ""
}

// instanceInputs is what a ModuleInstance render reads, copied from the
// object before the render starts, so a render that outlives its reconcile
// never reads an object the status patch is rewriting.
type instanceInputs struct {
	name, namespace string
	path, version   string
	values          *releasesv1alpha1.RawValues
}

func instanceInputsOf(mi *releasesv1alpha1.ModuleInstance) instanceInputs {
	return instanceInputs{
		name:      mi.Name,
		namespace: mi.Namespace,
		path:      mi.Spec.Module.Path,
		version:   mi.Spec.Module.Version,
		values:    mi.Spec.Values.DeepCopy(),
	}
}

// renderAndConvertInstance renders the instance described by in and exports
// the result for apply with convert. It runs inside the reconcile's render
// slot. On a conversion failure it still returns the render result, whose
// plain data the caller reports, and a *conversionError.
func renderAndConvertInstance(
	ctx context.Context,
	renderer render.ModuleRenderer,
	convert func(*render.RenderResult) (*convertedRender, error),
	in instanceInputs,
) (*render.RenderResult, *convertedRender, error) {
	result, err := renderer.RenderModule(
		ctx,
		in.name, in.namespace,
		in.path, in.version,
		in.values,
	)
	if err != nil {
		return nil, nil, err
	}
	converted, err := convert(result)
	return result, converted, err
}

// reportResume logs and emits the Resumed event when mi was suspended until
// this reconcile.
func reportResume(ctx context.Context, recorder events.EventRecorder, mi *releasesv1alpha1.ModuleInstance) {
	if ready := apimeta.FindStatusCondition(mi.Status.Conditions, status.ReadyCondition); ready != nil && ready.Reason == status.SuspendedReason {
		logf.FromContext(ctx).Info("Reconciliation resumed")
		recorder.Eventf(mi, nil, corev1.EventTypeNormal, status.ResumedReason, "Resume", "Reconciliation resumed")
	}
}

// instanceRenderSkippable reports whether this reconcile of mi may skip its
// render because the render input key computed from the spec, the cluster
// Platform and the running versions matches status.lastAppliedInputs, under
// the conditions of renderSkip.maySkip. It logs the skip.
func instanceRenderSkippable(ctx context.Context, params *ModuleInstanceParams, mi *releasesv1alpha1.ModuleInstance) bool {
	if params.DriftRenderInterval <= 0 || mi.Status.LastAppliedInputs == nil {
		return false
	}
	key := func() status.RenderInputKey {
		identity, skew := platformKeyParts(ctx, params.Client)
		return status.RenderInputKey{
			Source:          status.ModuleSourceDigest(mi.Spec.Module.Path, mi.Spec.Module.Version),
			Config:          status.ConfigDigest(mi.Spec.Values),
			PackageIdentity: identity,
			SkewPolicy:      skew,
			OperatorVersion: params.OperatorVersion,
			LibraryVersion:  params.LibraryVersion,
		}
	}
	skip := renderSkip{interval: params.DriftRenderInterval, now: time.Now()}
	if !skip.maySkip(mi.Status.Conditions, mi.Generation, mi.Status.ObservedGeneration, mi.Status.LastAppliedInputs, key) {
		return false
	}
	logRenderSkip(ctx, mi.Status.LastAppliedInputs, params.DriftRenderInterval)
	return true
}
