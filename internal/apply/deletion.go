package apply

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	k8sinventory "github.com/open-platform-model/library/opm/k8s/inventory"
	"github.com/open-platform-model/library/opm/k8s/lifecycle"
	"github.com/open-platform-model/library/opm/k8s/ownership"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
)

// StepResult is what happened to one inventory entry in one deletion plan.
type StepResult struct {
	// Entry is the inventory entry of the step.
	Entry releasesv1alpha1.InventoryEntry

	// Outcome is the library's record of the step. It is the zero Outcome
	// when the plan named no action for the step: a plan whose policy does
	// not prune names none at all.
	Outcome lifecycle.Outcome

	// Failed is the action that failed, a read or a delete; empty otherwise.
	Failed lifecycle.ActionKind

	// Live is the object the plan's read returned; nil when the read found
	// nothing, failed, or was never sent. It carries the deletionTimestamp
	// and the finalizers of the object as it was before the delete.
	Live *unstructured.Unstructured

	// Err is the raw error of the failed read or delete. A Conflict on a
	// delete that carried a UID precondition is wrapped in ErrReplaced. It is
	// nil for a read that returned another object than the step's: the
	// outcome's message says what was read.
	Err error
}

// DeletionRun is one deletion plan and how far it was driven.
type DeletionRun struct {
	// Plan is the plan as built: its steps are in the library's order.
	Plan lifecycle.DeletionPlan

	// State is the plan's state after the last action. A hold verdict is
	// asked with Plan and State.
	State lifecycle.State

	// Steps holds one result per step of Plan, in the plan's order.
	Steps []StepResult
}

// runDeletion drives plan from the zero State to done with c (0012:D4:R1). It
// is the only function of the operator that deletes an inventory object, and
// it deletes only what lifecycle.Advance names: every delete was judged by
// the library's delete verdict inside Advance. It sets no propagation policy
// and no precondition of its own; it sends the action's.
//
// A failed read or delete fails its step and not the run. The returned error
// is the library's refusal of a state that cannot belong to the plan, which a
// run from the zero State never meets.
func runDeletion(ctx context.Context, c client.Client, plan lifecycle.DeletionPlan) (DeletionRun, error) {
	steps := plan.Steps()
	run := DeletionRun{Plan: plan, Steps: make([]StepResult, len(steps))}
	for i, step := range steps {
		run.Steps[i].Entry = releasesv1alpha1.InventoryEntry(step.Entry)
	}

	var (
		state lifecycle.State
		ev    lifecycle.Event
		// preconditioned says whether the delete that ev answers carried a
		// UID precondition.
		preconditioned bool
	)
	for {
		next, act, err := lifecycle.Advance(plan, state, ev)
		if err != nil {
			return run, fmt.Errorf("advancing the deletion plan: %w", err)
		}
		run.record(state, next, ev, preconditioned)
		run.State = next
		state, ev, preconditioned = next, lifecycle.Event{}, false

		switch act.Kind {
		case lifecycle.ActionDone:
			return run, nil
		case lifecycle.ActionRead:
			ev.Live, ev.Err = readEntry(ctx, c, act.Entry)
			run.Steps[act.Step].Live = ev.Live
		case lifecycle.ActionDelete:
			var opts []client.DeleteOption
			if act.Propagation != "" {
				opts = append(opts, client.PropagationPolicy(act.Propagation))
			}
			if act.Preconditions != nil {
				opts = append(opts, client.Preconditions(*act.Preconditions))
				preconditioned = true
			}
			ev.Err = c.Delete(ctx, objectOfEntry(act.Entry), opts...)
		case lifecycle.ActionSkip:
			// The state has already moved past the step; nothing to perform.
		}
	}
}

// record copies the outcomes one call of Advance appended, from state before
// to state after, into the run. An appended outcome that failed belongs to
// the step the call answered: its action is what before awaited, and its raw
// error is the one the call was handed.
func (run *DeletionRun) record(before, after lifecycle.State, ev lifecycle.Event, preconditioned bool) {
	for _, outcome := range after.Outcomes[len(before.Outcomes):] {
		step := &run.Steps[outcome.Step]
		step.Outcome = outcome
		if outcome.Result != lifecycle.ResultFailed || outcome.Step != before.Next {
			continue
		}
		switch before.Awaiting {
		case lifecycle.AwaitRead:
			step.Failed = lifecycle.ActionRead
			step.Err = ev.Err
		case lifecycle.AwaitDelete:
			step.Failed = lifecycle.ActionDelete
			step.Err = replacedError(ev.Err, preconditioned)
		case lifecycle.AwaitNothing:
		}
	}
}

// readEntry reads the live object of one entry. It returns nil and the raw
// error when the read fails, NotFound included: the plan classifies it.
func readEntry(ctx context.Context, c client.Reader, entry k8sinventory.Entry) (*unstructured.Unstructured, error) {
	live := objectOfEntry(entry)
	if err := c.Get(ctx, types.NamespacedName{Namespace: entry.Namespace, Name: entry.Name}, live); err != nil {
		return nil, err
	}
	return live, nil
}

// objectOfEntry names an entry's object for a read or a delete.
func objectOfEntry(entry k8sinventory.Entry) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: entry.Group, Version: entry.Version, Kind: entry.Kind})
	obj.SetNamespace(entry.Namespace)
	obj.SetName(entry.Name)
	return obj
}

// Deletion is the plans one deletion of a set of entries ran: one per
// identity that was asked.
type Deletion struct {
	// Runs holds the plans in the order they ran. The first holds every
	// entry; each later one holds the entries the one before it skipped as
	// another instance's or as adopted by another instance. A hold verdict
	// is asked for each.
	Runs []DeletionRun
}

// RunDeletion deletes entries through the library's deletion plan
// (opm/k8s/lifecycle), with the client that deletes. A plan judges with one
// instance identity, so there is one plan per identity, in the order of
// identities (most recent first):
//
//   - The first plan holds every entry and the first identity.
//   - The entries a plan skips as another instance's (owner-mismatch) or as
//     adopted by another instance (adopted-elsewhere), the two answers that
//     depend on the identity, form the plan of the next identity.
//   - An empty list is one plan with no identity, which compares no UUID
//     label and leaves every object that carries an adopt annotation.
//
// No entry is deleted by two plans: an entry reaches a later plan only when
// every earlier one skipped it. Each plan deletes in the library's order,
// descending kind weight, so an object only a later identity owns is deleted
// after all of the plans before it.
//
// With a policy that does not prune the one plan names no action and no
// object is read.
func RunDeletion(
	ctx context.Context,
	c client.Client,
	entries []releasesv1alpha1.InventoryEntry,
	identities []string,
	policy lifecycle.Policy,
) (Deletion, error) {
	if len(identities) == 0 {
		identities = []string{""}
	}
	var deletion Deletion
	for i, identity := range identities {
		if i > 0 && len(entries) == 0 {
			break
		}
		planned := make([]k8sinventory.Entry, len(entries))
		for j, entry := range entries {
			planned[j] = k8sinventory.Entry(entry)
		}
		run, err := runDeletion(ctx, c, lifecycle.NewDeletionPlan(planned, policy, identity))
		deletion.Runs = append(deletion.Runs, run)
		if err != nil {
			return deletion, err
		}
		entries = entries[:0:0]
		for _, step := range run.Steps {
			if dependsOnIdentity(step.Outcome) {
				entries = append(entries, step.Entry)
			}
		}
	}
	return deletion, nil
}

// dependsOnIdentity reports whether a step was skipped for one of the two
// reasons another identity can answer differently.
func dependsOnIdentity(outcome lifecycle.Outcome) bool {
	return outcome.Result == lifecycle.ResultSkipped &&
		(outcome.Skip == ownership.SkipOwnerMismatch || outcome.Skip == ownership.SkipAdoptedElsewhere)
}

// Results returns one result per entry of the deletion, in the order of the
// first plan. An entry that a later plan deleted, found absent or failed on
// has that plan's result. An entry no plan deletes because every identity was
// refused keeps the reason and message of the first plan, as the first
// verdict words whose object it is.
func (d Deletion) Results() []StepResult {
	if len(d.Runs) == 0 {
		return nil
	}
	results := append([]StepResult(nil), d.Runs[0].Steps...)
	index := make(map[releasesv1alpha1.InventoryEntry]int, len(results))
	for i, result := range results {
		if dependsOnIdentity(result.Outcome) {
			index[result.Entry] = i
		}
	}
	for _, run := range d.Runs[1:] {
		for _, step := range run.Steps {
			i, ok := index[step.Entry]
			if !ok || dependsOnIdentity(step.Outcome) {
				continue
			}
			results[i] = step
		}
	}
	return results
}

// SplitKeptClaims takes the PersistentVolumeClaims of the core API group out
// of entries when the data policy keeps them: deleting a claim deletes the
// data on its volume, and a deletion plan has no data policy. planned keeps
// the order of entries. With deleteData every entry is planned: a claim is
// then deleted as any object.
func SplitKeptClaims(
	entries []releasesv1alpha1.InventoryEntry,
	deleteData bool,
) (planned, claims []releasesv1alpha1.InventoryEntry) {
	if deleteData {
		return entries, nil
	}
	for _, entry := range entries {
		if isDataClaim(entry) {
			claims = append(claims, entry)
			continue
		}
		planned = append(planned, entry)
	}
	return planned, claims
}

// ClassifyKeptClaims reads the claims a deletion keeps and says how to report
// each. It takes a reader, so it cannot delete, and decides whose a claim is
// with the library's delete verdict and no comparison of its own:
//
//   - A claim the verdict would let the instance delete is kept.
//   - A claim the verdict skips for every identity is left behind, with the
//     reason and message of the first verdict.
//   - A claim that is gone is not reported.
//   - A claim that cannot be read is kept without an error: nothing is going
//     to be deleted, so the failed read must not fail a prune or hold a
//     finalizer.
func ClassifyKeptClaims(
	ctx context.Context,
	c client.Reader,
	identities []string,
	claims []releasesv1alpha1.InventoryEntry,
) (kept []releasesv1alpha1.InventoryEntry, left []LeftBehind) {
	log := logf.FromContext(ctx)
	for _, entry := range claims {
		live, err := readEntry(ctx, c, k8sinventory.Entry(entry))
		switch {
		case apierrors.IsNotFound(err):
			continue
		case err != nil:
			log.Info("Keeping PersistentVolumeClaim that could not be read",
				"namespace", entry.Namespace, "name", entry.Name, "error", err.Error())
			kept = append(kept, entry)
			continue
		}
		obj := ownership.Object{Group: entry.Group, Kind: entry.Kind, Namespace: entry.Namespace, Name: entry.Name}
		verdict := judgeDelete(obj, live, identities)
		if !verdict.Proceed() {
			left = append(left, LeftBehind{Entry: entry, Reason: verdict.Skip, Message: verdict.Message})
			continue
		}
		log.Info("Keeping PersistentVolumeClaim and the data on it",
			"namespace", entry.Namespace, "name", entry.Name)
		kept = append(kept, entry)
	}
	return kept, left
}

// Deleted returns the steps whose DELETE the API server accepted, over every
// plan of the deletion.
func (d Deletion) Deleted() []StepResult {
	var out []StepResult
	for _, run := range d.Runs {
		for _, step := range run.Steps {
			if step.Outcome.Result == lifecycle.ResultDeleted {
				out = append(out, step)
			}
		}
	}
	return out
}

// StillThere reads the object of each step again and returns the ones that
// still exist, as read now: with their deletionTimestamp and finalizers. An
// object is gone when the read returns NotFound, or when another object (a
// different UID than the one the plan read) holds its name. It takes a
// reader, so it cannot delete. A read that fails otherwise ends the check
// with that error: whether the object is gone is then not known.
func StillThere(ctx context.Context, c client.Reader, steps []StepResult) ([]*unstructured.Unstructured, error) {
	var left []*unstructured.Unstructured
	for _, step := range steps {
		entry := k8sinventory.Entry(step.Entry)
		live, err := readEntry(ctx, c, entry)
		switch {
		case apierrors.IsNotFound(err):
			continue
		case err != nil:
			return nil, fmt.Errorf("checking that %s %s/%s is gone: %w", entry.Kind, entry.Namespace, entry.Name, err)
		case step.Live != nil && live.GetUID() != step.Live.GetUID():
			continue
		}
		left = append(left, live)
	}
	return left, nil
}
