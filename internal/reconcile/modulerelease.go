package reconcile

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	fluxssa "github.com/fluxcd/pkg/ssa"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	// softBlockedRequeue is the requeue delay for SoftBlocked outcomes.
	softBlockedRequeue = 30 * time.Second
)

// ModuleReleaseParams holds the dependencies injected into the reconcile loop.
type ModuleReleaseParams struct {
	Client          client.Client
	Provider        *provider.Provider
	ResourceManager *fluxssa.ResourceManager
	ArtifactFetcher source.Fetcher
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

	// Skip deleted objects (finalizer handling is a separate change).
	if !mr.DeletionTimestamp.IsZero() {
		log.Info("ModuleRelease is being deleted, skipping reconciliation")
		return ctrl.Result{}, nil
	}

	// Create serial patcher for deferred status commit.
	patcher := patch.NewSerialPatcher(&mr, params.Client)

	// Track reconcile start time for duration calculation.
	reconcileStart := time.Now()

	// Track digests and outcome across phases for deferred status commit.
	var (
		outcome    Outcome
		digests    status.DigestSet
		reconciled bool // true if apply (and optional prune) succeeded
		newEntries []releasesv1alpha1.InventoryEntry
		errMsg     string
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

		if patchErr := patcher.Patch(ctx, &mr,
			patch.WithOwnedConditions{
				Conditions: []string{
					status.ReadyCondition,
					status.ReconcilingCondition,
					status.StalledCondition,
					status.SourceReadyCondition,
				},
			},
			patch.WithStatusObservedGeneration{},
		); patchErr != nil {
			log.Error(patchErr, "Failed to patch ModuleRelease status")
		}
	}()

	// Suspend check.
	if mr.Spec.Suspend {
		log.Info("ModuleRelease is suspended, skipping reconciliation")
		status.MarkReconciling(&mr, status.SuspendedReason, "Reconciliation is suspended")
		outcome = SoftBlocked
		return ctrl.Result{}, nil
	}

	// Mark reconciling at the start.
	status.MarkReconciling(&mr, "Progressing", "Reconciliation in progress")

	// Phase 1: Resolve source.
	artifactRef, err := source.Resolve(ctx, params.Client, mr.Spec.SourceRef, mr.Namespace)
	if err != nil {
		if errors.Is(err, source.ErrSourceNotReady) {
			status.MarkSourceNotReady(&mr, status.SourceNotReadyReason, "%s", err)
			outcome = SoftBlocked
			errMsg = err.Error()
			return ctrl.Result{RequeueAfter: softBlockedRequeue}, nil
		}
		if errors.Is(err, source.ErrSourceNotFound) {
			status.MarkSourceNotReady(&mr, status.SourceUnavailableReason, "%s", err)
			status.MarkStalled(&mr, status.SourceUnavailableReason, "%s", err)
			outcome = FailedStalled
			errMsg = err.Error()
			return ctrl.Result{}, nil
		}
		// Transient error (e.g., API server unreachable).
		status.MarkSourceNotReady(&mr, status.SourceUnavailableReason, "%s", err)
		status.MarkNotReady(&mr, status.SourceUnavailableReason, "%s", err)
		outcome = FailedTransient
		errMsg = err.Error()
		return ctrl.Result{}, err
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

	// Phase 4: Plan actions — no-op detection, compute stale set.
	lastApplied := status.DigestSet{
		Source:    mr.Status.LastAppliedSourceDigest,
		Config:    mr.Status.LastAppliedConfigDigest,
		Render:    mr.Status.LastAppliedRenderDigest,
		Inventory: inventoryDigest(mr.Status.Inventory),
	}

	if status.IsNoOp(digests, lastApplied) {
		log.Info("No changes detected, skipping apply")
		status.MarkReady(&mr, "No changes detected")
		outcome = NoOp
		return ctrl.Result{}, nil
	}

	var previousEntries []releasesv1alpha1.InventoryEntry
	if mr.Status.Inventory != nil {
		previousEntries = mr.Status.Inventory.Entries
	}
	staleSet := inventory.ComputeStaleSet(previousEntries, renderResult.InventoryEntries)

	// Phase 5: Apply resources.
	resources, err := toUnstructuredSlice(renderResult.Resources)
	if err != nil {
		status.MarkStalled(&mr, status.ApplyFailedReason, "converting resources: %s", err)
		outcome = FailedStalled
		errMsg = fmt.Sprintf("converting resources: %s", err)
		return ctrl.Result{}, nil
	}

	force := mr.Spec.Rollout != nil && mr.Spec.Rollout.ForceConflicts
	applyResult, err := apply.Apply(ctx, params.ResourceManager, resources, force)
	if err != nil {
		status.MarkNotReady(&mr, status.ApplyFailedReason, "%s", err)
		outcome = FailedTransient
		errMsg = err.Error()
		return ctrl.Result{}, err
	}

	log.Info("Applied resources",
		"created", applyResult.Created, "updated", applyResult.Updated, "unchanged", applyResult.Unchanged)

	newEntries = renderResult.InventoryEntries

	// Phase 6: Prune stale resources (only if spec.prune=true and apply succeeded).
	if mr.Spec.Prune && len(staleSet) > 0 {
		pruneResult, err := apply.Prune(ctx, params.Client, staleSet)
		if err != nil {
			status.MarkNotReady(&mr, status.PruneFailedReason, "%s", err)
			outcome = FailedTransient
			errMsg = err.Error()
			// Apply succeeded but prune failed — do NOT update inventory.
			reconciled = false
			return ctrl.Result{}, err
		}
		log.Info("Pruned stale resources", "deleted", pruneResult.Deleted, "skipped", pruneResult.Skipped)
		reconciled = true
		outcome = AppliedAndPruned
	} else {
		reconciled = true
		outcome = Applied
	}

	// Phase 7: Commit status (handled by deferred function).
	status.MarkReady(&mr, "Reconciliation succeeded")
	log.Info("Reconciliation complete", "outcome", outcome.String())

	return ctrl.Result{}, nil
}

// inventoryDigest returns the digest from the inventory, or empty string if nil.
func inventoryDigest(inv *releasesv1alpha1.Inventory) string {
	if inv == nil {
		return ""
	}
	return inv.Digest
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
