## Context

See `proposal.md` for the motivation. The facts of the code at 45c6a12 that shape the design:

- Every delete of the prune and of the deletion cleanup goes through one function, `apply.Prune` (`internal/apply/prune.go:62`). One other delete exists, outside this change: with `spec.rollout.forceConflicts`, `apply.Apply` sets the `Force` option of the Flux SSA manager, which deletes and recreates an object when the API server refuses the update as an immutable change (found by the review of this change). It has four callers: the stale prune and the deletion cleanup of ModuleInstance (`internal/reconcile/moduleinstance.go:1432` and `:1260`) and of ModulePackage (`internal/reconcile/modulepackage.go:757` and `:938`).
- `apply.Prune` already skips two kinds without an error (`isSafeToDelete`, Namespace and CustomResourceDefinition, ADR-011) and skips a live object whose labels say another owner. A skipped entry is counted in `PruneResult.Skipped` and never fails the prune.
- After a successful apply the recorded inventory is the rendered set, nothing else (`newEntries = withoutEntries(converted.entries, expired)`, `moduleinstance.go:656`; `entries: converted.entries`, `modulepackage.go:773`). A stale entry leaves the inventory whether it was deleted, skipped as a Namespace, or not pruned because `spec.prune` is false. The inventory digest is part of no-op detection (`noOp`, `lastApplied.Inventory`), and the health judgement reads every inventory entry.
- Deletion cleanup removes the finalizer when `apply.Prune` returns no error (`moduleinstance.go:1283`). The ModuleInstance and its `status.inventory` are then gone.
- A CLI-owned instance is never pruned by the operator (`handleCLIOwned`). The CLI and the operator share one record, `status.inventory` on the ModuleInstance.
- `spec.prune` has no default; absent means false, and then the operator deletes nothing.

Reversibility of the field: a costly two-way door while the line is in beta (a rename is a `feat!` and a beta counter), a one-way door after GA. A human accepted it: the owner chose the field at the proposal gate on 2026-10-08.

## Goals / Non-Goals

**Goals:**

- With no new input from the user, the prune and the deletion cleanup never delete a tracked PersistentVolumeClaim.
- One explicit, optional field restores the old behaviour.
- A kept claim never blocks a reconcile or a deletion.
- The user can see which claims were kept.

**Non-Goals:**

- Protecting PersistentVolumes, VolumeSnapshots or any other kind.
- Tracking or deleting claims a StatefulSet creates.
- A restore step, a backup, or a status list of kept claims.
- Any change to the CLI.

## Decisions

### 1. The opt-out field

**Decided by the owner at the proposal gate, 2026-10-08: the enum `spec.dataPolicy` with the values `Keep` and `Delete`, on ModuleInstance and ModulePackage, with no validation rule.** The first version of this design recommended the boolean (shape A below); the owner chose shape B.

The field is optional, adds nothing to existing objects, and changes the meaning of no existing field. Its doc comment is what `kubectl explain moduleinstance.spec.dataPolicy` prints and what the generated resource reference shows:

```go
// DataPolicy says what the operator does with PersistentVolumeClaims.
type DataPolicy string

const (
    // DataPolicyKeep keeps PersistentVolumeClaims. An absent value means the same.
    DataPolicyKeep DataPolicy = "Keep"
    // DataPolicyDelete lets the operator delete PersistentVolumeClaims under spec.prune.
    DataPolicyDelete DataPolicy = "Delete"
)

// DataPolicy says what the operator does with the PersistentVolumeClaims it
// would otherwise delete under spec.prune. With Keep, or when the field is
// absent, the operator does not delete a PersistentVolumeClaim when it
// prunes or when the ModuleInstance is deleted: a claim that a new render
// no longer produces stays in the cluster and is no longer tracked, and
// deleting the ModuleInstance leaves its claims in place. With
// Delete, claims are pruned and deleted like any other object, and the data
// on their volumes goes with them under the reclaim policy of the volume.
// The field has no effect unless spec.prune is true: Delete without
// spec.prune is accepted and deletes nothing. Claims that a StatefulSet
// creates from its volumeClaimTemplates are never tracked and never
// deleted by the operator, whatever this field says.
//
// The field covers pruning and deletion only. With
// spec.rollout.forceConflicts, an apply that the API server refuses as a
// change to an immutable field deletes the object and creates it again, a
// PersistentVolumeClaim included, whatever this field says.
// +kubebuilder:validation:Enum=Keep;Delete
// +optional
DataPolicy DataPolicy `json:"dataPolicy,omitempty"`
```

```text
$ kubectl explain moduleinstance.spec.dataPolicy
FIELD: dataPolicy <string>
ENUM:
    Keep
    Delete

DESCRIPTION:
    DataPolicy says what the operator does with the PersistentVolumeClaims it
    would otherwise delete under spec.prune. ...
```

Why the enum: it can grow a third value without a new field, and it follows the Kubernetes API conventions, which advise against booleans. Its cost: the operator's name differs from the CLI's `--delete-data`, so the docs and the CLI prompt must name both.

The CRD carries no default, as for `spec.owner`: the reconciler gives an absent value the meaning `Keep`. The API server refuses any other value, an explicit empty string included.

No CRD validation ties the field to `spec.prune` (owner: "Allow it, documented no-op"). `dataPolicy: Delete` with prune off is admitted and deletes nothing; the field description says so.

Shapes that were not chosen:

- **A, a boolean `spec.deleteData` beside `spec.prune`.** For: one name on both managers (`--delete-data` on the CLI), and it matches its siblings `suspend` and `prune`. Against: a boolean cannot grow; a later split between prune and deletion, or a third policy, needs a second field. Rejected by the owner in favour of B.
- **C, a struct `spec.persistentVolumeClaimRetentionPolicy` with `whenPruned` and `whenDeleted`, each `Retain` or `Delete`,** after `StatefulSet.spec.persistentVolumeClaimRetentionPolicy`. For: a known name, and prune and deletion are set apart. Against: two switches where the CLI has one, and the most surface for a need nobody has stated (Principle VII). Not chosen.
- Changing `spec.prune` to an enum or a struct: changes an existing field.
- A per-claim annotation such as `opmodel.dev/prune: disabled` in place of a field: the default would not protect.
- A manager flag: cluster-wide, not per instance.

### 2. The guard lives in `apply.Prune`

```go
// PruneOptions tunes one prune run.
type PruneOptions struct {
    // DeleteData allows the deletion of PersistentVolumeClaims. The
    // reconcilers set it from spec.dataPolicy == Delete.
    DeleteData bool
}

type PruneResult struct {
    Deleted int
    Skipped int
    // Kept lists the claims the run did not delete because DeleteData is false.
    Kept []releasesv1alpha1.InventoryEntry
}

func Prune(ctx context.Context, c client.Client, ownerUUID string,
    stale []releasesv1alpha1.InventoryEntry, opts PruneOptions) (*PruneResult, error)
```

A data claim is an entry with `Group == ""` and `Kind == "PersistentVolumeClaim"`, the test the CLI uses. The check runs per entry, after the Namespace and CRD exclusion, in this order:

1. Read the live object, as today.
2. NotFound: the claim is gone. Not kept, not counted, no error (existing rule). This avoids the defect the CLI review found: a claim that no longer exists is not reported as kept.
3. The read fails for another reason: for a data claim under protection, keep it and list it in `Kept`, with no error. Nothing will be deleted, so the failed read must not fail the prune or hold a finalizer. For every other entry, and for a claim when `DeleteData` is true, the error is collected as today.
4. The live object is not OPM-managed, or carries another instance's UUID: skipped as today, counted in `Skipped`, not in `Kept`. The operator does not claim to have kept data it does not own.
5. A data claim, `DeleteData` false: listed in `Kept`. No delete call.
6. Otherwise: delete, as today.

Why here and not in the callers: all four call sites get the default by construction, and a fifth caller cannot forget it. The option has no safe-by-omission problem: the zero value protects.

Alternative considered: filter claims out of the stale set in each reconciler before calling `Prune` (what the CLI does with `SplitDataClaims`). Rejected: four call sites, and the CLI review's nit 1 was exactly that the exported prune had no guard of its own.

### 3. What happens to a kept claim

**Prune (a render drops a claim).**

- The claim: left in the cluster, unchanged. Its OPM labels stay.
- The inventory entry: it leaves `status.inventory` with the rest of the stale set, because the recorded inventory is the rendered set (Context). The claim is untracked from then on.
- Conditions: unchanged. `Ready=True` with reason `ReconciliationSucceeded`; the outcome is `AppliedAndPruned` when something else was deleted and `Applied` otherwise. A kept claim is not a failure and does not count in `failureCounters.prune`.
- What the user sees: one `Normal` event, reason `ClaimsKept`, action `Prune`, and an info log line with the same names.
- With `dataPolicy: Delete`: the stale claim is deleted like any other stale object.

The entry leaves the inventory, where the CLI keeps it. The alternative, keeping the entry as the CLI does, was rejected for the operator:

| | Entry leaves the inventory (chosen) | Entry stays in the inventory |
| --- | --- | --- |
| No-op detection | unchanged | the stored inventory digest never equals the rendered one, so every reconcile applies; needs a second digest or a second list |
| Health | unchanged | the health judgement reads a claim no workload mounts; a pending claim keeps `Healthy=False` |
| A later `dataPolicy: Delete` | does not reach claims kept earlier; `kubectl delete pvc` does | deletes them on the next reconcile |
| Handover | no inventory names the claim, so no manager can delete it | the hazard the cli#345 review named: a record that lists a kept claim makes it deletable by whoever takes over |
| Consistency inside the operator | same as a skipped Namespace or CRD, and as every stale entry under `spec.prune: false` | a new kind of entry |

The cost of the chosen row is that the operator and the CLI differ in one respect: after a kept prune, a CLI-managed instance still lists the claim and an operator-managed one does not. The docs state it.

**Instance deletion (`spec.prune: true`).**

- The claims: every tracked claim is left in the cluster.
- The inventory: it goes with the ModuleInstance. The claims are untracked.
- The finalizer: removed in the same reconcile, as soon as every other entry is deleted. A kept claim is not an error, so it cannot hold the finalizer. A failure on another entry still holds it, as today, and the retry keeps the claims again.
- Conditions: none are written; the object is going away. The stall conditions of the deletion path (`DeletionSAMissing`, `ImpersonationFailed`) are unchanged.
- What the user sees: one `Normal` event, reason `ClaimsKept`, action `Delete`, on the ModuleInstance, emitted before the finalizer is removed, and an info log line. An event outlives its object for the event TTL of the cluster.
- With `dataPolicy: Delete`: claims are deleted with the rest. The operator sends the delete and does not wait: a claim that a pod still mounts stays in `Terminating` under `kubernetes.io/pvc-protection` until the pod is gone. That is the behaviour before this change.

**The event.**

```text
Normal  ClaimsKept  Kept 2 PersistentVolumeClaim(s) and the data on them: media/config, media/cache. They are no longer tracked. Delete one with: kubectl delete pvc <name> -n <namespace>. To let the operator delete claims from now on, set spec.dataPolicy to Delete.
```

At most ten names are listed, and fewer when long names would take the note past the 1024-character limit of `events.k8s.io/v1`; the rest is "and N more". The event carries no enhancement reference.

**Taking a kept claim back.** A kept claim keeps its `app.kubernetes.io/managed-by` and instance UUID labels. If a later render of the same instance produces a claim of the same name, the server-side apply adopts the live object and it is in the inventory again. An integration test proves this before the docs say it.

### 4. Handover between the CLI and the operator

| Case | What happens |
| --- | --- |
| CLI to operator (`spec.owner` changes from `cli` to `operator`) | The CLI's record can list a stale claim that a CLI prune kept. The operator's first reconcile computes the stale set from that record. With `spec.prune` false: nothing is deleted. With `spec.prune` true and no `dataPolicy: Delete`: the claim is kept, reported with `ClaimsKept`, and leaves the inventory. Only `spec.prune: true` with `dataPolicy: Delete` deletes it. This closes the gap of cli#345 review question 3. |
| Operator to CLI (`spec.owner` changes to `cli`) | The operator stops. The inventory lists the rendered claims only. The CLI protects them by default; `--delete-data` on a CLI command decides. `spec.dataPolicy` has no effect on a CLI-owned instance, because the operator does nothing there, and the CLI does not read it. |
| `opm instance delete --delete-data` on an operator-managed instance | Unchanged in the CLI: the flag is ignored with a warning, and the operator decides. After this change the operator decides by `spec.dataPolicy`. To delete the data with the instance, the user sets `spec.dataPolicy: Delete` (and `spec.prune: true`) before the delete. |
| A deleting CLI-owned instance that still carries the operator's finalizer | Unchanged: the operator releases the finalizer and prunes nothing. |

**The cli#345 prompt.** For an operator-managed instance with `spec.prune` set, the CLI at origin/main prints: "spec.prune is set, so the operator deletes its tracked resources, PersistentVolumeClaims and the data on them included" (`cli/internal/cmd/instance/delete.go:487`). After this change that sentence is true only when `spec.dataPolicy` is `Delete`. With the field absent the prompt over-warns: it announces a data loss that does not happen. That is the safe direction, but it is not true, and the CLI's docs page `kept-volume-claims.md` ("it does not keep PersistentVolumeClaims") is wrong in the same way.

No operator-side design keeps that sentence true while the default protects. The fix is a small CLI change: read `spec.dataPolicy` from the record the delete already reads before it prompts, and say "kept" or "deleted" accordingly; correct the docs page. The CLI is outside this change; it is swarm task T9.26 (owner decision 3).

### 5. Upgrade

- The CRD change is additive: one optional field, no default, no stored-version change, no conversion. Existing objects are valid as they are.
- An existing instance with `spec.prune: true` and tracked claims is protected from the first reconcile of the upgraded operator: the field is absent, so claims are kept. Nothing is written to the object on upgrade; the inventory still lists its rendered claims, because they are rendered.
- Rollback to the previous operator: the old binary ignores the field and deletes claims again under `spec.prune`. Claims kept in between are untracked and stay. If the old CRD is applied too, the API server drops the field from objects on their next write.
- A user who wants the old behaviour sets `spec.dataPolicy: Delete`.

Release note (the PR body carries it; the CHANGELOG entry links the PR):

> **Breaking: the operator keeps PersistentVolumeClaims.** With `spec.prune: true`, the operator no longer deletes a PersistentVolumeClaim when a render drops it or when the ModuleInstance or ModulePackage is deleted. The claim and its data stay in the cluster and are no longer tracked. To have the operator delete claims as before, set `spec.dataPolicy: Delete`. Instances without `spec.prune` are not affected. Claims created by a StatefulSet were never deleted by the operator and still are not.

### 6. What is not protected, and what is not touched

- **Claims of a StatefulSet's `volumeClaimTemplates`.** The StatefulSet controller creates them; they are in no render and in no inventory. The operator never tracked them, so it never deleted them, and `dataPolicy: Delete` does not delete them either. Kubernetes keeps them when the StatefulSet is deleted unless the StatefulSet sets `persistentVolumeClaimRetentionPolicy`. The field's doc comment and the docs page say this, so that nobody reads `dataPolicy: Delete` as "removes all data of the instance".
- **A claim under `spec.rollout.forceConflicts`.** The apply, not the prune, deletes and recreates an object whose update the API server refuses as an immutable change. A claim whose `storageClassName` or `accessModes` a new module version changes is deleted this way, with `Keep` too. This change does not alter the apply; the field description, the docs page and ADR-020 name the exception. Making the apply honour `spec.dataPolicy` is a scope change for the owner.
- **A claim of another API group or another kind** (PersistentVolume, VolumeSnapshot, a CRD-backed volume claim): pruned as before. Only `PersistentVolumeClaim` of the core group is kept.
- **A claim the instance does not own** (not OPM-managed, or another instance's UUID): skipped by the existing ownership guard and not reported as kept.
- **`spec.prune` false or absent:** nothing is deleted at all, as before; no `ClaimsKept` event, because nothing was up for deletion.

### 7. ModulePackage

Same path: the ModulePackage reconciler applies directly, records its own `status.inventory`, has its own `spec.prune`, and calls the same `apply.Prune` at both sites. It is **in**: the default protects it because the guard is in `apply.Prune`, and `ModulePackageSpec` gains the same field with the same doc comment so that the opt-out exists there too. Leaving it out would need an explicit bypass in the two ModulePackage call sites to keep a behaviour the owner called a hazard (owner decision 2).

### 8. Reconcile phase impact

| Phase | Impact |
| --- | --- |
| Source, Render | none |
| Apply | none |
| Prune | claims kept unless `spec.dataPolicy` is `Delete`; `ClaimsKept` event; outcome `Applied` when only claims were stale |
| Status | no new field; the inventory is the rendered set as before |
| Deletion | claims kept unless `spec.dataPolicy` is `Delete`; `ClaimsKept` event; finalizer removed |

### 9. Security

- Asset: the data on the volumes of tracked claims.
- Trust boundary: none new. Whoever may update a ModuleInstance can already set `spec.prune` and remove every object the instance renders, claims included; `spec.dataPolicy` gives that principal nothing it lacks today, and the default takes a destructive power away until it is asked for by name.
- Identity: deletes still run as the impersonated ServiceAccount. Keeping a claim needs no permission.
- Threat considered: a user with edit rights on the ModuleInstance sets `dataPolicy: Delete` to destroy data. Same principal, same power as before the change; RBAC on `moduleinstances` is the control, and the Kubernetes audit log records the field change.
- Residual risk: kept claims accumulate and hold storage until an administrator deletes them. Owner: the cluster administrator. The event and the docs page are the signal.
- Baseline: the repo's ADR-011 (safety exclusions from pruning), which this extends.

### 10. Record

A new ADR, `adr/020-data-claims-kept-by-default.md`, records the decision beside ADR-011: context, the three field shapes, the owner's choice, consequences. ADR-011 is not edited; ADR-020 refers to it.

## Research & Decisions

### Where the opt-out is enforced

**Context**: four call sites delete objects.
**Explored**: `internal/apply/prune.go`, both reconcilers; the CLI's `SplitDataClaims` approach and nit 1 of `reviews/T9.14.md`.
**Decision**: in `apply.Prune`, with a zero-value-safe option.
**Rationale**: one guard, no caller can forget it.

### The inventory entry of a kept stale claim

**Context**: the CLI keeps the entry; the operator's inventory is the rendered set.
**Explored**: `noOp`, `nextInventory`, `judgeHealth` and the stale set in `internal/reconcile/moduleinstance.go`.
**Decision**: the entry leaves the inventory.
**Rationale**: no change to no-op detection or health; no record that makes the claim deletable after a handover.

### The cli#345 prompt

**Context**: the brief asks that the prompt text for an operator-managed instance stays true.
**Explored**: `cli/internal/cmd/instance/delete.go:480-490` and `docs/site/diagnostics/kept-volume-claims.md` at cli origin/main.
**Decision**: not solvable in the operator; a CLI follow-up is named.
**Rationale**: the prompt says claims are deleted whenever `spec.prune` is set; a protecting default contradicts that sentence by definition.

## Risks / Trade-offs

- [A user relied on the operator to delete claims] → the `!` title, the release note and the docs page; `dataPolicy: Delete` restores it.
- [Kept claims pile up and cost storage] → the `ClaimsKept` event names them; the docs give the `kubectl` command. No status list (owner decision 5).
- [The event expires and nothing in status says a claim was kept] → accepted; the claim keeps its labels and is still in its namespace; the docs point to the event and to `kubectl get pvc`.
- [The operator and the CLI differ: after a kept prune the CLI still lists the claim, the operator does not] → stated in the docs.
- [The CLI prompt over-warns until the CLI follow-up ships] → safe direction; named as a follow-up.
- [A new caller of `apply.Prune` passes `DeleteData: true` by mistake] → the option has one source, a method on the spec field that is true only for `Delete`; an integration test per call site.
- [`dataPolicy: Delete` read as "removes all data"] → the doc comment and the docs name StatefulSet claims.

## Migration Plan

1. Merge and release as the next beta with the `!` title.
2. Users with `spec.prune: true` who want claims deleted set `spec.dataPolicy: Delete`.
3. Rollback: release the previous behaviour again; the field becomes inert. Claims kept in between stay.

No decommission step: nothing old remains.

## Owner decisions

All five questions of the first version of this design were answered at the proposal gate on 2026-10-08. None is open.

1. **The field.** Owner: "spec.dataPolicy: Delete", the enum with `Keep` and `Delete`. Not the boolean, not the struct.
2. **ModulePackage.** Owner: "Yes, same field on both".
3. **The CLI follow-up.** A separate CLI task (swarm task T9.26). This change does not wait for it; the PR body says what the CLI must say after this change.
4. **Validation.** Owner: "Allow it, documented no-op". No CEL rule.
5. **A status record of kept claims.** None (supervisor ruling, as recommended). The event and the labels on the claim are the record.

