package apply

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	releasesv1alpha1 "github.com/open-platform-model/poc-controller/api/v1alpha1"
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
// Skipped resources are logged as warnings and counted in PruneResult.Skipped.
//
// If a stale resource is already gone (NotFound), it is treated as success.
// Individual delete failures are collected and returned as a joined error;
// remaining deletes continue (design decision 2: continue-on-error / fail-slow).
//
// The caller is responsible for:
//   - Computing the stale set via internal/inventory.ComputeStaleSet
//   - Checking spec.prune before calling this function
//   - Ensuring apply succeeded before calling prune
func Prune(
	ctx context.Context,
	c client.Client,
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

		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   entry.Group,
			Version: entry.Version,
			Kind:    entry.Kind,
		})
		obj.SetNamespace(entry.Namespace)
		obj.SetName(entry.Name)

		if err := c.Delete(ctx, obj); err != nil {
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
