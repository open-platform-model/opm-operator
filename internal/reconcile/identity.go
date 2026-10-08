package reconcile

import "fmt"

// recordedIdentities returns the non-empty identities among ids, in order: the
// list a prune or a deletion cleanup judges with. It is empty when nothing is
// recorded, and the prune then judges with no identity.
func recordedIdentities(ids ...string) []string {
	var out []string
	for _, id := range ids {
		if id != "" {
			out = append(out, id)
		}
	}
	return out
}

// identityPlan is what one render means for the two identity fields of a
// ModuleInstance or a ModulePackage.
type identityPlan struct {
	// InstanceUUID and PreviousInstanceUUID are the state to store before the
	// first write of an apply.
	InstanceUUID         string
	PreviousInstanceUUID string

	// Changed says that the state to store differs from the recorded one.
	Changed bool

	// Refused says that the render carries a third identity while an earlier
	// change is not settled. Nothing is applied or pruned, and the state to
	// store is the recorded one.
	Refused bool

	// Prune holds the identities the stale prune of this reconcile judges
	// with, most recent first. An empty element judges with no identity.
	Prune []string
}

// planIdentities decides what a render of identity rendered means for a
// status that records instanceUUID and previousInstanceUUID.
//
//   - Nothing recorded: the render's identity is recorded. The prune asks
//     with it first and then with no identity, so it deletes what the
//     managed-by label alone let it delete before the field existed.
//   - The render carries the recorded identity, or none: nothing changes.
//   - The render carries another identity and no change is pending: the
//     recorded identity becomes the previous one.
//   - A change is pending and the render carries the previous identity: the
//     two swap.
//   - A change is pending and the render carries a third identity: refused.
//
// previousInstanceUUID is cleared by the caller, in the commit of a
// reconcile whose apply and prune succeeded, and nowhere else.
func planIdentities(recorded, previous, rendered string) identityPlan {
	keep := identityPlan{
		InstanceUUID:         recorded,
		PreviousInstanceUUID: previous,
		Prune:                recordedIdentities(recorded, previous),
	}
	switch {
	case rendered == "" || rendered == recorded:
		return keep
	case recorded == "":
		return identityPlan{InstanceUUID: rendered, Changed: true, Prune: []string{rendered, ""}}
	case previous == "":
		return identityPlan{
			InstanceUUID: rendered, PreviousInstanceUUID: recorded,
			Changed: true, Prune: []string{rendered, recorded},
		}
	case rendered == previous:
		return identityPlan{
			InstanceUUID: previous, PreviousInstanceUUID: recorded,
			Changed: true, Prune: []string{previous, recorded},
		}
	default:
		keep.Refused = true
		return keep
	}
}

// keepsNoOp reports whether a reconcile whose render equals what was last
// applied is a NoOp. It is not while an identity change is unsettled: going
// back to the earlier identity renders what was last applied, while the
// cluster holds objects the unsettled apply relabelled, and only an apply and
// a prune that succeed relabel them back and clear the earlier identity.
func (p identityPlan) keepsNoOp(isNoOp bool) bool {
	return isNoOp && p.PreviousInstanceUUID == ""
}

// fillEmpty records the render's identity in a reconcile that applies
// nothing, when none is recorded: an object recorded before the field existed
// gains it. A recorded identity is never moved by such a reconcile.
func (p identityPlan) fillEmpty(recorded *string) {
	if *recorded == "" {
		*recorded = p.InstanceUUID
	}
}

// store writes a changed identity state to the two status fields and commits
// it with patch, before the first write of an apply, which relabels the
// objects: from then on the cluster may hold objects of both identities, and
// a prune or a deletion cleanup must accept either, also after a restart. An
// error means that nothing may be applied.
func (p identityPlan) store(current, previous *string, patch func() error) error {
	if !p.Changed {
		return nil
	}
	*current, *previous = p.InstanceUUID, p.PreviousInstanceUUID
	if err := patch(); err != nil {
		return fmt.Errorf("recording the instance identity before the apply: %w", err)
	}
	return nil
}
