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

	"github.com/open-platform-model/library/opm/k8s/labels"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
)

// PruneResult carries counts of prune outcomes.
type PruneResult struct {
	// Deleted is the number of stale resources successfully deleted.
	Deleted int

	// Skipped is the number of stale resources skipped due to safety exclusions.
	Skipped int

	// Kept lists the PersistentVolumeClaims the run left in the cluster
	// because PruneOptions.DeleteData is false. A kept claim is not an error
	// and is not counted in Skipped.
	Kept []releasesv1alpha1.InventoryEntry
}

// PruneOptions tunes one prune run. The zero value protects data.
type PruneOptions struct {
	// DeleteData allows the deletion of PersistentVolumeClaims. The
	// reconcilers set it from spec.dataPolicy; when false, every claim that
	// would be deleted is kept and listed in PruneResult.Kept.
	DeleteData bool
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
// carries a module-instance.opmodel.dev/uuid label that disagrees with
// ownerUUID. An empty live UUID label is tolerated (legacy resources predate
// UUID stamping). An empty ownerUUID disables the UUID comparison — callers
// that cannot supply a UUID (e.g. the ModulePackage reconciler, or a freshly-created
// ModuleInstance whose Status.InstanceUUID is not yet persisted) fall back to
// the managed-by check alone.
//
// Skipped resources are logged as warnings and counted in PruneResult.Skipped.
//
// Data protection: a PersistentVolumeClaim of the core API group is deleted
// only when opts.DeleteData is true, because deleting a claim deletes the
// data on its volume. Otherwise a claim that exists and passes the ownership
// guard is left in place and listed in PruneResult.Kept. A claim that is
// already gone is not listed, a claim another owner holds is skipped as any
// other resource, and a claim that cannot be read is kept without an error:
// nothing is going to be deleted, so the failed read must not fail the prune
// or hold a finalizer.
//
// If a stale resource is already gone (NotFound), it is treated as success.
// Individual failures (Get or Delete) are collected and returned as a joined
// error; remaining entries continue (design decision 2: continue-on-error /
// fail-slow).
//
// The caller is responsible for:
//   - Computing the stale set with the library's opm/k8s/inventory.StaleSet
//   - Checking spec.prune before calling this function
//   - Ensuring apply succeeded before calling prune
//   - Supplying ownerUUID from the freshly-rendered resources or
//     ModuleInstanceStatus.InstanceUUID
func Prune(
	ctx context.Context,
	c client.Client,
	ownerUUID string,
	stale []releasesv1alpha1.InventoryEntry,
	opts PruneOptions,
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
			if isDataClaim(entry) && !opts.DeleteData {
				log.Info("Keeping PersistentVolumeClaim that could not be read",
					"namespace", entry.Namespace, "name", entry.Name, "error", getErr.Error())
				result.Kept = append(result.Kept, entry)
				continue
			}
			errs = append(errs, fmt.Errorf("failed to get %s/%s %s: %w",
				entry.Namespace, entry.Name, entry.Kind, getErr))
			continue
		}

		liveLabels := live.GetLabels()
		if !labels.IsOPMManagedBy(liveLabels[labels.ManagedBy]) {
			log.Info("Skipping prune: live resource is not OPM-managed",
				"kind", entry.Kind, "namespace", entry.Namespace, "name", entry.Name,
				"managedBy", liveLabels[labels.ManagedBy])
			result.Skipped++
			continue
		}

		liveUUID := liveLabels[labels.ModuleInstanceUUID]
		if ownerUUID != "" && liveUUID != "" && liveUUID != ownerUUID {
			log.Info("Skipping prune: live resource instance UUID does not match owner",
				"kind", entry.Kind, "namespace", entry.Namespace, "name", entry.Name,
				"ownerUUID", ownerUUID, "liveUUID", liveUUID)
			result.Skipped++
			continue
		}

		if isDataClaim(entry) && !opts.DeleteData {
			log.Info("Keeping PersistentVolumeClaim and the data on it",
				"namespace", entry.Namespace, "name", entry.Name)
			result.Kept = append(result.Kept, entry)
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

// isDataClaim reports whether entry is a PersistentVolumeClaim of the core
// API group, the one kind whose deletion also deletes user data.
func isDataClaim(entry releasesv1alpha1.InventoryEntry) bool {
	return entry.Group == "" && entry.Kind == "PersistentVolumeClaim"
}
