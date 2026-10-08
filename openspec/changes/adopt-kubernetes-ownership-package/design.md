## Context

See `proposal.md` for the motivation. The state of the operator at the base of this change (`main` at 0f75ccb, library v1.0.0-beta.7):

- `internal/apply/prune.go`: `Prune` skips a `Namespace` and a `CustomResourceDefinition` by kind alone (`isSafeToDelete`), reads each stale object, compares the managed-by label and the UUID label itself, keeps a PersistentVolumeClaim unless `DeleteData`, and deletes with no precondition. A skip is a log line and a count.
- `internal/reconcile/moduleinstance.go`: the render writes `status.instanceUUID` in memory before anything is applied (line 487), and the deferred status commit patches it on every outcome. The stale prune (`pruneStaleResources`) and the deletion cleanup (`handleDeletion`) both hand `mi.Status.InstanceUUID` to `Prune`, so after an identity change the stale prune judges with the new identity only, and every stale object, which carries the earlier one, is skipped and dropped from the inventory.
- `internal/reconcile/modulepackage.go`: both prune calls pass an empty identity, so only the managed-by label guards a package's deletes. `ModulePackageStatus` has no identity field.
- `internal/apply/apply.go`, `claims.go`, `manager.go`: `Apply` runs Flux `ApplyAllStaged` with `ForceOwnership`. No ownership check runs before it. With `Force`, Flux deletes and recreates an object whose update the API server refuses; it sends that delete through the resource manager's client with the live object it read (`fluxcd/pkg/ssa` `manager_apply.go`), and that client is `claimGuard`, which protects claims (#268). `claimGuard.DeleteAllOf` passes every kind but claims through; nothing calls it.
- No other code applies or deletes a cluster object: the only delete call sites outside tests are `internal/apply/prune.go` and `internal/apply/claims.go`. The Platform and TransformerRegistration controllers write status and finalizers of their own kinds only.

Constraints:

- The library verdicts are pure. The caller reads the live object and hands it in; the library words every skip (`opm/k8s/ownership` package doc). `CanDelete` takes one identity. With an empty identity it compares no UUID label and skips every object that carries a non-blank adopt annotation, also one that names this instance (library test "an empty instance UUID skips an annotated object"). The adopt annotation is no delete override (library test "the adopt annotation is no delete override").
- The contract is 0012:D4:R1/R2, 0012:D7:R1 and 0012:D8:R1 to R8, with the "annotation only" rule of 0012:D8:R8 (library ADR-013, row e4).
- What #260 to #269 delivered MUST NOT change: the narrowed impersonation, the periodic reconcile and the restore, the stuck-delete fixes, held claims, drift as the identity that applies, `spec.dataPolicy` on prune, deletion and forced recreate.
- The deletion protocol (order, finalizer holds, propagation policy) is a later change. This one keeps the prune loop and changes only the per-object decision and the precondition.

Reversibility: costly two-way for the code. The parts that are hard to take back are owner decisions and are not decided here: the contract of 0012:D8, the additive `status.instanceUUID` on ModulePackage (2026-10-03), and the second status field that keeps the earlier identity (2026-10-08). A status field, once released, stays.

## Goals / Non-Goals

**Goals:**

- One function, the library's, decides ownership for every object the operator deletes on behalf of an instance in a prune or a deletion cleanup. A test keeps the list of delete call sites closed.
- Every delete carries the UID precondition of the object that was read.
- No instance deletes an object whose adopt annotation names another instance.
- An identity change never orphans an object: not the stale ones, and not the live ones when the instance is deleted before the change is settled.
- One design for both halves, so the apply half starts from recorded decisions.

**Non-Goals:**

- The apply guard itself (the change `guard-every-apply-by-ownership`).
- An ownership question on the forced recreate (owner decision of 2026-10-08).
- The deletion protocol, `opm/k8s/lifecycle`, foreground propagation.
- RBAC, and any change to which identity the operator impersonates.
- A command, field or annotation by which the operator sets the adopt annotation (0012:D8:R6 excludes it).
- The `Admit` input of the verdicts. It exists for `opm operator install` only; the operator MUST never set it.

## Decisions

### Every path, both halves

The operator MUST act on each path as stated. "Half" says which change builds the row: D is this change, A is `guard-every-apply-by-ownership`. The rows hold for ModuleInstance and ModulePackage alike unless a row says otherwise. "The identities" are defined under "Which identities judge".

| # | Path | Half | Verdict and identity | Answer | Operator action (condition, reason, event) |
| --- | --- | --- | --- | --- | --- |
| 1 | Apply of a changed render (`apply.Apply`) | A | `CanApply` for every object of the apply list, before the first write. `InstanceUUID` is the render's. `InInventory` is true when `status.inventory` lists the object. `Admit` false. | allowed | Apply. |
| | | | | `terminating`, `foreign-object`, `other-instance` | Refuse the whole apply before any write. `Ready=False`, reason `ApplyRefused`, not Stalled; `Warning` event `ApplyRefused`, action `Apply`; the library's line for each refused object. Retry on the bounded backoff. |
| | | | | `adopted-elsewhere` | Do not apply the object and leave it out of the inventory. The apply goes on. `Warning` event `AdoptedElsewhere`, action `Apply`. |
| | | | | (no verdict) read error other than NotFound | Fail closed before any write, as a failed apply is handled today. |
| 2 | Restore of missing objects (#261) | A | The same guard over the restore list. | allowed (the object is missing) | Create it. |
| 3 | NoOp reconcile with a render | A | `CanApply` over the apply list, for the let-go answer only. | `adopted-elsewhere` | The object is left out of drift detection and of the restore, and its entry leaves `status.inventory`. |
| 4 | Drift dry-run, `checkClaims` dry-run | none | none | | No write. Unchanged. |
| 5 | Forced recreate inside the apply (`forceConflicts`, #268) | D | No ownership verdict (owner decision of 2026-10-08: "UID precondition only"). | | The delete carries a precondition on the UID of the live object Flux read. A delete refused on it fails the apply: `Ready=False` `ApplyFailed`, retried, as any failed apply. The claim rule of #268 is checked first and is not changed. A delete of a collection is refused. |
| 6 | Prune of stale objects | D | `CanDelete`, asked with each of the identities in turn until one says proceed. | proceed | DELETE with `verdict.Preconditions()`; counted as deleted. A claim without `spec.dataPolicy: Delete` is kept instead (#267), after the verdict. |
| | | | | `already-absent` | Done, as today. |
| | | | | `safety-excluded`; or `not-opm-managed`, `owner-mismatch`, `adopted-elsewhere` for every identity | Left in the cluster; the entry leaves the inventory with the apply's commit, as today. One `LeftBehind` event for the run, action `Prune`: `Normal` when only safety-excluded kinds were left, `Warning` otherwise. The reconcile still ends `Ready=True`. |
| | | | | (no verdict) read error other than NotFound; DELETE refused on the precondition; any other DELETE error | A failed prune, as today: `Ready=False` `PruneFailed` with backoff, or `Stalled` `ImpersonationFailed` on Forbidden with an effective ServiceAccount. The inventory is not replaced, the earlier identity stays recorded, and no `LeftBehind` event is emitted. An unreadable claim under `Keep` stays kept without an error (#267). |
| 7 | Deletion cleanup (`spec.prune: true`) | D | `CanDelete` for every inventory entry, with the identities of the deletion path. | proceed | DELETE with the precondition. |
| | | | | any skip | Left in the cluster. It does not hold the finalizer. One `LeftBehind` event, action `Delete`, in the reconcile that removes the finalizer and before it does. |
| | | | | (no verdict) or a failed DELETE | The finalizer is held and the cleanup is retried, as today. |
| 8 | Deletion with `spec.prune: false`; orphan exit (#263) | none | none | | Nothing is deleted. Unchanged. |
| 9 | `spec.owner: cli`; the operator's own instance | none | none | | No apply, prune or delete. Unchanged. |

A ModulePackage has no restore and no drift detection today, so rows 2 and 3 do not exist for it.

### Shape in the code (this change)

```go
// api/v1alpha1: ModuleInstanceStatus and ModulePackageStatus

// InstanceUUID is the identity of the instance the operator last rendered and
// began to apply: the value of the module-instance.opmodel.dev/uuid label on
// its objects. The prune and the deletion cleanup use it to tell the
// instance's own objects from another instance's.
// +optional
InstanceUUID string `json:"instanceUUID,omitempty"`

// PreviousInstanceUUID is the identity the instance had before its identity
// last changed. It is set only while that change is not settled: from the
// apply that starts to relabel the instance's objects until a reconcile has
// applied and pruned with success. While it is set, an object that carries
// either identity counts as the instance's own.
// +optional
PreviousInstanceUUID string `json:"previousInstanceUUID,omitempty"`
```

Both are plain strings with no validation pattern: the operator is their only writer on an operator-owned object, and a value that is no UUID only matches nothing.

```go
// internal/apply

// LeftBehind is one object a prune did not delete because the delete verdict
// skipped it, with the library's reason and message.
type LeftBehind struct {
    Entry   releasesv1alpha1.InventoryEntry
    Reason  ownership.SkipReason
    Message string
}

type PruneResult struct {
    Deleted int
    Skipped int          // len(Left); kept for the log line and the tests
    Left    []LeftBehind // safety-excluded included
    Kept    []releasesv1alpha1.InventoryEntry
}

// ErrReplaced reports a DELETE the API server refused on the UID
// precondition: the object was replaced since it was read. It is never
// counted as deleted. Recognised by the API status, never by message text.
var ErrReplaced = errors.New("object was replaced since it was read")

// Prune takes the identities to judge with, most recent first. An empty
// list judges with no identity.
func Prune(ctx context.Context, c client.Client, identities []string,
    stale []releasesv1alpha1.InventoryEntry, opts PruneOptions) (*PruneResult, error)
```

Order inside `Prune`, per entry: `ownership.SafetyExcluded` (no read) -> read -> `ownership.CanDelete` with the first identity, and with the next one while the answer is `owner-mismatch` or `adopted-elsewhere` -> the claim rule of #267 -> DELETE with `verdict.Preconditions()`. When no identity lets the delete proceed, the object is left behind with the reason and message of the first verdict. `isSafeToDelete` and the two label comparisons go. Asking the library twice is not a rule of the operator's own: every answer is the library's.

The forced recreate: `claimGuard.Delete` adds a precondition on the UID of the object it is handed, which is the live object Flux read. An object without a UID is deleted without one, as the library's `Preconditions` does. `claimGuard.DeleteAllOf` refuses every kind. `ApplyOptions` does not change.

Reconcile phase impact: Source and Render: none. Apply: one status patch before the first write when the identity changed; the UID precondition on a forced recreate. Prune: the verdict, the identities, the precondition, the event. Status: `status.previousInstanceUUID` on both kinds; `status.instanceUUID` on ModulePackage; a new `Ready` reason.

### Research & Decisions

#### Which identities judge

**Context**: `CanDelete` skips as `owner-mismatch` when the live UUID label and the given identity are both set and differ. An instance's identity is a UUID v5 of its module path without the major, its name and its namespace (core `src/module_instance.cue`, as the cli's design reads it). A ModuleInstance's name and namespace are its object's and cannot change, so its identity changes only when `spec.module.path` changes. When an identity changes from A to B, the apply relabels to B every object it still renders; the stale objects keep A. Until a reconcile has applied and pruned with success, the cluster holds objects of both identities.
**Explored**: `moduleinstance.go:487`, `:640-675`, `:1293`, `:1469`; the cli's archived design and build report (`reports/T3.2.md`: a failed prune after an identity change leaves its entries behind on the retry, accepted there as documented); the design review of this proposal (`reviews/T2.2-design.md`, blocking 1).
**Options considered**:
1. Status quo: the new identity alone. The stale objects are skipped and abandoned.
2. The earlier identity alone until a reconcile succeeded. The stale objects are deleted, but an instance deleted in the window leaves every relabelled object behind.
3. The new identity in status, the earlier one in memory for that reconcile's prune. A failed prune leaves the stale objects on the retry (the cli's documented leftover).
4. Both identities in status until the prune succeeded; an object that carries either is the instance's own.
**Decision**: Option 4 (owner decision of 2026-10-08: "Keep both identities until done": the status holds the earlier and the new identity until the prune of that reconcile succeeded; the deletion cleanup and the stale prune accept an object that carries either; then the earlier one is cleared). It builds on the owner decision of the same day that prune judges with the identity stored in the instance's record, also after the identity changed.
**Rationale**: Nothing is orphaned in either direction, and the rule survives an operator restart, because both identities are stored before the first relabel. The cost is one more optional status field on each kind.

The identities, by path:

| Path | `status.instanceUUID` at the start | Identities handed to the verdict |
| --- | --- | --- |
| Stale prune | set | `status.instanceUUID`, then `status.previousInstanceUUID` when set (both as stored before the apply) |
| Stale prune | empty | the render's identity, then no identity (see "No recorded identity") |
| Deletion cleanup | set | `status.instanceUUID`, then `status.previousInstanceUUID` when set |
| Deletion cleanup | empty | no identity |

#### When the identities are written, and the bound on a second change

**Decision**:

- Before the first write of an apply, when the render's identity differs from `status.instanceUUID`, the reconciler MUST store the new state with a status patch of its own, and MUST NOT apply when that patch fails. Recorded identity empty: `instanceUUID` becomes the render's, `previousInstanceUUID` stays empty. Recorded identity A, render B, nothing pending: `instanceUUID` B, `previousInstanceUUID` A. Recorded B with A pending, render A (the user went back): `instanceUUID` A, `previousInstanceUUID` B.
- `previousInstanceUUID` is cleared in the status commit of a reconcile whose apply and prune succeeded, with the inventory, and at no other time.
- A reconcile that finds nothing to apply writes `instanceUUID` when it is empty and touches neither field otherwise.
- A second change before the first is settled: recorded B with A pending, and a render of a third identity C. The reconciler MUST NOT apply. It reports `Ready=False`, `Stalled=True`, reason `IdentityChangeUnsettled`, with one `Warning` event, and a message that says to restore the earlier module path, wait until the instance is Ready, and change it again. Both fields stay. The deletion cleanup still works and accepts A and B.

**Options considered for the second change**: (1) refuse, as decided; (2) keep the oldest identity and drop the middle one, which orphans whatever the unsettled apply relabelled; (3) a list of earlier identities in status, unbounded by nature.
**Rationale**: A single earlier identity is enough because a third is never let in. The case needs two path changes with a reconcile between them that rendered and then failed; the price is a refusal with a remedy, and nothing is orphaned.

| Event | What the operator does |
| --- | --- |
| `spec.module.path` of a ModuleInstance changes (A to B) | Stores B with A pending, then applies, which relabels the rendered objects to B. The stale prune deletes stale objects that carry A or B. The success commit clears the pending identity. |
| The apply or the prune of that reconcile fails, or the apply is refused (`DependentsRemain`, `ClaimConflict`) | Both identities and the inventory stay. The retry prunes with both, so no stale object is orphaned. |
| The instance is deleted before the change is settled | The cleanup accepts A and B, so the relabelled objects and the ones not yet relabelled are both deleted. |
| The user goes back to A before the change is settled | The identities swap; the retry relabels back and settles. |
| A third identity before the change is settled | Refused: `IdentityChangeUnsettled`. |
| The instance name or namespace changes | That is another ModuleInstance object with its own, empty record. The earlier object's cleanup judges with its own identities. |
| The instance inside a ModulePackage changes name, namespace or module path | As the first row: one record, both identities until settled. |
| A stale object carries a UUID label that is neither identity | It is another instance's: left behind, reported. |
| An object the instance adopted under A, whose annotation still names A after the change is settled | Left behind as `adopted-elsewhere`: the annotation names an identity the instance no longer has. 0012:D8 asks the user to annotate it again with the new identity. |

#### No recorded identity

**Context**: A ModulePackage has no recorded identity until its first render under this release. A package that is suspended, waits for a dependency or fails before its render does not gain one in that state. Today a package's prune and cleanup compare no identity. A ModuleInstance has an empty field only when a release older than the field recorded its inventory.
**Decision**: One rule for both kinds and every path: while nothing is recorded, the verdict is asked with no identity, which is what a package's deletes do today. On the stale-prune path the render's identity is known, so it is asked first; this lets an object annotated for this instance be deleted when its UUID label is this instance's or absent. On the deletion path no identity is known, so an object with any adopt annotation is left behind, also one that names this instance, because the operator cannot know that it does. It is reported in `LeftBehind` and does not hold the finalizer: a held finalizer there could be released by nobody.

The first reconcile after the upgrade that renders writes `status.instanceUUID`: before its apply when it applies, in its NoOp commit when it does not. `status.previousInstanceUUID` stays empty: the operator cannot know an earlier identity it never recorded.
**Rationale**: No package loses a delete it has today, and the one case where the empty identity is stricter than the label check is stated.

#### ModulePackage keeps a persisted identity

**Context**: 0012:D8:R4. Today a ModulePackage persists no identity: its rendered objects carry the UUID label, but both prune calls pass an empty identity (`modulepackage.go:762`, `:945`).
**Options considered**: (1) status quo, the managed-by label alone; (2) read the identity from the live objects at delete time, which trusts the labels the guard is meant to check; (3) an additive `status.instanceUUID`, written as a ModuleInstance's.
**Decision**: Option 3 (owner decision of 2026-10-03, recorded in 0012:D8: "ModulePackage gets a persisted UUID in an additive status field so the UUID guard applies"), with `status.previousInstanceUUID` beside it as on a ModuleInstance.
**Rationale**: It is the decided contract, and the same two fields on both kinds let one helper serve both.

#### Delete precondition

**Options considered**: (1) status quo, none; (2) the UID; (3) the UID and the resourceVersion.
**Decision**: Option 2, on the prune, the deletion cleanup and the forced recreate.
**Rationale**: The UID closes the case that matters, an object deleted and recreated under the same name since the read. A resourceVersion precondition fails on any status write between the read and the DELETE, as the library's doc states. A DELETE the API server refuses on the precondition is `ErrReplaced`: a failed delete on every path, never a success; the next reconcile reads the new object.

#### The forced recreate

**Context**: With `spec.rollout.forceConflicts`, Flux deletes an object whose update the API server refuses and creates it again. Flux counts every Conflict and Invalid answer of its dry-run as such a refusal. The first proposal asked the delete verdict there; the design review showed that the refusal would fire only when no in-place update exists, would print no remedy, and would not be lifted by the adopt annotation.
**Options considered**: (1) status quo; (2) ask the delete verdict and refuse (`RecreateRefused`); (3) the UID precondition only; (4) judge an inventoried or adopted object by the apply rule, which needs a new library input.
**Decision**: Option 3 (owner decision of 2026-10-08: "UID precondition only": the forced recreate does not ask the ownership question; it deletes exactly the object that was read).
**Rationale**: The forced recreate is part of an apply, and the apply's ownership question is the apply guard's (the other change). What this change can close without a new refusal is the replaced object. The `spec.dataPolicy` rule of #268 stays as it is.

#### Reporting what a prune left behind

**Options considered**: (1) status quo, a log line and a count; (2) one event per object; (3) one event per run, shaped as `ClaimsKept`.
**Decision**: Option 3: reason `LeftBehind`, action `Prune` or `Delete`; the count, then the library's message for each object (at most ten, fewer past 1024 characters, then the number of the rest). The type is `Normal` when every object left is of a safety-excluded kind and `Warning` when at least one was left for an ownership reason (supervisor ruling of 2026-10-08: an instance that renders a Namespace must not warn on every deletion). `already-absent` is not reported. The event is emitted only by a run whose result is committed: a stale prune that returned no error, and the cleanup that removes the finalizer. A failed run emits none, because its retry judges the same entries again.
**Rationale**: The library's message is the text the cli prints, which is the point of 0012:D4. A skipped object leaves the inventory in the same reconcile, so each is reported once.

#### The apply half: decisions recorded here

These are the starting point of `guard-every-apply-by-ownership`. They mirror the cli's design of the same name unless stated. Four of them are still questions at the gate (the class of a refusal, how a let-go object stays visible, the reading identity, the guard on every render), and that change carries its own threat statement.

- Where the guard runs: one pass over the apply list before the first write, so a refused apply has written nothing. It runs on every reconcile that renders, because drift detection and the restore must leave a let-go object out.
- A refusal is `Ready=False` with reason `ApplyRefused`, not Stalled, on the bounded backoff: the remedy is on another object, which no watch of the instance reports. `ClaimConflict` and `DependentsRemain` have this class.
- A let-go object stays out of the apply, out of drift detection and out of the inventory, and the stale set is computed from the full render, so no delete of it is attempted. The delete verdict of this change is the second lock.
- The guard's `InstanceUUID` is the render's; inside the inventory a UUID label alone never refuses, so the window of an identity change needs no second identity on the apply side.
- The operator has no dry run and no install command, so the cli's dry-run and install decisions have no counterpart.

### The handover between cli and operator ownership

- `spec.owner: cli`: the operator applies, prunes and deletes nothing, so it asks no verdict. The release of a leftover finalizer without pruning (#263) is not changed.
- cli to operator: the first operator reconcile reads the inventory and the identity the cli recorded in the same status fields. Both sides compute the identity with the same formula, and the verdict accepts every OPM manager label value, so the prune judges the cli's objects as its own.
- operator to cli: the cli's prune judges with the record's `status.instanceUUID`, which is the new identity from the start of an identity change, as today. The cli does not know `status.previousInstanceUUID`. A handover inside the window of an unsettled change therefore falls back to the cli's documented leftover (stale objects of the earlier identity are left behind). Teaching the cli the field is a follow-up.
- The cli finds an instance by `--instance-id` through `status.instanceUUID`. That field still holds the newest identity as soon as an apply starts, so the lookup does not change.
- The operator's own instance is never reconciled, so it is not affected.

### The PVC rule

`spec.dataPolicy` keeps its meaning and its order. On prune and deletion a claim counts as kept only when it exists and the delete verdict lets the operator delete it; a claim the verdict skips is left behind, not kept, as today. An unreadable claim under `Keep` stays kept without an error. On the forced recreate the claim rule runs before the UID precondition and is not changed. A kept claim still leaves the inventory.

One consequence for the apply half: a kept claim keeps the labels of the identity that applied it. After an identity change the apply half refuses it as `other-instance`; with the same identity it is taken back, as the spec scenario "A render takes a kept claim back" requires.

### ServiceAccount impersonation

Every read that feeds a verdict MUST be made by the client that would delete the object: the impersonated ServiceAccount when one is effective, the operator's own identity otherwise. This is what the prune's read does today and what #265 set for drift. This change adds no read: the prune already reads each object, and the forced recreate uses the object Flux read.

**Options considered**: (1) the acting identity; (2) the operator's own identity, which can read more.
**Decision**: Option 1.
**Rationale**: The operator acts for a tenant. A verdict from a read the tenant's ServiceAccount could not make would let the tenant learn, through a message, the owner of an object it may not read. A refused read fails closed with the outcome it has today: the stale prune stalls with `ImpersonationFailed` and keeps the inventory, the cleanup keeps the finalizer.

### What changes for a user

| What | Before | After | Breaking |
| --- | --- | --- | --- |
| Stale or deleted instance's object whose adopt annotation names another instance | deleted when the labels match | left behind, `LeftBehind` event | yes |
| Kind `Namespace` or `CustomResourceDefinition` in another API group | never deleted | deleted as any object | yes |
| Stale objects after `spec.module.path` changed | skipped and abandoned | deleted | yes |
| Instance deleted before an identity change is settled | relabelled objects deleted, others left | both deleted | no (fewer leftovers) |
| A second identity change before the first is settled | applied | refused: `Ready=False`, `Stalled`, `IdentityChangeUnsettled` | yes |
| `status.previousInstanceUUID` on both kinds | absent | present while an identity change is not settled | no (additive) |
| `status.instanceUUID` of a ModuleInstance | written at the end of every attempt that rendered | written before the apply when it changes | no |
| `status.instanceUUID` of a ModulePackage | absent | present after the first render | no (additive) |
| ModulePackage stale object carrying another instance's UUID, once an identity is recorded | deleted (managed-by label only) | left behind | yes |
| Object with an adopt annotation in the inventory of an object with no recorded identity, on deletion | deleted on its managed-by label | left behind | yes |
| Forced recreate of an object replaced since Flux read it | the new object is deleted | `ApplyFailed`, retried | no |
| DELETE of a stale object replaced since the read | the new object is deleted | `PruneFailed`, retried | no |
| Skipped object on prune or deletion | log line, `Skipped` count | the same, and one `LeftBehind` event | no |
| New reasons | | `LeftBehind` (event), `IdentityChangeUnsettled` (event and `Ready` reason) | no |
| Metrics, `spec` | | no metric changed, no spec field | no |

The apply half adds: `Ready=False` `ApplyRefused` and its event, the `AdoptedElsewhere` event, the inventory without let-go objects, and a refused apply where a ServiceAccount may patch an object but not read it.

### Refusals that hit a user's own objects

The cli's design review found three cases with a manual remedy only (`reviews/T2.3-design.md`, should-fix 3). All three belong to the apply half. The operator has all three, and the first is wider:

1. Leftovers of a deleted instance. The cli leaves Namespaces and CustomResourceDefinitions. The operator also leaves kept claims, and every object when `spec.prune` is false or after an orphan exit. A new instance under another name, namespace or module path is refused on them as `other-instance`. Remedy: annotate each with `opmodel.dev/adopt=<new UUID>`, as the refusal prints, or delete them.
2. No record and a changed identity. For the operator: a ModuleInstance deleted without pruning and created again with another module path. Remedy: annotate each object.
3. An inventoried object stuck terminating. Every apply of a changed render refuses. Remedy outside OPM: release the object, then wait for the retry.

A fourth is the operator's alone, also in the apply half: a tenant ServiceAccount that may patch an object but not read it.

This half adds one refusal of its own: `IdentityChangeUnsettled`, with the remedy in its message.

### Which cli decisions this mirrors, and where the operator differs

Mirrored: prune judges with the identity in the record; the UID-only precondition; `ErrReplaced` as a failed delete; fail closed on a failed read; the stale set is not filtered and the verdict decides per object; the library's message is never reworded; a call-site test that matches by method and receiver, with a test of the matcher (the cli's build review found a name-only matcher too weak, `reports/T3.2.md`); the cut into a delete half and an apply half, delete half first.

Different, with the reason:

- The operator keeps the earlier identity in status until the change is settled. The cli writes the new identity with its record and documents the leftover of a failed prune. The operator reconciles unattended and can be deleted at any moment, so the owner chose to store both.
- The operator reports through events and conditions, not exit codes and lines. `LeftBehind` is the `left behind` line.
- The safety-excluded kinds are skipped inside the prune loop and reported in `LeftBehind`. The cli splits them out before the prune.
- The forced recreate has no cli counterpart: the cli never deletes inside an apply.
- No `Admit`, no migration deletes, no dry run: the operator has none of those paths.
- Propagation stays the API server's default. The cli deletes with foreground propagation; the operator takes that with the deletion protocol.

### Security

- Assets: objects in the cluster that an instance did not create, and their data.
- Trust boundary: the cluster API. Live labels and annotations are input that any principal with patch rights on the object can set. The two identity fields are in the status subresource of the operator's own kinds.
- Threats and mitigations: deleting another owner's object (the verdict on every prune and cleanup delete, on a fresh read); deleting a successor of the object that was read (the UID precondition on every delete); deciding on a read that failed (fails closed); deciding for a tenant with rights it lacks (the read is made as the acting identity); a delete that cannot be judged per object (the guard refuses a delete of a collection). Baseline: the contract of 0012:D8 and the library's verdict tests.
- Residual risk, owner: the operator maintainers.
  - A principal with patch rights on an object can set the adopt annotation to another UUID; the instance then never deletes that object, so it outlives the instance. For the delete side this is what removing the OPM labels does today.
  - A principal who may write `status` of a ModuleInstance or ModulePackage can set `previousInstanceUUID` to another instance's identity; the next cleanup or prune then accepts that instance's objects when they are in this object's inventory. `instanceUUID` has the same property today, and the inventory itself is in the same status, so such a principal can already name any object for deletion. No new right is gained.
  - A relabel between the read and the DELETE is not closed by the UID precondition.
- For the apply half, recorded here so that it is not lost: an adopt annotation that names another UUID takes an object out of OPM management while the instance stays `Ready=True`, and unlike removed labels the next apply does not undo it. Detection there is the `AdoptedElsewhere` event and the count. The apply-half change states this threat and its owner itself.
- Detection: every object left behind for an ownership reason is named in a `Warning` event with the library's message.
- Messages carry object names and instance UUIDs. Neither is a secret.

## Risks / Trade-offs

- [A wrong adopt annotation on an object] -> The object is left behind on prune and deletion and reported. Removing the annotation lets the next cleanup delete it only while it is still in an inventory.
- [An object applied by a failed reconcile is in no inventory] -> Existing behaviour, not changed here: the inventory is replaced only on full success, so an object first created by an apply whose prune failed is not deleted when the instance is deleted before the retry. The deletion protocol change is the place for it.
- [Upgrade, identity change and failed prune together] -> An object with no recorded identity whose first render under this release also changes its identity, and whose prune then fails: the retry has the new identity recorded and no earlier one, so the stale objects are left behind and reported. Three rare events at once; accepted.
- [A handover to the cli inside the window] -> The cli judges with the new identity only; see the handover section.
- [`IdentityChangeUnsettled` blocks a user] -> The message names the remedy; deletion always works.
- [The CRD change releases the operator module] -> The repo rule for every CRD change; the section that adds the fields regenerates `modules/opm_operator/zz_generated_*` in the same commit.
- [The fake client does not enforce delete preconditions] -> The precondition tests run in envtest, against a real API server.
- [This half lands without the apply half] -> Safe: it narrows deletes and adds one refusal. The apply still takes over objects until the apply half lands; both ship in one operator release.

## Migration Plan

No stored data is migrated. The two status fields fill as described under "No recorded identity". Rollback is a revert: old code ignores `previousInstanceUUID` and the annotation, and keeps reading `instanceUUID`; an identity change that was unsettled at the rollback is then judged with the new identity only, as before this change.

Migration note for the PR body and the release:

- The operator never deletes an object whose `opmodel.dev/adopt` annotation names another instance. It leaves the object in place and reports it in a `LeftBehind` event.
- After `spec.module.path` of an instance changes, the operator deletes the objects the instance no longer renders. Before, it left them in the cluster. Until that change is settled, `status.previousInstanceUUID` holds the earlier identity.
- A second change of the module path before the first is settled is refused with `IdentityChangeUnsettled`. Restore the earlier path, wait until the instance is Ready, then change it again.
- A ModulePackage records its instance identity in `status.instanceUUID` and no longer deletes an object that carries another instance's identity.
- A resource of a kind named `Namespace` or `CustomResourceDefinition` outside the core and `apiextensions.k8s.io` groups is pruned like any other resource.

## Open Questions

- Whether the docs page that explains the adopt annotation lives in this repo's `docs/site/operating/` or is shared with the cli's page. Either way this change adds the operator's `LeftBehind` and `IdentityChangeUnsettled` entries to the operating docs.
