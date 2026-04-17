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

	releasesv1alpha1 "github.com/open-platform-model/poc-controller/api/v1alpha1"
	"github.com/open-platform-model/poc-controller/internal/apply"
	"github.com/open-platform-model/poc-controller/internal/inventory"
	opmmetrics "github.com/open-platform-model/poc-controller/internal/metrics"
	"github.com/open-platform-model/poc-controller/internal/render"
	"github.com/open-platform-model/poc-controller/internal/status"
	"github.com/open-platform-model/poc-controller/pkg/core"
	"github.com/open-platform-model/poc-controller/pkg/provider"
)

const (
	// FinalizerName is the finalizer registered on ModuleRelease resources
	// to ensure owned resources are cleaned up before deletion completes.
	FinalizerName = "releases.opmodel.dev/cleanup"
)

// ModuleReleaseParams holds the dependencies injected into the reconcile loop.
type ModuleReleaseParams struct {
	Client client.Client
	// APIReader is an uncached reader used for one-off reads (e.g. ServiceAccount
	// existence checks for impersonation) that should not provision a cache informer.
	APIReader       client.Reader
	RestConfig      *rest.Config
	Provider        *provider.Provider
	ResourceManager *fluxssa.ResourceManager
	EventRecorder   events.EventRecorder
	// Renderer produces the render result for a ModuleRelease. Must be non-nil;
	// production wires render.RegistryRenderer, tests wire a stub.
	Renderer render.ModuleRenderer
}

// ReconcileModuleRelease orchestrates all phases of the reconcile loop.
// Phases run sequentially; errors halt progression.
// Status is always patched at the end via deferred function.
func ReconcileModuleRelease(
	ctx context.Context,
	params *ModuleReleaseParams,
	req ctrl.Request,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Phase 0: Load ModuleRelease, check deletion, check suspend, create patch helper.
	var mr releasesv1alpha1.ModuleRelease
	if err := params.Client.Get(ctx, req.NamespacedName, &mr); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Track reconcile start time for duration calculation.
	// Set before suspend/deletion checks so all paths are measured.
	reconcileStart := time.Now()

	// Register finalizer if not present. Finalizer patches don't bump
	// .metadata.generation, so GenerationChangedPredicate filters the
	// subsequent UPDATE event — explicit Requeue re-enters the workqueue.
	if !controllerutil.ContainsFinalizer(&mr, FinalizerName) {
		log.Info("Adding finalizer to ModuleRelease")
		if err := addFinalizer(ctx, params.Client, &mr); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Deletion branch: if DeletionTimestamp is set, run cleanup and return.
	if !mr.DeletionTimestamp.IsZero() {
		err := handleDeletion(ctx, params, &mr)
		opmmetrics.RecordDuration(mr.Name, mr.Namespace, time.Since(reconcileStart))
		return ctrl.Result{}, err
	}

	// Create serial patcher for status patching.
	patcher := patch.NewSerialPatcher(&mr, params.Client)

	// Suspend check — runs before deferred status commit to preserve existing status fields.
	if mr.Spec.Suspend {
		log.Info("Reconciliation is suspended")
		status.MarkSuspended(&mr)
		mr.Status.ObservedGeneration = mr.Generation
		params.EventRecorder.Eventf(&mr, nil, corev1.EventTypeNormal, status.SuspendedReason, "Suspend", "Reconciliation is suspended")
		if patchErr := patcher.Patch(ctx, &mr,
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
			return ctrl.Result{}, patchErr
		}
		opmmetrics.RecordDuration(mr.Name, mr.Namespace, time.Since(reconcileStart))
		return ctrl.Result{}, nil
	}

	// Check for resume from suspend.
	if ready := apimeta.FindStatusCondition(mr.Status.Conditions, status.ReadyCondition); ready != nil && ready.Reason == status.SuspendedReason {
		log.Info("Reconciliation resumed")
		params.EventRecorder.Eventf(&mr, nil, corev1.EventTypeNormal, status.ResumedReason, "Resume", "Reconciliation resumed")
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
			status.MarkReady(&mr, "Reconciliation succeeded")
			updateFailureCounters(&mr.Status, outcome, phases)
			mr.Status.NextRetryAt = nil
			if patchErr := patcher.Patch(ctx, &mr,
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
			recordReconcileMetrics(mr.Name, mr.Namespace, outcome, time.Since(reconcileStart), false, 0)
			opmmetrics.RecordDuration(mr.Name, mr.Namespace, time.Since(reconcileStart))
			return
		}

		now := metav1.Now()
		mr.Status.ObservedGeneration = mr.Generation
		mr.Status.LastAttemptedAction = "reconcile"
		mr.Status.LastAttemptedAt = &now
		duration := metav1.Duration{Duration: time.Since(reconcileStart)}
		mr.Status.LastAttemptedDuration = &duration
		mr.Status.LastAttemptedSourceDigest = digests.Source
		mr.Status.LastAttemptedConfigDigest = digests.Config
		mr.Status.LastAttemptedRenderDigest = digests.Render

		if reconciled {
			mr.Status.LastAppliedAt = &now
			mr.Status.LastAppliedSourceDigest = digests.Source
			mr.Status.LastAppliedConfigDigest = digests.Config
			mr.Status.LastAppliedRenderDigest = digests.Render

			invDigest := inventory.ComputeDigest(newEntries)
			rev := int64(1)
			if mr.Status.Inventory != nil {
				rev = mr.Status.Inventory.Revision + 1
			}
			mr.Status.Inventory = &releasesv1alpha1.Inventory{
				Revision: rev,
				Digest:   invDigest,
				Count:    int64(len(newEntries)),
				Entries:  newEntries,
			}
			digests.Inventory = invDigest

			entry := status.NewSuccessEntry("reconcile", "complete", digests, int64(len(newEntries)))
			status.RecordHistory(&mr.Status, entry)
		} else if errMsg != "" {
			entry := status.NewFailureEntry("reconcile", errMsg, digests)
			status.RecordHistory(&mr.Status, entry)
		}
		// NoOp does not record history (per design doc).

		// Update failure counters based on phase outcomes.
		updateFailureCounters(&mr.Status, outcome, phases)

		// Set or clear NextRetryAt based on outcome.
		if retryAfter > 0 {
			retryTime := metav1.NewTime(time.Now().Add(retryAfter))
			mr.Status.NextRetryAt = &retryTime
		} else {
			mr.Status.NextRetryAt = nil
		}

		// Record reconcile metrics.
		recordReconcileMetrics(mr.Name, mr.Namespace, outcome, time.Since(reconcileStart), reconciled, len(newEntries))

		if patchErr := patcher.Patch(ctx, &mr,
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
			log.Error(patchErr, "Failed to patch ModuleRelease status")
		}
	}()

	// Mark reconciling at the start.
	status.MarkReconciling(&mr, "Progressing", "Reconciliation in progress")

	// Compute source and config digests early for no-op detection.
	// Source digest is derived from the module path + version (replaces Flux artifact digest).
	digests.Source = status.ModuleSourceDigest(mr.Spec.Module.Path, mr.Spec.Module.Version)
	digests.Config = status.ConfigDigest(mr.Spec.Values)

	// Phase 1: Synthesize, resolve, and render module from OCI registry.
	// CUE's native module system resolves the target module from the registry.
	renderResult, err := params.Renderer.RenderModule(
		ctx,
		mr.Name, mr.Namespace,
		mr.Spec.Module.Path, mr.Spec.Module.Version,
		mr.Spec.Values,
		params.Provider,
	)
	if err != nil {
		reason := status.RenderFailedReason
		if isResolutionError(err) {
			reason = status.ResolutionFailedReason
		}
		params.EventRecorder.Eventf(&mr, nil, corev1.EventTypeWarning, reason, "Render", "%s", err)
		status.MarkStalled(&mr, reason, "%s", err)
		outcome = FailedStalled
		errMsg = err.Error()
		retryAfter = StalledRecheckInterval
		return ctrl.Result{RequeueAfter: retryAfter}, nil
	}

	status.MarkModuleResolved(&mr, fmt.Sprintf("%s@%s", mr.Spec.Module.Path, mr.Spec.Module.Version))

	renderDigest, err := status.RenderDigest(renderResult.Resources)
	if err != nil {
		status.MarkStalled(&mr, status.RenderFailedReason, "computing render digest: %s", err)
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
		status.MarkStalled(&mr, status.ApplyFailedReason, "converting resources: %s", err)
		outcome = FailedStalled
		errMsg = fmt.Sprintf("converting resources: %s", err)
		retryAfter = StalledRecheckInterval
		return ctrl.Result{RequeueAfter: retryAfter}, nil
	}

	lastApplied := status.DigestSet{
		Source:    mr.Status.LastAppliedSourceDigest,
		Config:    mr.Status.LastAppliedConfigDigest,
		Render:    mr.Status.LastAppliedRenderDigest,
		Inventory: inventoryDigest(mr.Status.Inventory),
	}

	isNoOp := status.IsNoOp(digests, lastApplied)

	// Drift detection runs on every reconcile, including no-ops.
	// Uses SSA dry-run to compare desired state against live cluster state.
	phases.driftRan = true
	phases.driftFailed = detectDrift(ctx, params.ResourceManager, &mr, resources)

	if isNoOp {
		log.Info("No changes detected, skipping apply")
		params.EventRecorder.Eventf(&mr, nil, corev1.EventTypeNormal, status.NoOpReason, "Reconcile", "No changes detected")
		outcome = NoOp
		return ctrl.Result{}, nil
	}

	var previousEntries []releasesv1alpha1.InventoryEntry
	if mr.Status.Inventory != nil {
		previousEntries = mr.Status.Inventory.Entries
	}
	staleSet := inventory.ComputeStaleSet(previousEntries, renderResult.InventoryEntries)

	// Build impersonated client and resource manager if serviceAccountName is set.
	// Apply and prune use the impersonated identity; all other phases use the controller's own client.
	applyRM, applyClient, impErr := buildApplyClient(ctx, params, &mr)
	if impErr != nil {
		status.MarkStalled(&mr, status.ImpersonationFailedReason, "%s", impErr)
		outcome = FailedStalled
		errMsg = impErr.Error()
		retryAfter = StalledRecheckInterval
		return ctrl.Result{RequeueAfter: retryAfter}, nil
	}

	// Phase 5: Apply resources.
	phases.applyRan = true
	force := mr.Spec.Rollout != nil && mr.Spec.Rollout.ForceConflicts
	applyResult, err := apply.Apply(ctx, applyRM, resources, force)
	if err != nil {
		phases.applyFailed = true
		params.EventRecorder.Eventf(&mr, nil, corev1.EventTypeWarning, status.ApplyFailedReason, "Apply", "%s", err)
		if mr.Spec.ServiceAccountName != "" && isForbidden(err) {
			status.MarkStalled(&mr, status.ImpersonationFailedReason, "%s", err)
			outcome = FailedStalled
			errMsg = err.Error()
			retryAfter = StalledRecheckInterval
			return ctrl.Result{RequeueAfter: retryAfter}, nil
		}
		status.MarkNotReady(&mr, status.ApplyFailedReason, "%s", err)
		outcome = FailedTransient
		errMsg = err.Error()
		retryAfter = ComputeBackoff(reconcileFailureCount(mr.Status.FailureCounters) + 1)
		return ctrl.Result{RequeueAfter: retryAfter}, nil
	}

	total := applyResult.Created + applyResult.Updated + applyResult.Unchanged
	params.EventRecorder.Eventf(&mr, nil, corev1.EventTypeNormal, status.AppliedReason, "Apply",
		"Applied %d resources (%d created, %d updated, %d unchanged)",
		total, applyResult.Created, applyResult.Updated, applyResult.Unchanged)

	log.Info("Applied resources",
		"created", applyResult.Created, "updated", applyResult.Updated, "unchanged", applyResult.Unchanged)

	// Record apply metrics.
	opmmetrics.RecordApply(mr.Name, mr.Namespace, applyResult.Created, applyResult.Updated, applyResult.Unchanged)

	// Successful apply resolves any drift.
	status.ClearDrifted(&mr)

	newEntries = renderResult.InventoryEntries

	// Phase 6: Prune stale resources (only if spec.prune=true and apply succeeded).
	phases.pruneRan = true
	var pruneDeleted int
	outcome, reconciled, pruneDeleted, err = pruneStaleResources(ctx, &mr, applyClient, staleSet, params.EventRecorder)
	if err != nil {
		phases.pruneFailed = true
		errMsg = err.Error()
		retryAfter = ComputeBackoff(reconcileFailureCount(mr.Status.FailureCounters) + 1)
		return ctrl.Result{RequeueAfter: retryAfter}, nil
	}
	if !reconciled {
		phases.pruneFailed = true
		retryAfter = StalledRecheckInterval
		return ctrl.Result{RequeueAfter: retryAfter}, nil
	}

	// Record prune metrics.
	opmmetrics.RecordPrune(mr.Name, mr.Namespace, pruneDeleted)

	// Phase 7: Commit status (handled by deferred function).
	status.MarkReady(&mr, "Reconciliation succeeded")
	params.EventRecorder.Eventf(&mr, nil, corev1.EventTypeNormal, status.ReconciliationSucceededReason, "Reconcile", "Reconciliation succeeded")
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
	mrStatus *releasesv1alpha1.ModuleReleaseStatus,
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
	mr *releasesv1alpha1.ModuleRelease,
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
		status.MarkDrifted(mr, len(driftResult.Resources))
	} else {
		status.ClearDrifted(mr)
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

// handleDeletion runs the deletion cleanup path.
// If spec.prune is true, all inventory entries are pruned (respecting safety exclusions).
// On success (or prune disabled), the finalizer is removed.
// On partial failure, the finalizer is retained and the error is returned for requeue.
func handleDeletion(
	ctx context.Context,
	params *ModuleReleaseParams,
	mr *releasesv1alpha1.ModuleRelease,
) error {
	log := logf.FromContext(ctx)
	log.Info("Running deletion cleanup for ModuleRelease")

	if mr.Spec.Prune && mr.Status.Inventory != nil && len(mr.Status.Inventory.Entries) > 0 {
		deleteClient := params.Client
		if mr.Spec.ServiceAccountName != "" && params.RestConfig != nil {
			impClient, impErr := apply.NewImpersonatedClient(ctx, params.RestConfig, params.APIReader, params.Client.Scheme(), mr.Namespace, mr.Spec.ServiceAccountName)
			if impErr != nil {
				log.Info("ServiceAccount unavailable for deletion cleanup, using controller client",
					"serviceAccount", mr.Spec.ServiceAccountName, "error", impErr)
			} else {
				deleteClient = impClient
			}
		}
		pruneResult, err := apply.Prune(ctx, deleteClient, mr.Status.Inventory.Entries)
		if err != nil {
			log.Error(err, "Partial failure during deletion cleanup, retaining finalizer")
			return err
		}
		log.Info("Deletion cleanup pruned resources",
			"deleted", pruneResult.Deleted, "skipped", pruneResult.Skipped)
	} else if !mr.Spec.Prune {
		log.Info("Prune disabled, orphaning managed resources on deletion")
	}

	if err := removeFinalizer(ctx, params.Client, mr); err != nil {
		return fmt.Errorf("removing finalizer: %w", err)
	}
	log.Info("Finalizer removed, deletion can proceed")

	return nil
}

// addFinalizer adds the cleanup finalizer to the ModuleRelease and patches it.
func addFinalizer(ctx context.Context, c client.Client, mr *releasesv1alpha1.ModuleRelease) error {
	mergePatch := client.MergeFrom(mr.DeepCopy())
	controllerutil.AddFinalizer(mr, FinalizerName)
	return c.Patch(ctx, mr, mergePatch)
}

// removeFinalizer removes the cleanup finalizer from the ModuleRelease and patches it.
func removeFinalizer(ctx context.Context, c client.Client, mr *releasesv1alpha1.ModuleRelease) error {
	mergePatch := client.MergeFrom(mr.DeepCopy())
	controllerutil.RemoveFinalizer(mr, FinalizerName)
	return c.Patch(ctx, mr, mergePatch)
}

// pruneStaleResources runs Phase 6: prune stale resources if spec.prune is true and stale resources exist.
// Emits prune events via the provided recorder. Returns the outcome, whether reconcile succeeded,
// the number of resources deleted, and any error.
func pruneStaleResources(
	ctx context.Context,
	mr *releasesv1alpha1.ModuleRelease,
	c client.Client,
	staleSet []releasesv1alpha1.InventoryEntry,
	recorder events.EventRecorder,
) (Outcome, bool, int, error) {
	if !mr.Spec.Prune || len(staleSet) == 0 {
		return Applied, true, 0, nil
	}
	log := logf.FromContext(ctx)
	pruneResult, err := apply.Prune(ctx, c, staleSet)
	if err != nil {
		recorder.Eventf(mr, nil, corev1.EventTypeWarning, status.PruneFailedReason, "Prune", "%s", err)
		if mr.Spec.ServiceAccountName != "" && isForbidden(err) {
			status.MarkStalled(mr, status.ImpersonationFailedReason, "%s", err)
			return FailedStalled, false, 0, nil
		}
		status.MarkNotReady(mr, status.PruneFailedReason, "%s", err)
		return FailedTransient, false, 0, err
	}
	if pruneResult.Deleted > 0 {
		recorder.Eventf(mr, nil, corev1.EventTypeNormal, status.PrunedReason, "Prune",
			"Pruned %d stale resources", pruneResult.Deleted)
	}
	log.Info("Pruned stale resources", "deleted", pruneResult.Deleted, "skipped", pruneResult.Skipped)
	return AppliedAndPruned, true, pruneResult.Deleted, nil
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
// If serviceAccountName is set, it builds an impersonated client; otherwise it returns the defaults.
func buildApplyClient(
	ctx context.Context,
	params *ModuleReleaseParams,
	mr *releasesv1alpha1.ModuleRelease,
) (*fluxssa.ResourceManager, client.Client, error) {
	if mr.Spec.ServiceAccountName == "" {
		return params.ResourceManager, params.Client, nil
	}
	log := logf.FromContext(ctx)
	log.Info("Building impersonated client", "serviceAccount", mr.Spec.ServiceAccountName)
	impClient, err := apply.NewImpersonatedClient(ctx, params.RestConfig, params.APIReader, params.Client.Scheme(), mr.Namespace, mr.Spec.ServiceAccountName)
	if err != nil {
		return nil, nil, err
	}
	return apply.NewResourceManager(impClient, "opm-controller"), impClient, nil
}

// recordReconcileMetrics records outcome, duration, and inventory size metrics.
func recordReconcileMetrics(name, namespace string, outcome Outcome, duration time.Duration, reconciled bool, inventoryCount int) {
	opmmetrics.RecordReconcile(name, namespace, outcome.MetricLabel(), duration)
	if reconciled {
		opmmetrics.SetInventorySize(name, namespace, inventoryCount)
	}
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
