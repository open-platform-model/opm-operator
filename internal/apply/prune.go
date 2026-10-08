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

	"github.com/open-platform-model/library/opm/k8s/ownership"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
)

// LeftBehind is one object a prune did not delete because the delete verdict
// skipped it, with the library's reason and message.
type LeftBehind struct {
	Entry   releasesv1alpha1.InventoryEntry
	Reason  ownership.SkipReason
	Message string
}

// PruneResult carries the outcomes of one prune run.
type PruneResult struct {
	// Deleted is the number of stale resources successfully deleted.
	Deleted int

	// Skipped is the number of resources the delete verdict left in the
	// cluster: len(Left).
	Skipped int

	// Left names every resource the delete verdict skipped, a kind OPM never
	// deletes included. A resource that was already gone and a kept
	// PersistentVolumeClaim are not listed.
	Left []LeftBehind

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

// ErrReplaced reports a DELETE the API server refused on the UID
// precondition: the object was replaced since it was read. It is never
// counted as deleted. Recognised by the API status, never by message text.
var ErrReplaced = errors.New("object was replaced since it was read")

// Prune deletes the stale resources the library's delete verdict
// (opm/k8s/ownership) lets this instance delete (0012:D4:R1, 0012:D8:R8). It
// decides ownership with no comparison of its own.
//
// identities are the instance identities to judge with, most recent first. An
// object counts as the instance's own when the verdict says proceed for one
// of them: the verdict is asked with the first, and with the next one while
// the answer is that the object belongs to, or is being adopted by, another
// instance. An empty list asks once with no identity, which compares no UUID
// label and leaves every object that carries an adopt annotation.
//
// For each entry, in this order:
//
//   - A kind OPM never deletes (a core Namespace, a CustomResourceDefinition
//     of apiextensions.k8s.io, matched on group and kind) is left without a
//     read.
//   - The live object is read with c, the client that would delete it. An
//     object that is already gone is done.
//   - The verdict is asked. An object it skips for every identity is left in
//     the cluster and named in PruneResult.Left with the reason and message
//     of the first verdict. That is not an error.
//   - A PersistentVolumeClaim of the core API group is deleted only when
//     opts.DeleteData is true, because deleting a claim deletes the data on
//     its volume. Otherwise a claim the verdict lets the instance delete is
//     left in place and listed in PruneResult.Kept. A claim that cannot be
//     read is kept without an error: nothing is going to be deleted, so the
//     failed read must not fail the prune or hold a finalizer.
//   - The DELETE carries a precondition on the UID of the object that was
//     judged. A DELETE the API server refuses on it returns ErrReplaced for
//     the entry: the object that now holds the name is not deleted.
//
// A failed read or DELETE fails its entry, not the run: the remaining entries
// are still attempted and the failures are returned as one joined error.
//
// The caller computes the stale set, checks spec.prune, and calls Prune only
// after the apply succeeded. Apply protects claims in the same way on a
// forced recreate (ApplyOptions).
func Prune(
	ctx context.Context,
	c client.Client,
	identities []string,
	stale []releasesv1alpha1.InventoryEntry,
	opts PruneOptions,
) (*PruneResult, error) {
	log := logf.FromContext(ctx)
	result := &PruneResult{}

	var errs []error
	for _, entry := range stale {
		obj := ownership.Object{Group: entry.Group, Kind: entry.Kind, Namespace: entry.Namespace, Name: entry.Name}
		if ownership.SafetyExcluded(entry.Group, entry.Kind) {
			result.leave(ctx, entry, ownership.CanDelete(ownership.DeleteInput{Object: obj}))
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

		verdict := judgeDelete(obj, live, identities)
		if !verdict.Proceed() {
			result.leave(ctx, entry, verdict)
			continue
		}

		if isDataClaim(entry) && !opts.DeleteData {
			log.Info("Keeping PersistentVolumeClaim and the data on it",
				"namespace", entry.Namespace, "name", entry.Name)
			result.Kept = append(result.Kept, entry)
			continue
		}

		var deleteOpts []client.DeleteOption
		pre := verdict.Preconditions()
		if pre != nil {
			deleteOpts = append(deleteOpts, client.Preconditions(*pre))
		}
		if err := c.Delete(ctx, live, deleteOpts...); err != nil {
			if apierrors.IsNotFound(err) {
				log.V(1).Info("Stale resource already deleted",
					"kind", entry.Kind, "namespace", entry.Namespace, "name", entry.Name)
				continue
			}
			errs = append(errs, fmt.Errorf("failed to delete %s/%s %s: %w",
				entry.Namespace, entry.Name, entry.Kind, replacedError(err, pre != nil)))
			continue
		}

		log.Info("Pruned stale resource",
			"kind", entry.Kind, "namespace", entry.Namespace, "name", entry.Name)
		result.Deleted++
	}

	return result, errors.Join(errs...)
}

// leave records an entry the delete verdict skipped.
func (r *PruneResult) leave(ctx context.Context, entry releasesv1alpha1.InventoryEntry, verdict ownership.DeleteVerdict) {
	logf.FromContext(ctx).Info("Leaving resource in place on prune",
		"kind", entry.Kind, "namespace", entry.Namespace, "name", entry.Name,
		"reason", string(verdict.Skip), "message", verdict.Message)
	r.Left = append(r.Left, LeftBehind{Entry: entry, Reason: verdict.Skip, Message: verdict.Message})
	r.Skipped = len(r.Left)
}

// judgeDelete asks the library's delete verdict for one live object with each
// identity in turn, and returns the first verdict that says proceed. It asks
// with the next identity only while the answer is that the object is another
// instance's or adopted by another instance, the two answers that depend on
// the identity. When no identity lets the delete proceed it returns the first
// verdict. An empty list asks once with no identity. Admit is never set: it
// is for the operator install only.
func judgeDelete(obj ownership.Object, live *unstructured.Unstructured, identities []string) ownership.DeleteVerdict {
	if len(identities) == 0 {
		identities = []string{""}
	}
	first := ownership.CanDelete(ownership.DeleteInput{Object: obj, Live: live, InstanceUUID: identities[0]})
	verdict := first
	for _, id := range identities[1:] {
		if verdict.Skip != ownership.SkipOwnerMismatch && verdict.Skip != ownership.SkipAdoptedElsewhere {
			break
		}
		verdict = ownership.CanDelete(ownership.DeleteInput{Object: obj, Live: live, InstanceUUID: id})
	}
	if verdict.Proceed() {
		return verdict
	}
	return first
}

// replacedError marks err with ErrReplaced when it is the API server's
// refusal of a DELETE on its UID precondition. The API server answers a
// failed precondition with a Conflict status, and a DELETE that carries only
// a UID precondition has no other cause of one.
func replacedError(err error, uidPrecondition bool) error {
	if uidPrecondition && apierrors.IsConflict(err) {
		return fmt.Errorf("%w: %w", ErrReplaced, err)
	}
	return err
}

// isDataClaim reports whether entry is a PersistentVolumeClaim of the core
// API group, the one kind whose deletion also deletes user data.
func isDataClaim(entry releasesv1alpha1.InventoryEntry) bool {
	return isCoreClaim(entry.Group, entry.Kind)
}
