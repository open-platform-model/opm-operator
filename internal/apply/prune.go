package apply

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/open-platform-model/library/opm/k8s/lifecycle"
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

// Prune deletes stale resources through the library's deletion plan
// (opm/k8s/lifecycle), which judges every object with the library's delete
// verdict (0012:D4:R1, 0012:D8:R8). It decides ownership with no comparison
// of its own, and it deletes nothing itself: the plan runner sends each
// delete the plan names, in the plan's order (descending kind weight), with
// the plan's Foreground propagation and UID precondition.
//
// identities are the instance identities to judge with, most recent first. An
// object counts as the instance's own when the verdict says proceed for one
// of them: there is one plan per identity, and the entries a plan skips as
// another instance's, or as adopted by another instance, are asked again with
// the next identity (RunDeletion). An empty list asks once with no identity,
// which compares no UUID label and leaves every object that carries an adopt
// annotation.
//
// For each entry:
//
//   - A kind OPM never deletes (a core Namespace, a CustomResourceDefinition
//     of apiextensions.k8s.io, matched on group and kind) is left without a
//     read.
//   - The live object is read with c, the client that would delete it. An
//     object that is already gone is done.
//   - An object the verdict skips for every identity is left in the cluster
//     and named in PruneResult.Left with the reason and message of the first
//     verdict. That is not an error.
//   - A PersistentVolumeClaim of the core API group is deleted only when
//     opts.DeleteData is true, because deleting a claim deletes the data on
//     its volume. Otherwise it never enters the plan: a claim the verdict
//     would let the instance delete is left in place and listed in
//     PruneResult.Kept, and a claim that cannot be read is kept without an
//     error (ClassifyKeptClaims).
//   - The DELETE carries a precondition on the UID of the object that was
//     judged. A DELETE the API server refuses on it returns ErrReplaced for
//     the entry: the object that now holds the name is not deleted.
//
// A failed read or DELETE fails its entry, not the run: the remaining entries
// are still attempted and the failures are returned as one joined error.
//
// A delete the API server accepted counts as deleted. With Foreground
// propagation the object can stay, terminating, until its dependents are
// gone; Prune does not wait for that.
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
	// The caller checked spec.prune, so the plan always prunes.
	cleanup, err := RunCleanup(ctx, c, identities, stale, lifecycle.Policy{Prune: true}, opts)
	if err != nil {
		return cleanup.Report, err
	}
	return cleanup.Report, cleanup.Failed
}

// Cleanup is one deletion of a set of inventory entries: the plans that ran
// and what they did, worded for a report.
type Cleanup struct {
	// Deletion holds the plans, for the hold verdict of a deletion cleanup.
	Deletion Deletion

	// Report counts and names what was deleted, left behind and kept. Left is
	// in plan order (descending kind weight), with left-behind claims last.
	// The order is deterministic and is not a contract.
	Report *PruneResult

	// Failed joins the errors of the steps that failed; nil when none did.
	Failed error
}

// RunCleanup deletes entries through the library's deletion plan as Prune
// documents, with the given policy, and returns the plans next to the report.
// The PersistentVolumeClaims that opts keeps never enter a plan: they are
// read and classified for the report only. With a policy that does not prune
// nothing is read, also no claim.
//
// The returned error is the library's refusal of a plan state, which a run
// from the zero State never meets; a failed read or delete is in Failed.
func RunCleanup(
	ctx context.Context,
	c client.Client,
	identities []string,
	entries []releasesv1alpha1.InventoryEntry,
	policy lifecycle.Policy,
	opts PruneOptions,
) (Cleanup, error) {
	log := logf.FromContext(ctx)
	result := &PruneResult{}
	cleanup := Cleanup{Report: result}

	planned, claims := SplitKeptClaims(entries, opts.DeleteData)
	deletion, err := RunDeletion(ctx, c, planned, identities, policy)
	cleanup.Deletion = deletion
	if err != nil || !policy.Prune {
		return cleanup, err
	}

	var errs []error
	for _, step := range deletion.Results() {
		entry := step.Entry
		switch step.Outcome.Result {
		case lifecycle.ResultDeleted:
			log.Info("Pruned stale resource",
				"kind", entry.Kind, "namespace", entry.Namespace, "name", entry.Name)
			result.Deleted++
		case lifecycle.ResultSkipped:
			if step.Outcome.Skip == ownership.SkipAlreadyAbsent {
				log.V(1).Info("Stale resource already deleted",
					"kind", entry.Kind, "namespace", entry.Namespace, "name", entry.Name)
				continue
			}
			result.leave(ctx, LeftBehind{Entry: entry, Reason: step.Outcome.Skip, Message: step.Outcome.Message})
		case lifecycle.ResultFailed:
			errs = append(errs, stepError(step))
		}
	}

	kept, left := ClassifyKeptClaims(ctx, c, identities, claims)
	result.Kept = kept
	for _, l := range left {
		result.leave(ctx, l)
	}

	cleanup.Failed = errors.Join(errs...)
	return cleanup, nil
}

// stepError words a failed step as the prune's error for its entry, keeping
// the API status of the failed read or delete.
func stepError(step StepResult) error {
	entry := step.Entry
	verb := "get"
	if step.Failed == lifecycle.ActionDelete {
		verb = "delete"
	}
	cause := step.Err
	if cause == nil {
		// A read that returned another object than the entry's has no API
		// error: the library's message says what was read.
		cause = errors.New(step.Outcome.Message)
	}
	return fmt.Errorf("failed to %s %s/%s %s: %w", verb, entry.Namespace, entry.Name, entry.Kind, cause)
}

// leave records an entry the delete verdict skipped.
func (r *PruneResult) leave(ctx context.Context, left LeftBehind) {
	entry := left.Entry
	logf.FromContext(ctx).Info("Leaving resource in place on prune",
		"kind", entry.Kind, "namespace", entry.Namespace, "name", entry.Name,
		"reason", string(left.Reason), "message", left.Message)
	r.Left = append(r.Left, left)
	r.Skipped = len(r.Left)
}

// judgeDelete asks the library's delete verdict for one live object with each
// identity in turn, and returns the first verdict that says proceed. It is
// for the report on a kept claim only: no delete follows its answer, since
// every delete is judged inside the deletion plan. It asks
// with the next identity only while the answer is that the object is another
// instance's or adopted by another instance, the two answers that depend on
// the identity. When no identity lets the delete proceed it returns the first
// verdict. An empty list asks once with no identity.
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
