## Context

See proposal.md for the motivation and the "today / after" tables. Facts about the library package that shape this design, read from the source of the pinned version (`library v1.0.0-beta.7`, `opm/k8s/lifecycle`):

1. **A plan has one owner UUID** (`plan.go:51`). The operator judges with up to two identities, and a ModulePackage with no recorded identity also asks with none (`internal/reconcile/identity.go:34-47`, `internal/apply/prune.go:196-212`).
2. **A plan has no data policy.** `Policy` holds `Prune` and `ForceOrphan` only (`plan.go:12-21`). Keeping PersistentVolumeClaims is the operator's rule.
3. **The package never waits.** A DELETE the API server accepted is `ResultDeleted` at once (`advance.go:234-239`), and `MayReleaseHold` releases when every step is deleted or skipped (`hold.go:83-123`). An object that stays Terminating is, to the plan, deleted.
4. **An object already being deleted is judged "proceed"** (`ownership/delete.go:93-94`), so a later plan over the same entry names the delete again. A second DELETE of a terminating object changes nothing.
5. **A failure does not stop the plan** (`advance.go:148-149`), as the operator's loop does not today.
6. **The hold is the instance's hold**: for the operator, the finalizer `opmodel.dev/cleanup` on the ModuleInstance or ModulePackage.

Reversibility: two-way door. No CRD field, no stored state, no flag. A revert of the PR restores today's behaviour; objects deleted in the meantime are deleted either way.

## Goals / Non-Goals

**Goals:**

- Every delete of an inventory object is sent because `lifecycle.Advance` named it, with the action's propagation and precondition (0012:D4:R1).
- The cleanup finalizer is removed only after a release verdict of `lifecycle.MayReleaseHold`.
- One function in the operator performs the plan's actions.
- Every rule of #260, #262, #263, #267, #268 and #271 holds unchanged (proposal.md, item 3).

**Non-Goals:**

- The apply-time ownership check (`guard-every-apply-by-ownership`).
- The forced recreate: it stays in `deleteGuard`.
- Waiting for a stale object after a prune.
- Any ordering beyond the library's.
- A CRD field, a flag, a library change.

## Research & Decisions

### One runner performs every plan

**Context**: `Advance` names actions; the operator must perform them. One loop exists today, `apply.Prune`, called from four places.

**Explored**: `lifecycle/advance.go`, `plan.go`, `hold.go`; the cli's runner (cli#350, `openspec/changes/archive/2026-10-08-adopt-kubernetes-lifecycle-package/design.md`).

**Options considered**:

1. Status quo. No work; 0012:D4:R1 is not met and the operator keeps a second implementation of the order and the propagation.
2. Each reconciler calls `Advance`. Four copies of the loop.
3. One runner in `internal/apply`; `Prune` and the two deletion handlers build plans and report.

**Decision**: option 3.

**Rationale**: the runner is the single delete site, which `TestDeleteCallSites` pins by file. The callers keep what differs: which entries, which policy, what is reported.

```go
// internal/apply/deletion.go

// StepResult is what happened to one inventory entry.
type StepResult struct {
	Entry   releasesv1alpha1.InventoryEntry
	Outcome lifecycle.Outcome
	// UID is the UID the accepted delete was sent with; empty otherwise.
	UID types.UID
	// Err is the raw error of a failed read or delete.
	Err error
}

// DeletionRun is one finished plan.
type DeletionRun struct {
	Plan  lifecycle.DeletionPlan
	State lifecycle.State
	Steps []StepResult
}

// runDeletion drives plan to done with c. It is the only function of the
// operator that deletes an inventory object. It sets no propagation and no
// precondition of its own.
func runDeletion(ctx context.Context, c client.Client, plan lifecycle.DeletionPlan) (DeletionRun, error) {
	state, ev := lifecycle.State{}, lifecycle.Event{}
	for {
		next, act, err := lifecycle.Advance(plan, state, ev)
		if err != nil {
			return run, err // the state cannot belong to the plan; never from the zero State
		}
		record(&run, state, next)
		state, ev = next, lifecycle.Event{}
		switch act.Kind {
		case lifecycle.ActionDone:
			return run, nil
		case lifecycle.ActionRead:
			ev.Live, ev.Err = get(ctx, c, act.Entry)
		case lifecycle.ActionDelete:
			ev.Err = c.Delete(ctx, objectOf(act.Entry),
				client.PropagationPolicy(act.Propagation), preconditions(act.Preconditions))
		}
	}
}
```

A Conflict on a delete that carried a precondition is still wrapped in `ErrReplaced`, recognised by `apierrors.IsConflict`, never by text.

### Two identities: one plan per identity

**Context**: fact 1. Today `judgeDelete` asks the verdict with the first identity and, only while the answer is `owner-mismatch` or `adopted-elsewhere`, with the next.

**Options considered**:

1. Judge with the first identity only. Breaks `prune-stale-resources` "The prune judges with the recorded identities" and the owner decision of 2026-10-08.
2. Ask the library for a plan that takes several owner UUIDs. Right in the long run, but it is a library contract change and a release, for a state that exists only until an identity change is settled.
3. One plan per identity. Plan 1 holds all entries and the first identity. The entries plan 1 skips as `owner-mismatch` or `adopted-elsewhere` form plan 2 with the next identity, and so on. An entry no plan deletes is reported with the reason and message of plan 1.

**Decision**: option 3.

**Rationale**: it is `judgeDelete` expressed in plans: same identities, same order, same two reasons, same first-verdict report. Every delete is still named by `Advance`. `judgeDelete` is removed from the delete path. The hold verdict is asked for each plan; the finalizer is released only when every one releases.

**Trade-off**: objects only the second identity owns are deleted after all of plan 1, so the order over the whole inventory is "descending weight, twice". This only happens while `status.previousInstanceUUID` is set or for a ModulePackage without a recorded identity.

### Kept claims never enter the plan

**Context**: fact 2. Today a claim is kept only after the verdict said proceed; a claim of another owner is "left behind", a claim that is gone is not reported, and an unreadable claim is kept without an error (`prune.go:132-153`).

**Options considered**:

1. Leave claims in the plan and, when it names the delete of a claim, send nothing and hand back an empty event. One read per claim, but the plan's state then says "deleted" for an object that was not, and an unreadable claim needs a second invented answer.
2. Take the claims out before the plan when `spec.dataPolicy` keeps them (the cli's choice, cli#350), and classify them in a separate read-only pass: read, ask `ownership.CanDelete` with the identities in turn, report as kept, left behind or gone.

**Decision**: option 2.

**Rationale**: the plan holds exactly the objects that may be deleted, and its state is true. The classification pass sends no delete, so it is outside 0012:D4:R1; it still decides with the library's verdict and no comparison of its own. With `spec.dataPolicy: Delete`, claims are ordinary plan entries.

### The finalizer follows the hold verdict

**Decision**: the deletion handlers build `lifecycle.Policy{Prune: spec.prune, ForceOrphan: annotation == "true"}` and `HoldInput{Identity}` from the impersonation result: available, `IdentityMissing` when the ServiceAccount is not found, `IdentityFailed` on any other impersonation error. They act on `MayReleaseHold` as the table in proposal.md item 2 says. With an unavailable identity no plan is run: the verdict is asked with the zero State, and it answers on the identity before it looks at the state (`hold.go:89-97`).

**Rationale**: every branch of today's handlers has exactly one verdict reason, so no status, event or requeue interval changes. The verdict's message is not shown: the operator's messages name the ServiceAccount and the remedies, which the library's cannot.

### The finalizer waits until the deleted objects are gone (owner decision Q1)

**Context**: fact 3, and Foreground. Today the default propagation removes a Deployment at once and its Pods follow in the background, so when the instance is gone its named objects are gone. With Foreground a Deployment stays, Terminating, until its Pods are gone. If the finalizer still came off at "delete accepted", a user or a GitOps tool that deletes an instance and creates it again would apply onto objects that the garbage collector is about to remove. The cli met the same gap and added `--wait` (cli#353).

**Options considered**:

| | A: release on the verdict alone | B: release on the verdict and "gone" (recommended) |
| --- | --- | --- |
| Finalizer comes off | When every DELETE is accepted, as today | When every object this cleanup deleted returns NotFound or has another UID |
| Delete, then create again | Can apply onto terminating objects; the new instance loses them and heals at a later reconcile | Safe: the old instance exists until its objects are gone |
| Stuck dependent (Pod on a dead node) | Invisible: instance gone, Deployment terminating, nothing tracks it | Visible: the instance stays, `DeletionBlocked` names the object |
| New stuck state for the user | None | Yes: a ModuleInstance can stay Terminating on a foreign finalizer; way out is `spec.prune: false` |
| Code | None beyond the plan | A read per deleted object, two reasons, a requeue |

**Decision**: B, pending the owner.

**Rationale**: Foreground without a wait makes the operator's delete worse than today for the delete-and-recreate case, and hides stuck objects. A controller's finalizer is the standard place to wait for external cleanup. 0012:D4:R1 allows it: the hold is released only on a release verdict, and the frontend may hold longer. The trade-off given up is liveness: a deletion can now wait on an object the operator does not control. The remedy is explicit and already documented for another stall (`spec.prune: false`).

Mechanics (B):

```go
verdict := releaseOf(runs, holdInput)        // every plan's MayReleaseHold
if !verdict.Release { return actOnHold(verdict) }   // statuses as today
left := stillTerminating(ctx, deleteClient, runs)   // GET each ResultDeleted step; gone = NotFound or other UID
if len(left) == 0 { return removeFinalizer() }
if oldest(left) < DeletionBlockedAfter {            // 10 min, from the objects' deletionTimestamp
	markDeletionInProgress(obj, left)               // Ready=False, Reconciling=True
	return ctrl.Result{RequeueAfter: DeletionRecheckInterval}, nil   // 10 s
}
markDeletionBlocked(obj, left)                      // Ready=False, Stalled=True, Warning event once
return ctrl.Result{RequeueAfter: DeletionBlockedRecheckInterval}, nil // 1 min
```

- The reconcile never blocks: it reads once and requeues.
- Each recheck is a fresh plan from `status.inventory` (decision below). Entries that are gone are read once and skipped as already absent; entries still terminating are read and their delete is named again (fact 4), which the API server accepts without effect.
- A kept claim, an object left behind and an object already absent are not waited for.
- `spec.prune` set to false while waiting: the next reconcile gets `prune-disabled` and removes the finalizer. A spec change bumps the generation, so it triggers a reconcile at once.
- The two intervals and the threshold are constants, not flags (Principle VII).

Reconcile phase impact: Source, Render and Apply are not touched. Prune: order and propagation change. Status: two new reasons, on a deleting object only. The deletion branch gains the wait.

### The state is not stored (owner decision Q2)

**Context**: 0012:D4 says the state is a serialisable value "so the operator carries it across reconciles". `02-design.md` of the entry words it as "can".

**Options considered**:

1. Store `lifecycle.State` in `status`. A new status field (CRD change), and a stored state can disagree with an inventory that changed; `Advance` then refuses it and the operator needs a reset rule.
2. Each reconcile starts from the zero State and drives the plan to done.

**Decision**: option 2, pending the owner.

**Rationale**: a plan over a few dozen entries finishes inside one reconcile, as `apply.Prune` does today. A re-run is idempotent: absent entries are skipped, terminating ones are deleted again without effect. Nothing is gained by resuming mid-plan, and the CRD stays as it is. 0012:D4:R5 is a property of the library, already tested there.

### The forced recreate stays outside the plan

**Decision**: `deleteGuard` (`internal/apply/claims.go:141-177`) is unchanged and stays an allowed delete site.

**Rationale**: owner decision "UID precondition only". The delete is sent by Flux's engine in the middle of an apply, with Background propagation (`manager_apply.go:144`), and the object is created again at once; a Foreground delete would leave the name terminating. The object is not leaving the inventory, so it is no step of a deletion plan. 0012:D4:R1 names one exception and this is not it; see Open Questions.

### Stale prune: no hold, no wait

**Decision**: `apply.Prune` keeps its signature and its result type and becomes a caller of the runner, with `Policy{Prune: true}` (the reconciler checks `spec.prune` before, as today). It asks no hold verdict and waits for nothing.

**Rationale**: a prune holds nothing. A stale entry leaves the inventory when its delete is accepted, as today. Keeping `Prune` and `PruneResult` keeps the four `prune-stale-resources` requirements that name them true without a rewrite.

## Security

- Assets: objects of other instances and of users that share a name with an inventory entry; the data on PersistentVolumeClaims; the instance's inventory record.
- Trust boundaries: the API server's answers (untrusted input to the verdict); the tenant's ServiceAccount (the identity every read and delete is sent as).
- Threats and mitigations: deleting an object the instance does not own (every step goes through `ownership.CanDelete` inside `Advance`; the operator cannot skip it); deleting an object created again since the read (UID precondition from the action); a delete added later that bypasses the plan (`TestDeleteCallSites`); escalation through the wait (the wait reads with the same impersonated client and the same `get` verb; the manager's own client is never a fallback, as `prune-stale-resources` "Prune not attempted while stalled on DeletionSAMissing" requires); a tenant holding the operator's work queue with an object that never terminates (one requeue per minute per blocked instance, one read per remaining object; no render slot is held).
- Status messages and events name object kinds, namespaces, names and finalizer names. They hold no secret and no object content.
- Baseline: Kubernetes RBAC with ServiceAccount impersonation (`docs/site`, capability `serviceaccount-impersonation`). Residual risk: a stale object that sticks after a prune is not tracked (unchanged). Owner: the operator maintainers.

## Risks / Trade-offs

- [envtest runs no garbage collector, so a Foreground delete leaves every object with the `foregroundDeletion` finalizer] → section 1 is a spike that proves it and adds a test helper that plays the garbage collector; without it every deletion test waits forever. This is the one unverified assumption of the design.
- [A deletion can now wait on a finalizer the operator does not own] → `DeletionBlocked` names the object; `spec.prune: false` releases; documented.
- [`opm instance delete` waits 5 minutes by default for an operator-managed instance; a slow termination can pass that] → the cli's timeout message already says the finalizer may still be pruning; no cli change. Not verified against the cli source in this proposal.
- [A stale object pruned with Foreground and rendered again before it is gone] → the apply meets a terminating object. That belongs to the apply verdict (`guard-every-apply-by-ownership`), which reads the live `deletionTimestamp` (`ownership/apply.go:99`). Not specified here.
- [Two plans change the order for an unsettled identity] → documented above; no ordering is promised within a module.
- [Repeated DELETE of terminating objects at each recheck] → one request per remaining object per 10 s, then per minute.
- [The library package may change before v1] → the runner and three callers import it.

## Migration Plan

No user migration. Rollback is a revert of the PR. Sections: spike; runner; stale prune on the runner; deletion cleanup on the plan and the hold verdict; the wait and the docs. Sections 4 and 5 ship in the same PR as the Foreground change, so `main` never has Foreground cleanup without the wait.

## Open Questions

Both change the specs, so the proposal gate must rule before implementation. The artifacts are built for the recommendation.

1. **Q1: does the finalizer wait until deleted objects are gone?** A: no, release on the verdict (drop tasks section 5, the two reasons, the two events and two `finalizer-and-deletion` requirements). B: yes (recommended, built).
2. **Q2: is the deletion state stored in `status`?** A: no (recommended, built). B: yes, a new status field on both kinds.
3. **For the supervisor, not blocking**: 0012:D4:R1 lists one exception (the operator install). The forced recreate is a second delete outside the plan, by owner decision. Either the enhancement gains the exception or the delivery log notes it. `enhancement.yaml` claims no decision until that is settled and the apply half has merged.
