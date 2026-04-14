package reconcile

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	fluxssa "github.com/fluxcd/pkg/ssa"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/fluxcd/pkg/runtime/patch"

	releasesv1alpha1 "github.com/open-platform-model/poc-controller/api/v1alpha1"
	"github.com/open-platform-model/poc-controller/internal/apply"
	"github.com/open-platform-model/poc-controller/internal/inventory"
	"github.com/open-platform-model/poc-controller/internal/render"
	"github.com/open-platform-model/poc-controller/internal/source"
	"github.com/open-platform-model/poc-controller/internal/status"
	"github.com/open-platform-model/poc-controller/pkg/core"
	"github.com/open-platform-model/poc-controller/pkg/provider"
)

const (
	// FinalizerName is the finalizer registered on ModuleRelease resources
	// to ensure owned resources are cleaned up before deletion completes.
	FinalizerName = "releases.opmodel.dev/cleanup"

	// softBlockedRequeue is the requeue delay for SoftBlocked outcomes.
	softBlockedRequeue = 30 * time.Second
)

// ModuleReleaseParams holds the dependencies injected into the reconcile loop.
type ModuleReleaseParams struct {
	Client          client.Client
	RestConfig      *rest.Config
	Provider        *provider.Provider
	ResourceManager *fluxssa.ResourceManager
	ArtifactFetcher source.Fetcher
	EventRecorder   record.EventRecorder
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

	// Register finalizer if not present.
	// The patch triggers a watch event, so no explicit requeue is needed.
	if !controllerutil.ContainsFinalizer(&mr, FinalizerName) {
		log.Info("Adding finalizer to ModuleRelease")
		if err := addFinalizer(ctx, params.Client, &mr); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
		return ctrl.Result{}, nil
	}

	// Deletion branch: if DeletionTimestamp is set, run cleanup and return.
	if !mr.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, handleDeletion(ctx, params, &mr)
	}

	// Create serial patcher for status patching.
	patcher := patch.NewSerialPatcher(&mr, params.Client)

	// Suspend check — runs before deferred status commit to preserve existing status fields.
	if mr.Spec.Suspend {
		log.Info("Reconciliation is suspended")
		status.MarkSuspended(&mr)
		mr.Status.ObservedGeneration = mr.Generation
		params.EventRecorder.Event(&mr, corev1.EventTypeNormal, status.SuspendedReason, "Reconciliation is suspended")
		if patchErr := patcher.Patch(ctx, &mr,
			patch.WithOwnedConditions{
				Conditions: []string{
					status.ReadyCondition,
					status.ReconcilingCondition,
					status.StalledCondition,
					status.SourceReadyCondition,
					status.DriftedCondition,
				},
			},
			patch.WithStatusObservedGeneration{},
		); patchErr != nil {
			return ctrl.Result{}, patchErr
		}
		return ctrl.Result{}, nil
	}

	// Check for resume from suspend.
	if ready := apimeta.FindStatusCondition(mr.Status.Conditions, status.ReadyCondition); ready != nil && ready.Reason == status.SuspendedReason {
		log.Info("Reconciliation resumed")
		params.EventRecorder.Event(&mr, corev1.EventTypeNormal, status.ResumedReason, "Reconciliation resumed")
	}

	// Track reconcile start time for duration calculation.
	reconcileStart := time.Now()

	// Track digests and outcome across phases for deferred status commit.
	var (
		outcome    Outcome
		digests    status.DigestSet
		reconciled bool // true if apply (and optional prune) succeeded
		newEntries []releasesv1alpha1.InventoryEntry
		errMsg     string

		// Phase outcome tracking for failure counters (updated in Phase 7).
		phases phaseOutcomes
	)

	// Deferred status commit — always patches status regardless of outcome.
	defer func() {
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

		if patchErr := patcher.Patch(ctx, &mr,
			patch.WithOwnedConditions{
				Conditions: []string{
					status.ReadyCondition,
					status.ReconcilingCondition,
					status.StalledCondition,
					status.SourceReadyCondition,
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

	// Phase 1: Resolve source.
	artifactRef, err := source.Resolve(ctx, params.Client, mr.Spec.SourceRef, mr.Namespace)
	if err != nil {
		return sourceError(&mr, err, &outcome, &errMsg, params.EventRecorder)
	}

	status.MarkSourceReady(&mr, artifactRef.Revision)
	mr.Status.Source = &releasesv1alpha1.SourceStatus{
		Ref:              &mr.Spec.SourceRef,
		ArtifactRevision: artifactRef.Revision,
		ArtifactDigest:   artifactRef.Digest,
		ArtifactURL:      artifactRef.URL,
	}

	// Compute source and config digests early for no-op detection.
	digests.Source = status.SourceDigest(artifactRef.Digest)
	digests.Config = status.ConfigDigest(mr.Spec.Values)

	// Phase 2: Fetch and unpack artifact.
	dir, err := os.MkdirTemp("", "opm-artifact-*")
	if err != nil {
		status.MarkNotReady(&mr, status.ArtifactFetchFailedReason, "creating temp dir: %s", err)
		outcome = FailedTransient
		errMsg = fmt.Sprintf("creating temp dir: %s", err)
		return ctrl.Result{}, err
	}
	defer func() {
		if removeErr := os.RemoveAll(dir); removeErr != nil {
			logf.FromContext(ctx).Error(removeErr, "Failed to remove temp dir", "dir", dir)
		}
	}()

	if err := params.ArtifactFetcher.Fetch(ctx, artifactRef.URL, artifactRef.Digest, dir); err != nil {
		params.EventRecorder.Eventf(&mr, corev1.EventTypeWarning, status.ArtifactFetchFailedReason, "%s", err)
		if errors.Is(err, source.ErrMissingCUEModule) {
			status.MarkStalled(&mr, status.ArtifactInvalidReason, "%s", err)
			outcome = FailedStalled
			errMsg = err.Error()
			return ctrl.Result{}, nil
		}
		status.MarkNotReady(&mr, status.ArtifactFetchFailedReason, "%s", err)
		outcome = FailedTransient
		errMsg = err.Error()
		return ctrl.Result{}, err
	}

	// Phase 3: Render module, compute digests.
	renderResult, err := render.RenderModule(ctx, dir, mr.Spec.Values, params.Provider)
	if err != nil {
		params.EventRecorder.Eventf(&mr, corev1.EventTypeWarning, status.RenderFailedReason, "%s", err)
		status.MarkStalled(&mr, status.RenderFailedReason, "%s", err)
		outcome = FailedStalled
		errMsg = err.Error()
		return ctrl.Result{}, nil
	}

	renderDigest, err := status.RenderDigest(renderResult.Resources)
	if err != nil {
		status.MarkStalled(&mr, status.RenderFailedReason, "computing render digest: %s", err)
		outcome = FailedStalled
		errMsg = fmt.Sprintf("computing render digest: %s", err)
		return ctrl.Result{}, nil
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
		return ctrl.Result{}, nil
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
		status.MarkReady(&mr, "No changes detected")
		params.EventRecorder.Event(&mr, corev1.EventTypeNormal, status.NoOpReason, "No changes detected")
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
		return ctrl.Result{}, nil
	}

	// Phase 5: Apply resources.
	phases.applyRan = true
	force := mr.Spec.Rollout != nil && mr.Spec.Rollout.ForceConflicts
	applyResult, err := apply.Apply(ctx, applyRM, resources, force)
	if err != nil {
		phases.applyFailed = true
		params.EventRecorder.Eventf(&mr, corev1.EventTypeWarning, status.ApplyFailedReason, "%s", err)
		if mr.Spec.ServiceAccountName != "" && isForbidden(err) {
			status.MarkStalled(&mr, status.ImpersonationFailedReason, "%s", err)
			outcome = FailedStalled
			errMsg = err.Error()
			return ctrl.Result{}, nil
		}
		status.MarkNotReady(&mr, status.ApplyFailedReason, "%s", err)
		outcome = FailedTransient
		errMsg = err.Error()
		return ctrl.Result{}, err
	}

	total := applyResult.Created + applyResult.Updated + applyResult.Unchanged
	params.EventRecorder.Eventf(&mr, corev1.EventTypeNormal, status.AppliedReason,
		"Applied %d resources (%d created, %d updated, %d unchanged)",
		total, applyResult.Created, applyResult.Updated, applyResult.Unchanged)

	log.Info("Applied resources",
		"created", applyResult.Created, "updated", applyResult.Updated, "unchanged", applyResult.Unchanged)

	// Successful apply resolves any drift.
	status.ClearDrifted(&mr)

	newEntries = renderResult.InventoryEntries

	// Phase 6: Prune stale resources (only if spec.prune=true and apply succeeded).
	phases.pruneRan = true
	outcome, reconciled, err = pruneStaleResources(ctx, &mr, applyClient, staleSet, params.EventRecorder)
	if err != nil {
		phases.pruneFailed = true
		errMsg = err.Error()
		return ctrl.Result{}, err
	}
	if !reconciled {
		phases.pruneFailed = true
	}

	// Phase 7: Commit status (handled by deferred function).
	status.MarkReady(&mr, "Reconciliation succeeded")
	params.EventRecorder.Event(&mr, corev1.EventTypeNormal, status.ReconciliationSucceededReason, "Reconciliation succeeded")
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
			impClient, impErr := apply.NewImpersonatedClient(ctx, params.RestConfig, params.Client, mr.Namespace, mr.Spec.ServiceAccountName)
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
// Emits prune events via the provided recorder. Returns the outcome, whether reconcile succeeded, and any error.
func pruneStaleResources(
	ctx context.Context,
	mr *releasesv1alpha1.ModuleRelease,
	c client.Client,
	staleSet []releasesv1alpha1.InventoryEntry,
	recorder record.EventRecorder,
) (Outcome, bool, error) {
	if !mr.Spec.Prune || len(staleSet) == 0 {
		return Applied, true, nil
	}
	log := logf.FromContext(ctx)
	pruneResult, err := apply.Prune(ctx, c, staleSet)
	if err != nil {
		recorder.Eventf(mr, corev1.EventTypeWarning, status.PruneFailedReason, "%s", err)
		if mr.Spec.ServiceAccountName != "" && isForbidden(err) {
			status.MarkStalled(mr, status.ImpersonationFailedReason, "%s", err)
			return FailedStalled, false, nil
		}
		status.MarkNotReady(mr, status.PruneFailedReason, "%s", err)
		return FailedTransient, false, err
	}
	if pruneResult.Deleted > 0 {
		recorder.Eventf(mr, corev1.EventTypeNormal, status.PrunedReason,
			"Pruned %d stale resources", pruneResult.Deleted)
	}
	log.Info("Pruned stale resources", "deleted", pruneResult.Deleted, "skipped", pruneResult.Skipped)
	return AppliedAndPruned, true, nil
}

// sourceError classifies a source resolution error, sets conditions, emits
// events, and writes the outcome and error message into the caller's deferred-captured variables.
func sourceError(
	mr *releasesv1alpha1.ModuleRelease,
	err error,
	outcome *Outcome,
	errMsg *string,
	recorder record.EventRecorder,
) (ctrl.Result, error) {
	*errMsg = err.Error()
	if errors.Is(err, source.ErrSourceNotReady) {
		recorder.Eventf(mr, corev1.EventTypeWarning, status.SourceNotReadyReason, "%s", err)
		status.MarkSourceNotReady(mr, status.SourceNotReadyReason, "%s", err)
		*outcome = SoftBlocked
		return ctrl.Result{RequeueAfter: softBlockedRequeue}, nil
	}
	if errors.Is(err, source.ErrSourceNotFound) {
		status.MarkSourceNotReady(mr, status.SourceUnavailableReason, "%s", err)
		status.MarkStalled(mr, status.SourceUnavailableReason, "%s", err)
		*outcome = FailedStalled
		return ctrl.Result{}, nil
	}
	// Transient error (e.g., API server unreachable).
	status.MarkSourceNotReady(mr, status.SourceUnavailableReason, "%s", err)
	status.MarkNotReady(mr, status.SourceUnavailableReason, "%s", err)
	*outcome = FailedTransient
	return ctrl.Result{}, err
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
	impClient, err := apply.NewImpersonatedClient(ctx, params.RestConfig, params.Client, mr.Namespace, mr.Spec.ServiceAccountName)
	if err != nil {
		return nil, nil, err
	}
	return apply.NewResourceManager(impClient, "opm-controller"), impClient, nil
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
