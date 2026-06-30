package reconcile

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/apply"
	"github.com/open-platform-model/opm-operator/internal/inventory"
	opmmetrics "github.com/open-platform-model/opm-operator/internal/metrics"
	"github.com/open-platform-model/opm-operator/internal/render"
	"github.com/open-platform-model/opm-operator/internal/status"
	"github.com/open-platform-model/opm-operator/pkg/core"
)

const (
	// FinalizerName is the finalizer registered on ModuleInstance resources
	// to ensure owned resources are cleaned up before deletion completes.
	// Was: "releases.opmodel.dev/cleanup" (enhancement 0002 D5 group move).
	FinalizerName = "opmodel.dev/cleanup"
)

// ModuleInstanceParams holds the dependencies injected into the reconcile loop.
//
// Was: ModuleReleaseParams
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
	// DefaultServiceAccount is the fallback SA name used when a
	// ModuleInstance has an empty spec.serviceAccountName. Empty disables
	// the default and preserves the controller-client fallback.
	DefaultServiceAccount string
}

// ReconcileModuleInstance orchestrates all phases of the reconcile loop.
// Phases run sequentially; errors halt progression.
// Status is always patched at the end via deferred function.
//
// Was: ReconcileModuleRelease
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

	// Owner-skip gate: CLI-owned instances are managed externally. The operator
	// stays entirely hands-off — no render, apply, prune, deletion cleanup, and
	// crucially no finalizer. The check sits before finalizer registration so a
	// CLI-owned CR never carries opmodel.dev/cleanup (whose deletion path would
	// prune resources the CLI owns). Only an explicit owner == cli skips; absent,
	// empty, and operator all fall through to the normal operator-managed path.
	if mi.Spec.Owner == releasesv1alpha1.OwnerCLI {
		return ctrl.Result{}, handleCLIOwned(ctx, params, &mi)
	}

	// Track reconcile start time for duration calculation.
	// Set after the CLI-owned skip (which records no metrics) and before the
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

	// Check for resume from suspend.
	if ready := apimeta.FindStatusCondition(mi.Status.Conditions, status.ReadyCondition); ready != nil && ready.Reason == status.SuspendedReason {
		log.Info("Reconciliation resumed")
		params.EventRecorder.Eventf(&mi, nil, corev1.EventTypeNormal, status.ResumedReason, "Resume", "Reconciliation resumed")
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
	)

	// Deferred status commit — patches status on every reconcile attempt,
	// including NoOp. On NoOp, the patch is bounded to drift condition,
	// failure counter deltas, and clearing nextRetryAt; lastAttempted/history/
	// inventory are not touched (they describe meaningful outcomes).
	// Storm-safe: GenerationChangedPredicate on the controller's event filter
	// prevents status-only patches from triggering watch-driven reconciles.
	defer func() {
		if outcome == NoOp {
			// Drift detection ran and may have set/cleared the Drifted
			// condition; phase counters may need increment/reset. Persist
			// these via a bounded patch.
			//
			// NoOp implies digests match LastApplied — a previous reconcile
			// applied successfully — so Ready=True is the correct state.
			// MarkReconciling at the start of this reconcile transiently set
			// Ready=Unknown; reset it now before patching.
			status.MarkReady(&mi, "Reconciliation succeeded")
			updateFailureCounters(&mi.Status, outcome, phases)
			mi.Status.NextRetryAt = nil
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
				log.Error(patchErr, "Failed to patch NoOp status")
			}
			recordReconcileMetrics(mi.Name, mi.Namespace, outcome, time.Since(reconcileStart), false, 0)
			opmmetrics.RecordDuration(mi.Name, mi.Namespace, time.Since(reconcileStart))
			return
		}

		now := metav1.Now()
		mi.Status.ObservedGeneration = mi.Generation
		mi.Status.LastAttemptedAction = "reconcile"
		mi.Status.LastAttemptedAt = &now
		duration := metav1.Duration{Duration: time.Since(reconcileStart)}
		mi.Status.LastAttemptedDuration = &duration
		mi.Status.LastAttemptedSourceDigest = digests.Source
		mi.Status.LastAttemptedConfigDigest = digests.Config
		mi.Status.LastAttemptedRenderDigest = digests.Render

		if reconciled {
			mi.Status.LastAppliedAt = &now
			mi.Status.LastAppliedSourceDigest = digests.Source
			mi.Status.LastAppliedConfigDigest = digests.Config
			mi.Status.LastAppliedRenderDigest = digests.Render

			invDigest := inventory.ComputeDigest(newEntries)
			rev := int64(1)
			if mi.Status.Inventory != nil {
				rev = mi.Status.Inventory.Revision + 1
			}
			mi.Status.Inventory = &releasesv1alpha1.Inventory{
				Revision: rev,
				Digest:   invDigest,
				Count:    int64(len(newEntries)),
				Entries:  newEntries,
			}
			digests.Inventory = invDigest

			entry := status.NewSuccessEntry("reconcile", "complete", digests, int64(len(newEntries)))
			status.RecordHistory(&mi.Status, entry)
		} else if errMsg != "" {
			entry := status.NewFailureEntry("reconcile", errMsg, digests)
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

	// Phase 1: Synthesize, resolve, and render module from OCI registry.
	// CUE's native module system resolves the target module from the registry.
	renderResult, err := params.Renderer.RenderModule(
		ctx,
		mi.Name, mi.Namespace,
		mi.Spec.Module.Path, mi.Spec.Module.Version,
		mi.Spec.Values,
	)
	if err != nil {
		outcome, errMsg = classifyRenderError(&mi, params.EventRecorder, err)
		// PlatformNotReady is a transient blocked-on-dependency state (the
		// platform store has no materialized platform yet), so it must retry on
		// the fast exponential backoff like other transient failures — not the
		// 30-minute stalled recheck. The Platform watch normally re-enqueues
		// promptly when the platform materializes, but that edge is missed when
		// the controller restarts into an already-Ready Platform (the
		// re-materialize emits no status event), leaving the bounded backoff as
		// the real recovery path. Genuinely stalled render/resolution errors
		// keep the long recheck.
		if outcome == FailedTransient {
			retryAfter = ComputeBackoff(reconcileFailureCount(mi.Status.FailureCounters) + 1)
		} else {
			retryAfter = StalledRecheckInterval
		}
		return ctrl.Result{RequeueAfter: retryAfter}, nil
	}

	status.MarkModuleResolved(&mi, fmt.Sprintf("%s@%s", mi.Spec.Module.Path, mi.Spec.Module.Version))

	renderDigest, err := status.RenderDigest(renderResult.Resources)
	if err != nil {
		status.MarkStalled(&mi, status.RenderFailedReason, "computing render digest: %s", err)
		outcome = FailedStalled
		errMsg = fmt.Sprintf("computing render digest: %s", err)
		retryAfter = StalledRecheckInterval
		return ctrl.Result{RequeueAfter: retryAfter}, nil
	}
	digests.Render = renderDigest
	digests.Inventory = inventory.ComputeDigest(renderResult.InventoryEntries)

	// Phase 4: Plan actions — no-op detection, drift detection, compute stale set.
	//
	// Convert resources early — needed for both drift detection and apply.
	resources, err := toUnstructuredSlice(renderResult.Resources)
	if err != nil {
		status.MarkStalled(&mi, status.ApplyFailedReason, "converting resources: %s", err)
		outcome = FailedStalled
		errMsg = fmt.Sprintf("converting resources: %s", err)
		retryAfter = StalledRecheckInterval
		return ctrl.Result{RequeueAfter: retryAfter}, nil
	}

	// Persist the rendered instance UUID on Status. All rendered resources
	// carry the same UUID (stamped by the CUE catalog's moduleLabels merge);
	// reading the first non-empty one is sufficient. Consumed by the prune
	// ownership guard in both apply→prune and deletion paths.
	if uuid := extractInstanceUUID(resources); uuid != "" {
		mi.Status.InstanceUUID = uuid
	}

	lastApplied := status.DigestSet{
		Source:    mi.Status.LastAppliedSourceDigest,
		Config:    mi.Status.LastAppliedConfigDigest,
		Render:    mi.Status.LastAppliedRenderDigest,
		Inventory: inventoryDigest(mi.Status.Inventory),
	}

	isNoOp := status.IsNoOp(digests, lastApplied)

	// Drift detection runs on every reconcile, including no-ops.
	// Uses SSA dry-run to compare desired state against live cluster state.
	phases.driftRan = true
	phases.driftFailed = detectDrift(ctx, params.ResourceManager, &mi, resources)

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
	staleSet := inventory.ComputeStaleSet(previousEntries, renderResult.InventoryEntries)

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
	applyResult, err := apply.Apply(ctx, applyRM, resources, force)
	if err != nil {
		phases.applyFailed = true
		params.EventRecorder.Eventf(&mi, nil, corev1.EventTypeWarning, status.ApplyFailedReason, "Apply", "%s", err)
		if effectiveSA != "" && isForbidden(err) {
			status.MarkStalled(&mi, status.ImpersonationFailedReason, "%s", err)
			outcome = FailedStalled
			errMsg = err.Error()
			retryAfter = StalledRecheckInterval
			return ctrl.Result{RequeueAfter: retryAfter}, nil
		}
		status.MarkNotReady(&mi, status.ApplyFailedReason, "%s", err)
		outcome = FailedTransient
		errMsg = err.Error()
		retryAfter = ComputeBackoff(reconcileFailureCount(mi.Status.FailureCounters) + 1)
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

	// Successful apply resolves any drift.
	status.ClearDrifted(&mi)

	newEntries = renderResult.InventoryEntries

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

// inventoryDigest returns the digest from the inventory, or empty string if nil.
func inventoryDigest(inv *releasesv1alpha1.Inventory) string {
	if inv == nil {
		return ""
	}
	return inv.Digest
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
// commit. Both classifications requeue on StalledRecheckInterval (set by the
// caller).
//
// render.ErrPlatformNotReady is a blocked-on-dependency state: the platform
// store holds no materialized platform yet. The instance is healthy but waiting
// for the cluster Platform, so it is marked Ready=False/PlatformNotReady (not
// Stalled), applies and prunes nothing, and requeues. The Platform watch
// (mapPlatformToModuleInstances) re-enqueues it promptly when the platform
// materializes; StalledRecheckInterval is the safety net. All other errors are
// terminal render/resolution stalls.
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
	reason := status.RenderFailedReason
	if isResolutionError(err) {
		reason = status.ResolutionFailedReason
	}
	recorder.Eventf(mi, nil, corev1.EventTypeWarning, reason, "Render", "%s", err)
	status.MarkStalled(mi, reason, "%s", err)
	return FailedStalled, err.Error()
}

// isResolutionError returns true if the error indicates a module resolution
// failure (CUE couldn't resolve the module from the OCI registry), as opposed
// to a render/evaluation error.
func isResolutionError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "loading synthesized release") ||
		strings.Contains(msg, "synthesizing release")
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
		if uuid := r.GetLabels()[core.LabelModuleInstanceUUID]; uuid != "" {
			return uuid
		}
	}
	return ""
}

// toUnstructuredSlice converts core.Resource slice to unstructured slice for apply.
func toUnstructuredSlice(resources []*core.Resource) ([]*unstructured.Unstructured, error) {
	result := make([]*unstructured.Unstructured, 0, len(resources))
	for _, r := range resources {
		u, err := r.ToUnstructured()
		if err != nil {
			return nil, fmt.Errorf("converting %s to unstructured: %w", r, err)
		}
		result = append(result, u)
	}
	return result, nil
}
