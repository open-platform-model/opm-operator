## Context

See `proposal.md` for the motivation. The state of the operator at the base of this change (`main` at 0f75ccb, library v1.0.0-beta.7):

- `internal/apply/prune.go`: `Prune` skips a `Namespace` and a `CustomResourceDefinition` by kind alone (`isSafeToDelete`), reads each stale object, compares the managed-by label and the UUID label itself, keeps a PersistentVolumeClaim unless `DeleteData`, and deletes with no precondition. A skip is a log line and a count.
- `internal/reconcile/moduleinstance.go`: the render writes `status.instanceUUID` in memory before anything is applied (line 487), and the deferred status commit patches it on every outcome, a failed apply included. The stale prune (`pruneStaleResources`) and the deletion cleanup (`handleDeletion`) both hand `mi.Status.InstanceUUID` to `Prune`, so the stale prune judges with the identity of the new render.
- `internal/reconcile/modulepackage.go`: both prune calls pass an empty identity ("ModulePackage does not persist an instance UUID on Status"), so only the managed-by label guards a package's deletes. `ModulePackageStatus` has no identity field.
- `internal/apply/apply.go`, `claims.go`, `manager.go`: `Apply` runs Flux `ApplyAllStaged` with `ForceOwnership`. No ownership check runs before it. With `Force`, Flux deletes and recreates an object whose update the API server refuses; the resource manager's client (`claimGuard`) intercepts that delete to protect claims (#268).
- `internal/apply/drift.go`: the dry-run of drift detection reads each object as the identity that applies (#265) and writes nothing.
- No other code applies or deletes a cluster object: the only `Delete` and apply call sites outside tests are in `internal/apply`. The Platform and TransformerRegistration controllers write status and finalizers of their own kinds only.

Constraints:

- The library verdicts are pure. The caller reads the live object and hands it in; the library words every refusal and skip (`opm/k8s/ownership` package doc).
- The contract is 0012:D4:R1/R2, 0012:D7:R1 and 0012:D8:R1 to R8, with the "annotation only" rule of 0012:D8:R8.
- What #260 to #269 delivered MUST NOT change: the narrowed impersonation, the periodic reconcile and the restore, the stuck-delete fixes, held claims, drift as the identity that applies, `spec.dataPolicy` on prune, deletion and forced recreate.
- The deletion protocol (order, finalizer holds, propagation policy) is a later change. This one keeps the prune loop and changes only the per-object decision and the precondition.

Reversibility: costly two-way for the code (the operator can change how it calls the verdict at any release). The one-way parts are already decided by the owner and are not decided here: the contract of 0012:D8 and the additive `status.instanceUUID` on ModulePackage.

## Goals / Non-Goals

**Goals:**

- One function, the library's, decides ownership for every object the operator deletes on behalf of an instance. A test keeps it so.
- Every delete after a verdict carries the UID precondition.
- No instance deletes an object whose adopt annotation names another instance.
- ModuleInstance and ModulePackage prune and delete with one recorded identity each, and the cli and the operator judge one object alike.
- One design for both halves, so the apply half needs no new decision.

**Non-Goals:**

- The apply guard itself (the change `guard-every-apply-by-ownership`).
- The deletion protocol, `opm/k8s/lifecycle`, foreground propagation.
- RBAC, and any change to which identity the operator impersonates.
- A command, field or annotation by which the operator sets the adopt annotation (0012:D8:R6 excludes it).
- The `Admit` input of the verdicts. It exists for `opm operator install` only; the operator MUST never set it.

## Decisions

### Every path, both halves

The operator MUST call the verdict named on each path and MUST act on each answer as stated. "Half" says which change builds the row: D is this change, A is `guard-every-apply-by-ownership`. The rows hold for ModuleInstance and ModulePackage alike unless a row says otherwise.

| # | Path | Half | Verdict and identity | Answer | Operator action (condition, reason, event) |
| --- | --- | --- | --- | --- | --- |
| 1 | Apply of a changed render (`apply.Apply`) | A | `CanApply` for every object of the apply list, before the first write. `InstanceUUID` is the render's. `InInventory` is true when `status.inventory` lists the object (group, kind, namespace, name). `Admit` false. | allowed | Apply. |
| | | | | `terminating`, `foreign-object`, `other-instance` | Refuse the whole apply before any write. `Ready=False`, reason `ApplyRefused`, not Stalled; `Warning` event `ApplyRefused`, action `Apply`; the message is the library's line for each refused object (at most ten, then a count). Retry on the bounded backoff. Nothing is pruned, the inventory and the recorded identity stay. |
| | | | | `adopted-elsewhere` | Do not apply the object and leave it out of the inventory. The apply goes on. `Warning` event `AdoptedElsewhere`, action `Apply`, with the library's message, when the object leaves the inventory and on every later apply that skips it. `Ready` is not changed by this alone; the `Ready=True` message names the count. |
| | | | | (no verdict) read error other than NotFound | Fail closed before any write: handled as a failed apply is today. Forbidden with an effective ServiceAccount: `Stalled`, `ImpersonationFailed`. Any other error: `ApplyFailed`, backoff. |
| 2 | Restore of missing objects (#261) | A | The same guard over the restore list. | allowed (the object is missing, so the verdict has no live object to refuse) | Create it. An object that appeared since the dry-run and is refused refuses the restore as row 1. |
| 3 | NoOp reconcile with a render | A | `CanApply` over the apply list, for the let-go answer only. | `adopted-elsewhere` | The object is left out of drift detection and of the restore, and its entry leaves `status.inventory` in the NoOp commit, as an expired Job's does. The inventory digest stays that of the rendered set, so the next reconcile is still a NoOp. `Warning` event `AdoptedElsewhere` when the entry leaves. |
| | | | | any other refusal | Nothing: nothing is applied. `Ready` is not changed. |
| 4 | Drift dry-run, `checkClaims` dry-run | none | none | | No write. Unchanged, except row 3's exclusion. |
| 5 | Forced recreate inside the apply (`forceConflicts`, #268) | D | `CanDelete` on the live object, with the judging identity of the apply path (below). Asked before the first write for every object of the list that exists; and again at the delete itself. | proceed | Flux may delete and recreate the object; the delete carries the verdict's UID precondition. |
| | | | | any skip, and the dry-run says the update would be refused | Refuse the whole apply before any write. `Ready=False`, reason `RecreateRefused`, not Stalled; `Warning` event `RecreateRefused`, action `Apply`, naming the object, the refused fields and the library's message. Retry on the backoff. |
| | | | | a claim without `spec.dataPolicy: Delete` | Unchanged (#268): `ClaimConflict`. The claim rule is checked first. |
| | | | | (no verdict) read error other than NotFound | A failed apply, as today for the claim check's read. |
| 6 | Prune of stale objects | D | `CanDelete` with the judging identity of the apply path. | proceed | DELETE with `verdict.Preconditions()`; counted as deleted. A claim without `spec.dataPolicy: Delete` is kept instead (#267), after the verdict. |
| | | | | `already-absent` | Done, as today. |
| | | | | `safety-excluded`, `not-opm-managed`, `owner-mismatch`, `adopted-elsewhere` | Left in the cluster; the entry leaves the inventory with the apply's commit, as today. One `Warning` event `LeftBehind`, action `Prune`, for the run. The reconcile still ends `Ready=True`. |
| | | | | (no verdict) read error other than NotFound; DELETE refused on the precondition; any other DELETE error | A failed prune, as today: `Ready=False` `PruneFailed` with backoff, or `Stalled` `ImpersonationFailed` on Forbidden with an effective ServiceAccount. The inventory is not replaced. An unreadable claim under `Keep` stays kept without an error (#267). |
| 7 | Deletion cleanup (`spec.prune: true`) | D | `CanDelete` for every inventory entry, with the recorded identity. | proceed | DELETE with the precondition. |
| | | | | any skip | Left in the cluster. It does not hold the finalizer. One `Warning` event `LeftBehind`, action `Delete`, before the finalizer goes. |
| | | | | (no verdict) or a failed DELETE | The finalizer is held and the cleanup is retried, as today. |
| 8 | Deletion with `spec.prune: false`; orphan exit (#263) | none | none | | Nothing is deleted. Unchanged. |
| 9 | `spec.owner: cli`; the operator's own instance | none | none | | No apply, prune or delete. Unchanged. |

A ModulePackage has no restore and no drift detection today, so rows 2, 3 and 4 do not exist for it.

### Shape in the code (this change)

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

// Prune keeps its signature. identity is the judging identity; the caller
// chooses it (see "Which identity judges").
func Prune(ctx context.Context, c client.Client, identity string,
    stale []releasesv1alpha1.InventoryEntry, opts PruneOptions) (*PruneResult, error)
```

Order inside `Prune`, per entry: `ownership.SafetyExcluded` (no read) -> read -> `ownership.CanDelete` -> the claim rule of #267 -> DELETE with `verdict.Preconditions()`. `isSafeToDelete` and the two label comparisons go.

The forced recreate: `ApplyOptions` gains the judging identity. With `Force`, `Apply` reads every object of the list that exists, asks `CanDelete`, and for each object the verdict skips it asks the API server, with the dry-run `checkClaims` already makes, whether the update would be refused. If one would, `Apply` returns a `*RecreateRefusedError` and applies nothing. The resource manager's client (today `claimGuard`) also judges at the delete itself: it reads the object, asks `CanDelete`, refuses on a skip, and adds the UID precondition on proceed. The claim rule runs first in both places and is not changed.

Reconcile phase impact: Source and Render: none. Apply: the forced recreate only. Prune: the verdict, the identity, the precondition, the event. Status: `status.instanceUUID` is written in the success commit and backfilled on a NoOp; ModulePackage gains the field.

### Research & Decisions

#### Which identity judges

**Context**: `CanDelete` skips as `owner-mismatch` when the live UUID label and the given identity are both set and differ. An instance's identity is a UUID v5 of the module path without its major, the instance name and the namespace (core `src/module_instance.cue`). A ModuleInstance's name and namespace are its object's and cannot change, so within one ModuleInstance the identity changes only when `spec.module.path` changes. A ModulePackage renders the instance its package holds, so its identity changes when that instance's name, namespace or module path changes in the source.
**Explored**: `moduleinstance.go:487` and the deferred commit; the cli's archived design (`cli/openspec/changes/archive/2026-10-08-adopt-kubernetes-ownership-package/design.md`, "Which identity prune judges with").
**Options considered**:
1. Status quo: the render's identity for the stale prune. After a path change every stale object carries the earlier identity, is skipped as `owner-mismatch` and dropped from the inventory: abandoned.
2. The recorded identity: `status.instanceUUID` as the reconcile read it at its start.
3. Either identity.
**Decision**: Option 2 (owner decision of 2026-10-08, "prune a": prune judges stale objects with the identity stored in the instance's record, also after the instance identity changed). The operator's record is the object's own status: `status.instanceUUID` beside `status.inventory`. The judging identity on the apply path is the recorded identity, and the render's when none is recorded (an object reconciled by a release older than the field, whose inventory was applied under the render's identity). On the deletion path it is the recorded identity; when none is recorded the comparison is off, as the library defines and as today.
**Rationale**: The recorded identity is the one the stale objects carry, and it is the one the deletion cleanup uses, so prune and deletion judge one object alike, and so does the cli when ownership moves to it.

#### What happens on an identity change

**Decision**: The recorded identity MUST move only in the status commit of a reconcile that applied and pruned with success, together with the inventory. A failed apply or a failed prune leaves it. A NoOp writes it only when none is recorded.

| Event | What the operator does |
| --- | --- |
| `spec.module.path` of a ModuleInstance changes (same object) | The apply relabels the objects it still renders. Stale objects are judged with the earlier, recorded identity and deleted. The success commit records the new identity. If the apply or the prune fails, the recorded identity and the inventory stay, so the retry judges with the earlier identity again. |
| The instance name or namespace changes | That is another ModuleInstance object with its own, empty record. The earlier object's deletion cleanup judges with its own recorded identity. What that cleanup leaves by rule (Namespaces, CustomResourceDefinitions, kept claims, everything when `spec.prune` is false) carries the earlier identity; the apply half then refuses it for the new object as `other-instance` until it is annotated. |
| The instance inside a ModulePackage changes name, namespace or module path | As the first row: one record, the earlier identity judges the stale set, the success commit records the new one. |
| A stale object carries a UUID label that is not the recorded identity | It is another instance's: left behind, reported. |

**Rationale**: Today the new identity is patched even when the apply failed (`moduleinstance.go:487` with the deferred commit), so the record can hold the new identity beside the earlier inventory. The retry then skips every stale object, and a hand-over to the cli in that window makes the cli judge with the wrong identity too. This is better than what the cli could accept in its own change (a failed prune after a path change leaves its entries behind): the operator replaces the inventory only on success.

#### ModulePackage keeps a persisted identity

**Context**: 0012:D8:R4. Today a ModulePackage persists no identity anywhere: its rendered objects carry the UUID label (the package renders a `ModuleInstance`), but both prune calls pass an empty identity (`modulepackage.go:762`, `:945`).
**Options considered**:
1. Status quo: the managed-by label alone.
2. Read the identity from the live objects of the inventory at delete time. No field, but the guard would trust the labels it is meant to check.
3. An additive `status.instanceUUID` on `ModulePackageStatus`, written as a ModuleInstance's.
**Decision**: Option 3 (owner decision of 2026-10-03, recorded in 0012:D8: "ModulePackage gets a persisted UUID in an additive status field so the UUID guard applies"). The field has the name and the meaning of `ModuleInstanceStatus.InstanceUUID`. A package created before the field gains it on its first reconcile that renders, with no change to its spec; the first reconcile after an operator upgrade always renders, because the render input key holds the operator version.
**Rationale**: It is the decided contract, and one field name on both kinds lets one helper serve both.

#### Delete precondition

**Options considered**: (1) status quo, none; (2) the UID, from `verdict.Preconditions()`; (3) the UID and the resourceVersion.
**Decision**: Option 2, on the prune, the deletion cleanup and the forced recreate.
**Rationale**: The UID closes the case that matters, an object deleted and recreated under the same name since the read. A resourceVersion precondition fails on any status write between the read and the DELETE, as the library's doc states. A DELETE the API server refuses on the precondition is `ErrReplaced`: a failed delete on every path, never a success; the next reconcile reads the new object and judges it.

#### The forced recreate is a delete

**Context**: With `spec.rollout.forceConflicts`, Flux deletes an object whose update the API server refuses and creates it again. That delete is judged by nothing today, except for claims (#268).
**Options considered**:
1. Status quo.
2. Judge at the delete only (the resource manager's client). Flux deletes the refused objects of a stage before it applies any, so a refusal there leaves other objects of the stage deleted until the retry.
3. Judge before the first write and again at the delete, as #268 does for claims.
**Decision**: Option 3. The identity is the judging identity of the apply path.
**Rationale**: A refusal that comes before any write changes nothing in the cluster. The pre-check costs one read per live object, only when `forceConflicts` is set, and a dry-run only for the objects the verdict skips. The cases it refuses are narrow: an object whose OPM labels were removed, an object of another identity, and an object annotated for another instance. Each can still be updated in place; only its delete is refused.

#### Reporting what a prune left behind

**Options considered**: (1) status quo, a log line and a count; (2) one event per object; (3) one event per run, shaped as `ClaimsKept`.
**Decision**: Option 3: reason `LeftBehind`, type `Warning`, action `Prune` or `Delete`; the count, then the library's message for each object (at most ten, fewer past 1024 characters, then the number of the rest). `already-absent` is not reported. No condition changes.
**Rationale**: The library's message is the same text the cli prints, which is the point of 0012:D4. A skipped object leaves the inventory in the same reconcile, so the event is emitted once per object and not on every reconcile.

#### The apply half: decisions recorded here

These bind `guard-every-apply-by-ownership`. They mirror the cli's `guard-every-apply-by-ownership` design unless stated.

- Where the guard runs: one pass over the apply list before the first write, so a refused apply has written nothing (the cli's option 2). It runs on every reconcile that renders, because drift detection and the restore must leave a let-go object out.
- A refusal is `Ready=False` with reason `ApplyRefused`, not Stalled, and retries on the bounded backoff. Reason: the remedy is an annotation on, or the removal of, another object, which no watch of the instance reports. This is the classification `ClaimConflict` and `DependentsRemain` already have.
- A let-go object stays out of the apply, out of drift detection and out of the inventory, and the stale set is computed from the full render, so no delete of it is attempted (the cli's option 2 for 0012:D7:R1). The delete verdict of this change is the second lock.
- The operator has no dry run and no install command, so the cli's dry-run and install decisions have no counterpart.

### The handover between cli and operator ownership

- `spec.owner: cli`: the operator applies, prunes and deletes nothing, so it asks no verdict. The release of a leftover finalizer without pruning (#263) is not changed.
- cli to operator: the first operator reconcile reads the inventory and the identity the cli recorded in the same status fields. Both sides compute the identity with the same formula, and the verdict accepts every OPM manager label value, so the prune judges the cli's objects as its own.
- operator to cli: the cli's prune judges with the record's `status.instanceUUID`. Because the operator now moves that field only with a successful apply, the cli never reads a new identity beside an earlier inventory.
- The operator's own instance is never reconciled, so it is not affected.

### The PVC rule

`spec.dataPolicy` keeps its meaning and its order. On prune and deletion a claim counts as kept only when it exists and the delete verdict lets the operator delete it; a claim the verdict skips is left behind, not kept, as today. An unreadable claim under `Keep` stays kept without an error. On the forced recreate the claim rule runs before the ownership verdict. A kept claim still leaves the inventory.

One consequence for the apply half: a kept claim keeps the labels of the identity that applied it. After an identity change the apply half refuses it as `other-instance`; with the same identity it is taken back, as the spec scenario "A render takes a kept claim back" requires.

### ServiceAccount impersonation

Every read that feeds a verdict MUST be made by the client that would delete or apply the object: the impersonated ServiceAccount when one is effective, the operator's own identity otherwise. This is what the prune's read does today and what #265 set for drift.

**Options considered**: (1) the acting identity; (2) the operator's own identity, which can read more.
**Decision**: Option 1.
**Rationale**: The operator acts for a tenant. A verdict from a read the tenant's ServiceAccount could not make would let the tenant learn, through a refusal message, the owner of an object it may not read, and would decide for the tenant on facts it has no right to. A refused read fails closed with the outcome a refused delete has today.

### What changes for a user

| What | Before | After | Breaking |
| --- | --- | --- | --- |
| Stale or deleted instance's object whose adopt annotation names another instance | deleted when the labels match | left behind, `LeftBehind` event | yes |
| Kind `Namespace` or `CustomResourceDefinition` in another API group | never deleted | deleted as any object | yes |
| Stale objects after `spec.module.path` changed | skipped and abandoned (the render's identity judged) | deleted (the recorded identity judges) | yes |
| `status.instanceUUID` of a ModuleInstance after a failed apply | the render's identity | unchanged until an apply succeeds | no |
| `status.instanceUUID` of a ModulePackage | absent | present after the first render | no (additive) |
| ModulePackage stale object carrying another instance's UUID | deleted (managed-by label only) | left behind | yes |
| Forced recreate of an object the delete verdict skips | deleted and recreated | apply refused, `Ready=False` `RecreateRefused` | yes |
| DELETE of an object replaced since the read | the new object is deleted | `PruneFailed`, retried | no |
| Skipped object on prune or deletion | log line, `Skipped` count | the same, and one `Warning` event `LeftBehind` | no |
| New reasons | | `LeftBehind` (event), `RecreateRefused` (event and `Ready` reason) | no |
| Conditions, metrics, `spec` | | no type added, no metric changed, no spec field | no |

The apply half adds: `Ready=False` `ApplyRefused` and its event, the `AdoptedElsewhere` event, the inventory without let-go objects, and a refused apply where a ServiceAccount may patch an object but not read it. Each is breaking for a user whose apply takes over an existing object today.

### Refusals that hit a user's own objects

The cli's design review found three cases with a manual remedy only. All three belong to the apply half; this change causes none of them. The operator has all three, and the first is wider:

1. Leftovers of a deleted instance. The cli leaves Namespaces and CustomResourceDefinitions. The operator also leaves kept claims, and every object when `spec.prune` is false or after an orphan exit. A new instance under another name, namespace or module path is refused on them as `other-instance`. Remedy: annotate each with `opmodel.dev/adopt=<new UUID>`, as the refusal prints, or delete them. The same name, namespace and path give the same identity and no refusal.
2. No record and a changed identity. For the operator: a ModuleInstance deleted without pruning and created again with another module path. Remedy: annotate each object.
3. An inventoried object stuck terminating. Every apply of a changed render refuses; NoOp reconciles are not affected. Remedy outside OPM: release the object, then wait for the retry.

One case is the operator's alone: a tenant ServiceAccount that may patch an object but not read it. The guard's read fails closed. Remedy: grant `get`.

### Which cli decisions this mirrors, and where the operator differs

Mirrored: the recorded identity judges the prune; the UID-only precondition; `ErrReplaced` as a failed delete; fail closed on a failed read; the stale set is not filtered and the verdict decides per object; the library's message is never reworded; a call-site test; the cut into a delete half and an apply half, delete half first.

Different, with the reason:

- The operator reports through events and conditions, not exit codes and lines. `LeftBehind` is the `left behind` line.
- A failed prune does not move the recorded identity or the inventory. The cli writes its record after a failed prune and keeps the failed entries; the operator's inventory is replaced only on success, so it needs no such rule.
- The safety-excluded kinds are skipped inside the prune loop and reported in `LeftBehind`. The cli splits them out before the prune.
- The forced recreate has no cli counterpart: the cli never deletes inside an apply.
- No `Admit`, no migration deletes, no dry run: the operator has none of those paths.
- Propagation stays the API server's default. The cli deletes with foreground propagation; the operator takes that with the deletion protocol.

### Security

- Assets: objects in the cluster that an instance did not create, and their data.
- Trust boundary: the cluster API. Live labels and annotations are input that any principal with patch rights on the object can set.
- Threats and mitigations: deleting another owner's object (the verdict on every delete, on a fresh read); deleting a successor of the judged object (the UID precondition); deciding on a read that failed (fails closed); deciding for a tenant with rights it lacks (the read is made as the acting identity). Baseline: the contract of 0012:D8 and the library's verdict tests.
- Residual risk, owner: the operator maintainers. A principal with patch rights on an object can set the adopt annotation to another UUID; the instance then never deletes that object, so it outlives the instance. Patch rights do not include delete rights, so the principal gains nothing it could not do by removing the OPM labels today. A relabel between the read and the DELETE is not closed by the UID precondition.
- Detection: every object left behind is named in a `LeftBehind` event with the instance its annotation names.
- Messages carry object names and instance UUIDs. Neither is a secret.

## Risks / Trade-offs

- [A principal sets a wrong adopt annotation on an object] -> The object is left behind on prune and deletion and reported. Removing the annotation lets the next cleanup delete it only while it is still in an inventory; after that it is a leftover to delete by hand.
- [An object reconciled by a release older than `status.instanceUUID`, after a path change] -> No recorded identity exists, so the render's identity judges and the stale objects are left behind, as today. Reported in `LeftBehind`.
- [The CRD change releases the operator module] -> The repo rule for every CRD change; the section that adds the field regenerates `modules/opm_operator/zz_generated_*` in the same commit.
- [One more read per live object on a forced apply] -> Only with `forceConflicts`.
- [The fake client does not enforce delete preconditions] -> The precondition tests run in envtest, against a real API server.
- [This half lands without the apply half] -> Safe: it only narrows deletes. The apply still takes over objects until the apply half lands; both ship in one operator release.

## Migration Plan

No stored data is migrated. `status.instanceUUID` on a ModulePackage fills on its first render after the upgrade. Rollback is a revert: old code ignores the field and the annotation.

Migration note for the PR body and the release:

- The operator never deletes an object whose `opmodel.dev/adopt` annotation names another instance. It leaves the object in place and reports it in a `LeftBehind` event.
- After `spec.module.path` of an instance changes, the operator deletes the objects the instance no longer renders. Before, it left them in the cluster.
- A ModulePackage records its instance identity in `status.instanceUUID` and no longer deletes an object that carries another instance's identity.
- With `spec.rollout.forceConflicts`, the operator refuses to delete and recreate an object it does not own; the apply reports `RecreateRefused`.
- A resource of a kind named `Namespace` or `CustomResourceDefinition` outside the core and `apiextensions.k8s.io` groups is pruned like any other resource.

## Open Questions

- Whether the docs page that explains the adopt annotation lives in this repo's `docs/site/operating/` or is shared with the cli's page. Either way this change adds the operator's `LeftBehind` and `RecreateRefused` entries to the operating docs.
