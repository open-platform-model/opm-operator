## Context

See proposal.md for the motivation and the "today / after" tables. Facts about the library package that shape this design, read from the source of the pinned version (`library v1.0.0-beta.8`, `opm/k8s/lifecycle`):

1. **A plan has one owner UUID** (`plan.go:51`). The operator judges with up to two identities, and a ModulePackage with no recorded identity also asks with none (`internal/reconcile/identity.go:8-16`, `:40-47`, `internal/apply/prune.go:196-212`).
2. **A plan has no data policy.** `Policy` holds `Prune` and `ForceOrphan` only (`plan.go:12-21`). Keeping PersistentVolumeClaims is the operator's rule.
3. **The package never waits.** A DELETE the API server accepted is `ResultDeleted` at once (`advance.go:234-239`), and `MayReleaseHold` releases when every step is deleted or skipped (`hold.go:83-123`). An object that stays Terminating is, to the plan, deleted.
4. **An object already being deleted is judged "proceed"** (`ownership/delete.go:93-94`), so a later plan over the same entry names the delete again. A second DELETE of a terminating object deletes nothing more, but it is not without effect: the API server computes the garbage collector's finalizers again from the options of every DELETE (`k8s.io/apiserver v0.36.4`, `pkg/registry/generic/registry/store.go:984-1010`, `:1082-1085`), so a repeated Foreground DELETE sets `foregroundDeletion` again after the collector removed it. The status message accounts for that (decision "What the message names").
5. **A failure does not stop the plan** (`advance.go:148-149`), as the operator's loop does not today.
6. **The hold is the instance's hold**: for the operator, the finalizer `opmodel.dev/cleanup` on the ModuleInstance or ModulePackage.
7. **The hold verdict checks an empty plan before the identity** (`hold.go:87-88` before `:89-97`).

Reversibility: two-way door. No CRD field, no stored state, no flag. A revert of the PR restores today's behaviour; objects deleted in the meantime are deleted either way. A deleting object that carries the reason `DeletionInProgress` when the revert is deployed is handled by the old loop, which ignores the reason.

## Goals / Non-Goals

**Goals:**

- Every delete of an inventory object is sent because `lifecycle.Advance` named it, with the action's propagation and precondition (0012:D4:R1).
- The cleanup finalizer is removed only after a release verdict of `lifecycle.MayReleaseHold`, and after the deleted objects are gone.
- A deletion never stalls on a lost identity once every delete was sent.
- One function in the operator performs the plan's actions.
- Every rule of #260, #262, #263, #267, #268 and #271 holds, with the two named exceptions to the `DeletionSAMissing` stall.

**Non-Goals:**

- The apply-time ownership check (`guard-every-apply-by-ownership`).
- The forced recreate: it stays in `deleteGuard`.
- Waiting for a stale object after a prune.
- Any ordering beyond the library's.
- A CRD field, a flag, a library change, a stored deletion state.

## Research & Decisions

### One runner performs every plan

**Context**: `Advance` names actions; the operator must perform them. One loop exists today, `apply.Prune`, called from four places.

**Explored**: `lifecycle/advance.go`, `plan.go`, `hold.go`; the cli's runner (cli#350, `openspec/changes/archive/2026-10-08-adopt-kubernetes-lifecycle-package/design.md`).

**Options considered**:

1. Status quo. No work; 0012:D4:R1 is not met and the operator keeps a second implementation of the order and the propagation.
2. Each reconciler calls `Advance`. Four copies of the loop.
3. One runner in `internal/apply`; `Prune` and the two deletion handlers build plans and report.

**Decision**: option 3.

**Rationale**: the runner is the single delete site, which `TestDeleteCallSitesAreClosed` (`internal/apply/callsites_test.go:237`) pins by file. The callers keep what differs: which entries, which policy, what is reported.

```go
// internal/apply/deletion.go

// StepResult is what happened to one inventory entry.
type StepResult struct {
	Entry   releasesv1alpha1.InventoryEntry
	Outcome lifecycle.Outcome
	// Failed is the action that failed (read or delete); empty otherwise.
	Failed lifecycle.ActionKind
	// Live is the object the plan's read returned; nil when it found nothing
	// or failed. It carries the deletionTimestamp and the finalizers.
	Live *unstructured.Unstructured
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

**Consequence (supervisor ruling 2 of 2026-10-09)**: the hold verdict is asked with the plans as built, after the claim split. With an inventory of kept claims only, the plan is empty and the verdict is `inventory-empty` before it looks at the identity (fact 7). The operator then removes the finalizer without a read, also when the ServiceAccount is missing. Today that deletion stalls with `DeletionSAMissing` (`moduleinstance.go:1399`, `:1410-1415`), although the cleanup would delete nothing. The claims cannot be read without the identity, so no `ClaimsKept` event would be true; the operator emits `DeletionUnconfirmed` with the number of claims left without a read. This is a behaviour change and is in the release note. With the identity available the claims are classified as usual. An inventory that also holds a Namespace or a CRD is not empty for the plan (those are steps, skipped up front), so it still needs the identity, as today.

### The finalizer follows the hold verdict

**Decision**: the deletion handlers build `lifecycle.Policy{Prune: spec.prune, ForceOrphan: annotation == "true"}` and `HoldInput{Identity}` from the impersonation result: available, `IdentityMissing` when the ServiceAccount is not found, `IdentityFailed` on any other impersonation error. They act on `MayReleaseHold` as the table in proposal.md item 2 says. With an unavailable identity no plan is run: the verdict is asked with the zero State, and it answers on the identity before it looks at the state (`hold.go:89-97`).

**Rationale**: every branch of today's handlers has one verdict reason. Statuses, events and requeue intervals stay, with the two exceptions this document names: the inventory of kept claims only (above), and the lost identity after every delete was sent (below). The verdict's message is not shown: the operator's messages name the ServiceAccount and the remedies, which the library's cannot.

### The finalizer waits until the deleted objects are gone

**Context**: fact 3, and Foreground. With Foreground a Deployment stays, Terminating, until its Pods are gone. The cli met the same gap and added `--wait` (cli#353).

**Options considered**:

| | A: release on the verdict alone | B: release on the verdict and "gone" |
| --- | --- | --- |
| Finalizer comes off | When every DELETE is accepted, as today | When every object this cleanup deleted returns NotFound or has another UID |
| Delete, then create again, with today's apply | Can apply onto terminating objects; the new instance loses them and heals at a later reconcile | Safe: the old instance exists until its objects are gone |
| Delete, then create again, after the apply guard (`guard-every-apply-by-ownership`) | The library's apply verdict refuses a terminating object (`ownership/apply.go:99`), so the new instance gets a refused apply and a retry, not lost objects | Same as above |
| Stuck dependent (Pod on a dead node) | Invisible: instance gone, Deployment terminating, nothing tracks it | Visible: the instance stays, `DeletionBlocked` names the object |
| New stuck state for the user | None | Yes: a ModuleInstance can stay Terminating on a foreign finalizer; way out is `spec.prune: false` |
| The identity must exist | For one reconcile | For one reconcile too, with the record below; without it, for the whole termination time |
| Code | None beyond the plan | A read per deleted object, two reasons, a requeue, the record |

**Decision**: B. Owner decision of 2026-10-09, relayed verbatim by the supervisor. Question: "When an instance is deleted, should the operator keep its finalizer until the deleted objects are really gone?" Selection: "Wait until gone (Recommended)".

**Rationale**: a stuck object becomes visible on the object the user deleted, and the old instance cannot be replaced while its objects are still going. After the apply guard lands, the delete-and-recreate argument is weaker (row 3); visibility is then the main gain. The trade-off given up is liveness: a deletion can wait on an object the operator does not control. The remedy is explicit (`spec.prune: false`). 0012:D4:R1 is met: the hold is released only after a release verdict; the operator holds longer than the verdict asks, which R1 does not forbid.

Mechanics:

```go
runs := runPlans(ctx, deleteClient, entries, identities, policy) // fresh plans, zero State
verdict := releaseOf(runs, holdInput)                            // every plan's MayReleaseHold
if !verdict.Release { return actOnHold(verdict) }                // statuses as today, or the record (next decision)
left := stillThere(ctx, deleteClient, runs)   // GET each ResultDeleted step; gone = NotFound or another UID
firstRelease := !carriesWaitReason(readyAtStart)
if firstRelease { reportKeptClaims(...); reportLeftBehind(...) }  // once per deletion
if len(left) == 0 { return removeFinalizer() }
age := wait.Now().Sub(oldestDeletionTimestamp(left))
if age < wait.BlockedAfter {
	markDeletionInProgress(obj, left)                             // Ready=False, Reconciling=True; event once
	return ctrl.Result{RequeueAfter: clamp(age/4, wait.MinRecheck, wait.MaxRecheck)}, nil
}
markDeletionBlocked(obj, left)                                    // Ready=False, Stalled=True; Warning event once
return ctrl.Result{RequeueAfter: wait.MaxRecheck}, nil
```

- The reconcile never blocks: it reads and requeues.
- Each recheck builds fresh plans from `status.inventory` and judges every entry again from the cluster. Entries that are gone are read once and skipped as already absent. Entries still terminating are read, and their delete is named and sent again (fact 4).
- A kept claim, an object left behind and an object already absent are not waited for.
- `spec.prune` set to false while waiting: the next reconcile gets `prune-disabled` and removes the finalizer. A spec change bumps the generation, so it triggers a reconcile at once.
- A deletion whose objects are all gone at the first gone-check (ConfigMaps, Secrets) releases in the same reconcile and never shows `DeletionInProgress`.

Reconcile phase impact: Source, Render and Apply are not touched. Prune: order and propagation change. Status: two new reasons, on a deleting object only. The deletion branch gains the wait.

### The record that every delete was sent

**Context**: blocking finding of the proposal gate. Each recheck is a fresh pass, and with an unavailable identity no plan runs and the verdict holds. A teardown that removes the instance and its ServiceAccount or RoleBinding together (one `kubectl delete -f`, a GitOps prune, or RBAC that is itself in the inventory and deleted last, at the lowest weight) would stall at the first recheck with `DeletionSAMissing` or `ImpersonationFailed`, although nothing is left to delete. The repo ships that pattern (`config/samples/opmodel.dev_v1alpha1_modulepackage.yaml`, `test/e2e/lifecycle_test.go:189-200`). Before this change the identity had to exist for one reconcile; the wait would stretch that to the whole termination time.

**Options considered**:

1. Keep the stall and document "keep the ServiceAccount until the instance is gone". Breaks the shipped sample's teardown.
2. Store the deletion `State`, or a list of deleted UIDs, in `status`. A CRD change; refused by supervisor ruling 1.
3. Use the `Ready` reason as the record: `DeletionInProgress` and `DeletionBlocked` are written only by a reconcile that reached a release verdict.

**Decision**: option 3. Owner decision of 2026-10-09, relayed verbatim by the supervisor. Question: "During that wait, the instance's ServiceAccount or its permissions disappear (for example deleted in the same kubectl command). Every delete was already sent. What should the operator do?" Selection: "Release the finalizer (Recommended)" (the operator remembers, through the instance's Ready reason, no new CRD field, that all deletes were already sent, and lets the instance go with an event saying it could not confirm the objects are gone).

**What "every delete was sent" means**: one reconcile of this deletion ended with `MayReleaseHold` releasing for every plan. By `hold.go:98-123` that is: every plan is finished, each step is `deleted` (the API server accepted the DELETE) or `skipped`, and no step failed. A reconcile in which one step failed has not sent every delete, whatever the number of accepted deletes, and writes no wait reason.

**How the record is written**: only `markDeletionInProgress` and `markDeletionBlocked` set the two reasons, only after the release verdict, in the status patch of that reconcile. If the patch fails, the record does not exist and the next reconcile is an ordinary first pass.

**How the record is read**:

```go
// readyAtStart is the Ready condition of the object as the reconcile read it,
// before any Mark* call of this reconcile.
func everyDeleteWasSent(obj metav1.Object, readyAtStart *metav1.Condition) bool {
	return !obj.GetDeletionTimestamp().IsZero() && readyAtStart != nil &&
		readyAtStart.Status == metav1.ConditionFalse &&
		(readyAtStart.Reason == status.DeletionInProgressReason ||
			readyAtStart.Reason == status.DeletionBlockedReason)
}
```

It is consulted in one place, `actOnHold`, and only for these verdicts:

| Verdict of this reconcile | Record absent | Record present |
| --- | --- | --- |
| `identity-unavailable` (ServiceAccount missing, or impersonation failed) | Stall `DeletionSAMissing` or `ImpersonationFailed`, as today | Remove the finalizer; `DeletionUnconfirmed` event; no read, no delete |
| `cleanup-forbidden`, and every failed step is a Forbidden **read** | Stall `ImpersonationFailed`, as today | Those entries are "not confirmed". If a readable deleted object still exists, keep waiting; else remove the finalizer with the event |
| `cleanup-forbidden` with a Forbidden **delete** of an object that already has a `deletionTimestamp` | Stall, as today | That object is still terminating: keep waiting |
| Any other hold (`cleanup-incomplete`, a Forbidden delete of an object that is not terminating) | Hold and retry, as today | Hold and retry, as today |
| Release | Not consulted | Not consulted |

While the identity works, the record changes nothing: the fresh pass is the authority, and an object that somehow was not deleted is deleted by it.

**How far the record is trusted**: it lives in `status`, so it can be written by whoever may write `moduleinstances/status` or `modulepackages/status`. In the operator's install that is the manager only: the roles for users grant `get` on the status subresource and no write verb (`config/rbac/moduleinstance_editor_role.yaml:31-33`, `moduleinstance_admin_role.yaml:25-27`; the viewer roles read only; no role for users names `modulepackages/status`). A delta-spec scenario and a test keep it so. A cluster admin who grants status write gives that principal one new ability: when the identity is also unavailable, to make the operator release the finalizer of a **deleting** object early. That leaves objects in the cluster; it never deletes one, and it is what `spec.prune: false` does for anyone who may edit the spec. The record is ignored on an object without a `deletionTimestamp`, and a live reconcile overwrites the condition. The field manager name in `managedFields` is self-declared by the client, so it is not used as proof.

**Rationale**: the fact that must survive a reconcile is one bit, and the status already holds it. 0012:D4:R1 is met: the release follows a release verdict, obtained in an earlier reconcile of the same deletion.

**What this gives up**: after the release the operator cannot say whether the objects are gone. The event says so. A change of `spec.dataPolicy` to `Delete` during the wait is honoured only while the identity works; with the identity lost the claims are left, which is the safe direction.

### The state is not stored

**Context**: 0012:D4 says the state is a serialisable value "so the operator carries it across reconciles". `02-design.md` of the entry words it as "can".

**Options considered**:

1. Store `lifecycle.State` in `status`. A new status field (CRD change), and a stored state can disagree with an inventory that changed; `Advance` then refuses it and the operator needs a reset rule.
2. Each reconcile starts from the zero State and drives the plan to done.

**Decision**: option 2 (supervisor ruling 1 of 2026-10-09), with the record above as the only carried fact.

**Rationale**: a plan over a few dozen entries finishes inside one reconcile, as `apply.Prune` does today. A re-run sends no delete to an absent object. 0012:D4:R5 is a property of the library, already tested there.

### The threshold and the clock are test seams, not flags

**Context**: the API server sets `deletionTimestamp`; a test cannot backdate it. Without a seam the blocked scenario needs a 10-minute sleep.

**Decision**: the reconciler params of both kinds carry one value:

```go
// DeletionWait tunes the wait of a deletion cleanup. The zero value is
// replaced by the defaults; cmd/main.go sets nothing.
type DeletionWait struct {
	BlockedAfter time.Duration    // default 10 * time.Minute
	MinRecheck   time.Duration    // default 5 * time.Second
	MaxRecheck   time.Duration    // default 60 * time.Second
	Now          func() time.Time // default time.Now
}
```

A test sets `Now` 11 minutes ahead, or `BlockedAfter` to zero. No flag is added (Principle VII): nobody asked to tune these, and a flag is a contract.

**Rationale for the interval**: `clamp(age/4, 5 s, 60 s)`, where age is the age of the oldest terminating object. It needs no stored counter, is quick while most deletions finish (a Pod's default grace period is 30 s), and backs off on its own. The constants are a judgment, not a measurement.

### The cost of a recheck

For one deleting object with E inventory entries, of which S are safety-excluded, K are kept claims and T are still terminating, one recheck sends, as the tenant's identity:

- E - S - K GETs: the plan reads every entry it may delete, also those that are gone (`advance.go:194-195`);
- T DELETEs: the delete of each terminating entry is named again;
- T GETs: the gone-check of each deleted step;
- K GETs: the classification of kept claims.

That is at most E + T requests, about 2E while everything is terminating and E once most are gone. It is not "one read per remaining object", as the first draft said.

Number of rechecks with `clamp(age/4, 5 s, 60 s)`: about 4 in the first 20 s, about 11 more until the interval reaches 60 s at 4 minutes, then one per minute; about 21 in the first 10 minutes. For E = 50 and T = 5 that is about 55 requests per recheck and about 1,200 requests in 10 minutes for one deleting object. This is arithmetic from the formula, not a measurement.

What bounds it: the interval (never under 5 s, 60 s when blocked); one reconcile at a time per object; the controller's `MaxConcurrentReconciles` (`internal/controller/moduleinstance_controller.go:202`); and the wait ends when the objects are gone, when `spec.prune` is set to false, or when the identity is lost. A blocked deletion costs E + T requests per minute for as long as it is blocked.

Against the coming apply guard: `guard-every-apply-by-ownership` (merged as opm-operator#274) adds one GET per rendered object before each apply (`internal/apply/guard.go:137`, read after the merge), for every live instance at every reconcile that applies. One recheck therefore costs about the reads of one guarded apply of the same instance. The guard's load is steady and on every instance; the wait's load is on deleting objects only and ends. The two never add up on one object: a deleting object applies nothing.

### What the message names

**Decision**: the `DeletionInProgress` and `DeletionBlocked` messages list at most ten objects and count the rest; for each object at most three finalizers and a count of the rest; finalizers other than `foregroundDeletion` first. When `foregroundDeletion` is the only finalizer, the message says the object waits for its dependents.

**Rationale**: every repeated Foreground DELETE sets `foregroundDeletion` again (fact 4), so it is on every waiting object and rarely the cause. Finalizer names are written by anyone who may write the object, so their number in a status message is bounded.

### Kept claims and left-behind objects are reported once

**Context**: `reportKeptClaims` and `reportLeftBehind` emit on every call (`moduleinstance.go:1448-1449`, `:1637-1642`, `:1675`). Today they run once, because the finalizer goes in the same reconcile. With rechecks they would repeat for as long as the deletion waits.

**Decision**: both are emitted by the first reconcile that reaches a release verdict, that is when the record is absent at the start of the reconcile. A recheck (record present) emits neither. The two new events are deduplicated the same way the stall events are today (`readyAlreadyStalledWith`).

**Rationale**: the record already says "this deletion reported". A kept or left-behind set that changes during the wait is not reported again; the first report is the one the user acts on.

### The forced recreate stays outside the plan

**Decision**: `deleteGuard` (`internal/apply/claims.go:141-183`) is unchanged and stays an allowed delete site.

**Rationale**: owner decision "UID precondition only". The delete is sent by Flux's engine in the middle of an apply, with Background propagation (`fluxcd/pkg/ssa v0.77.0`, `manager_apply.go:144`), and the object is created again at once; a Foreground delete would leave the name terminating. The object is not leaving the inventory, so it is no step of a deletion plan. 0012:D4:R1 names one exception and this is not it; see Open Questions.

### Stale prune: no hold, no wait

**Decision**: `apply.Prune` keeps its signature and its result type and becomes a caller of the runner, with `Policy{Prune: true}` (the reconciler checks `spec.prune` before, as today). It asks no hold verdict and waits for nothing.

**Rationale**: a prune holds nothing. A stale entry leaves the inventory when its delete is accepted, as today. Keeping `Prune` and `PruneResult` keeps the `prune-stale-resources` requirements that name them true without a rewrite.

## Security

- Assets: objects of other instances and of users that share a name with an inventory entry; the data on PersistentVolumeClaims; the instance's inventory record; the `Ready` reason as the record that every delete was sent.
- Trust boundaries: the API server's answers (untrusted input to the verdict); the tenant's ServiceAccount (the identity every read and delete is sent as); write access to the status subresource (the record).
- Threats and mitigations:
  - Deleting an object the instance does not own: every step goes through `ownership.CanDelete` inside `Advance`; the operator cannot skip it.
  - Deleting an object created again since the read: the UID precondition from the action.
  - A delete added later that bypasses the plan: `TestDeleteCallSitesAreClosed`.
  - Escalation through the wait: it reads with the same impersonated client and the same `get` verb. The manager's own client is never a fallback, also not when the identity is lost; the operator releases without reading.
  - A forged record: see "How far the record is trusted". Effect bounded to an early release of a deleting object's finalizer; no delete; shipped roles give users no status write.
  - A tenant loading the API server with an object that never terminates: E + T requests per minute per blocked object, as the tenant's own identity; no render slot is held.
- Status messages and events name object kinds, namespaces, names and at most three finalizer names per object. They hold no secret and no object content.
- Baseline: Kubernetes RBAC with ServiceAccount impersonation (capability `serviceaccount-impersonation`). Residual risks, both low, owner the operator maintainers, to be looked at again when 0012 graduates: a stale object that sticks after a prune is not tracked (unchanged); after a `DeletionUnconfirmed` release, objects may still exist and nothing tracks them (new, by owner decision).

## Risks / Trade-offs

- [envtest runs no garbage collector, so a Foreground delete leaves every object with the `foregroundDeletion` finalizer] → proven by the spike (`test/integration/apply/foreground_delete_test.go`, envtest 1.35.0 and 1.36.2): after a Foreground DELETE a Deployment and a ConfigMap both stay, with a `deletionTimestamp` and `foregroundDeletion` as their only finalizer, for as long as nothing removes it; the API server sets the finalizer on every kind, with or without dependents. A second Foreground DELETE of the terminating object is accepted and changes nothing. The helper `test/collector` plays the collector for the whole of each of the four envtest suites: it removes `foregroundDeletion` from every terminating object of every served resource (found by discovery, so custom kinds are covered), keeps every other finalizer, deletes no dependent (envtest runs no workload controller, so there is none), and can be paused for one namespace. With it a Foreground delete ends in NotFound within one sweep (50 ms plus the sweep itself), not at once: an integration test that reads NotFound in the statement after a prune or a deletion must poll. "Pass unchanged" for existing tests holds for unit tests only.
- [The fake client of the unit tests acts on no delete option] → checked (`internal/apply/fake_client_test.go`, controller-runtime v0.24 fake client): it deletes at once under Foreground propagation and under a UID precondition that does not match. A unit test can assert the order of the requests and the options each DELETE was sent with (propagation, precondition UID), read through an interceptor, and every outcome that follows from an injected error (Conflict, Forbidden, NotFound). Only envtest can assert that the API server refuses a replaced object (`test/integration/apply/prune_replaced_test.go`) and that an object stays terminating until it is collected.
- [A deletion can now wait on a finalizer the operator does not own] → `DeletionBlocked` names the object; `spec.prune: false` releases; documented as visible text.
- [The identity is removed before every delete was sent] → the deletion stalls as today. The release note says: delete the instance first, its ServiceAccount after it. With RBAC in the inventory, the plan deletes it last (lowest weight), after every other delete was accepted, so the record is written in the same reconcile.
- [Two plans and RBAC in the inventory: plan 1 deletes a RoleBinding before plan 2 reads] → plan 2's reads are Forbidden and no record exists yet, so the deletion stalls with `ImpersonationFailed`. Only while an identity change is unsettled. Not solved here; named for the reviewer of the implementation.
- [After a `DeletionUnconfirmed` release nobody knows whether the objects went] → the event says so and names the count; owner decision.
- [The per-kind default propagation of today is not proven] → `k8s.io/kubernetes` is not in the module cache. If a kind orphans by default, this change newly deletes its dependents. Task 6.3 adds an e2e spec with a Job.
- [`opm instance delete` waits 5 minutes by default for an operator-managed instance; a slow termination can pass that] → the cli's timeout message already says the finalizer may still be pruning; no cli change. Read from the operator's docs page, not from the cli source.
- [A stale object pruned with Foreground and rendered again before it is gone] → the apply meets a terminating object. That belongs to the apply verdict (`guard-every-apply-by-ownership`). Not specified here.
- [A kept claim that carries an owner reference to a deleted object is collected by the garbage collector and still reported as kept] → existing, unchanged by this change (Foreground and Background collect the same dependents). Not solved here.
- [The cost of a recheck is about 2E requests] → see "The cost of a recheck".
- [The library package may change before v1] → the runner and three callers import it.

## Migration Plan

No user migration. Rollback is a revert of the PR. Six sections, two implement launches, one PR; nothing merges between the launches (proposal.md, "Impact").

## Open Questions

None blocks implementation. One is for the supervisor: 0012:D4:R1 lists one exception (the operator install). The forced recreate is a second delete outside the plan, by owner decision, and the `DeletionUnconfirmed` release rests on a verdict of an earlier reconcile. Either the enhancement gains a note or the delivery log carries it. `enhancement.yaml` claims no decision until that is settled and the apply half has merged.
