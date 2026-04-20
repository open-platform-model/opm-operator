package apply

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/pkg/core"
)

// PruneResult carries counts of prune outcomes.
type PruneResult struct {
	// Deleted is the number of stale resources successfully deleted.
	Deleted int

	// Skipped is the number of stale resources skipped due to safety exclusions.
	Skipped int
}

// Prune deletes stale resources from the cluster.
// Uses direct client.Delete per resource rather than Flux's DeleteAll to allow
// per-resource error control and safety exclusion logic (design decision 1).
//
// Safety exclusions (design decision 3: hard-coded, not configurable):
//   - Namespace: never auto-deleted (cascades to all resources inside)
//   - CustomResourceDefinition: never auto-deleted (deletes all instances globally)
//
// Live-state ownership guard (defense-in-depth): before each delete, Prune
// GETs the live object and skips the delete if the live object is not
// OPM-managed (missing/unrecognized app.kubernetes.io/managed-by label) or
// carries a module-release.opmodel.dev/uuid label that disagrees with
// ownerUUID. An empty live UUID label is tolerated (legacy resources predate
// UUID stamping). An empty ownerUUID disables the UUID comparison — callers
// that cannot supply a UUID (e.g. the Release reconciler, or a freshly-created
// ModuleRelease whose Status.ReleaseUUID is not yet persisted) fall back to
// the managed-by check alone.
//
// Skipped resources are logged as warnings and counted in PruneResult.Skipped.
//
// If a stale resource is already gone (NotFound), it is treated as success.
// Individual failures (Get or Delete) are collected and returned as a joined
// error; remaining entries continue (design decision 2: continue-on-error /
// fail-slow).
//
// The caller is responsible for:
//   - Computing the stale set via internal/inventory.ComputeStaleSet
//   - Checking spec.prune before calling this function
//   - Ensuring apply succeeded before calling prune
//   - Supplying ownerUUID from the freshly-rendered resources or
//     ModuleReleaseStatus.ReleaseUUID
func Prune(
	ctx context.Context,
	c client.Client,
	ownerUUID string,
	stale []releasesv1alpha1.InventoryEntry,
) (*PruneResult, error) {
	log := logf.FromContext(ctx)
	result := &PruneResult{}

	var errs []error
	for _, entry := range stale {
		if !isSafeToDelete(entry) {
			log.Info("Skipping safety-excluded resource from pruning",
				"kind", entry.Kind, "namespace", entry.Namespace, "name", entry.Name)
			result.Skipped++
			continue
		}

		live := &unstructured.Unstructured{}
		live.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   entry.Group,
			Version: entry.Version,
			Kind:    entry.Kind,
		})
		getErr := c.Get(ctx, types.NamespacedName{
			Namespace: entry.Namespace,
			Name:      entry.Name,
		}, live)
		if getErr != nil {
			if apierrors.IsNotFound(getErr) {
				log.V(1).Info("Stale resource already deleted",
					"kind", entry.Kind, "namespace", entry.Namespace, "name", entry.Name)
				continue
			}
			errs = append(errs, fmt.Errorf("failed to get %s/%s %s: %w",
				entry.Namespace, entry.Name, entry.Kind, getErr))
			continue
		}

		liveLabels := live.GetLabels()
		if !core.IsOPMManagedBy(liveLabels[core.LabelManagedBy]) {
			log.Info("Skipping prune: live resource is not OPM-managed",
				"kind", entry.Kind, "namespace", entry.Namespace, "name", entry.Name,
				"managedBy", liveLabels[core.LabelManagedBy])
			result.Skipped++
			continue
		}

		liveUUID := liveLabels[core.LabelModuleReleaseUUID]
		if ownerUUID != "" && liveUUID != "" && liveUUID != ownerUUID {
			log.Info("Skipping prune: live resource release UUID does not match owner",
				"kind", entry.Kind, "namespace", entry.Namespace, "name", entry.Name,
				"ownerUUID", ownerUUID, "liveUUID", liveUUID)
			result.Skipped++
			continue
		}

		if err := c.Delete(ctx, live); err != nil {
			if apierrors.IsNotFound(err) {
				log.V(1).Info("Stale resource already deleted",
					"kind", entry.Kind, "namespace", entry.Namespace, "name", entry.Name)
				continue
			}
			errs = append(errs, fmt.Errorf("failed to delete %s/%s %s: %w",
				entry.Namespace, entry.Name, entry.Kind, err))
			continue
		}

		log.Info("Pruned stale resource",
			"kind", entry.Kind, "namespace", entry.Namespace, "name", entry.Name)
		result.Deleted++
	}

	return result, errors.Join(errs...)
}

// isSafeToDelete returns false for Namespace and CustomResourceDefinition kinds.
func isSafeToDelete(entry releasesv1alpha1.InventoryEntry) bool {
	switch entry.Kind {
	case "Namespace", "CustomResourceDefinition":
		return false
	default:
		return true
	}
}
