## Context

See `proposal.md` for the motivation. The facts of the code at 45c6a12 that shape the design:

- Every delete the operator makes goes through one function, `apply.Prune` (`internal/apply/prune.go:62`). It has four callers: the stale prune and the deletion cleanup of ModuleInstance (`internal/reconcile/moduleinstance.go:1432` and `:1260`) and of ModulePackage (`internal/reconcile/modulepackage.go:757` and `:938`).
- `apply.Prune` already skips two kinds without an error (`isSafeToDelete`, Namespace and CustomResourceDefinition, ADR-011) and skips a live object whose labels say another owner. A skipped entry is counted in `PruneResult.Skipped` and never fails the prune.
- After a successful apply the recorded inventory is the rendered set, nothing else (`newEntries = withoutEntries(converted.entries, expired)`, `moduleinstance.go:656`; `entries: converted.entries`, `modulepackage.go:773`). A stale entry leaves the inventory whether it was deleted, skipped as a Namespace, or not pruned because `spec.prune` is false. The inventory digest is part of no-op detection (`noOp`, `lastApplied.Inventory`), and the health judgement reads every inventory entry.
- Deletion cleanup removes the finalizer when `apply.Prune` returns no error (`moduleinstance.go:1283`). The ModuleInstance and its `status.inventory` are then gone.
- A CLI-owned instance is never pruned by the operator (`handleCLIOwned`). The CLI and the operator share one record, `status.inventory` on the ModuleInstance.
- `spec.prune` has no default; absent means false, and then the operator deletes nothing.

Reversibility of the field: a costly two-way door while the line is in beta (a rename is a `feat!` and a beta counter), a one-way door after GA. A human accepts it: the owner approves the field at the proposal gate.

## Goals / Non-Goals

**Goals:**

- With no new input from the user, the operator never deletes a tracked PersistentVolumeClaim.
- One explicit, optional field restores the old behaviour.
- A kept claim never blocks a reconcile or a deletion.
- The user can see which claims were kept.

**Non-Goals:**

- Protecting PersistentVolumes, VolumeSnapshots or any other kind.
- Tracking or deleting claims a StatefulSet creates.
- A restore step, a backup, or a status list of kept claims.
- Any change to the CLI.

## Decisions

### 1. The opt-out field: three shapes

All three are optional, add nothing to existing objects, and change the meaning of no existing field. The text under each is the doc comment, which is what `kubectl explain moduleinstance.spec.<field>` prints and what the generated resource reference shows.

**Option A (recommended): a boolean beside `spec.prune`.**

```go
// DeleteData lets the operator delete PersistentVolumeClaims. When false or
// absent, the operator keeps every PersistentVolumeClaim it would otherwise
// delete under spec.prune: a claim that a new render no longer produces
// stays in the cluster and is no longer tracked, and deleting the
// ModuleInstance leaves its claims in place. When true, claims are pruned
// and deleted like any other object, and the data on their volumes goes
// with them under the reclaim policy of the volume. It has no effect unless
// spec.prune is true. Claims that a StatefulSet creates from its
// volumeClaimTemplates are never tracked and never deleted by the operator.
// +optional
DeleteData bool `json:"deleteData,omitempty"`
```

```text
$ kubectl explain moduleinstance.spec.deleteData
FIELD: deleteData <boolean>

DESCRIPTION:
    DeleteData lets the operator delete PersistentVolumeClaims. When false or
    absent, the operator keeps every PersistentVolumeClaim it would otherwise
    delete under spec.prune: ...
```

For: one name on both managers (`--delete-data` on the CLI, `deleteData` on the operator), so the docs and the CLI prompt name one thing. It matches its siblings `suspend` and `prune`. The dangerous value is the one a user must type. Against: the Kubernetes API conventions advise against booleans because they cannot grow; a later split between prune and deletion, or a third policy, needs a second field.

**Option B: an enum.**

```go
// DataPolicy says what the operator does with PersistentVolumeClaims it
// would delete under spec.prune. Keep, the meaning of an absent value,
// leaves them in the cluster. Delete removes them and the data on them.
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
```

For: it can grow a third value without a new field, and it follows the API conventions. Against: a second name for what the CLI calls `--delete-data`; `dataPolicy: Keep` reads as if the operator manages data, which it does not.

**Option C: a struct after the StatefulSet precedent.**

```go
// PersistentVolumeClaimRetentionPolicy says what the operator does with
// tracked PersistentVolumeClaims. whenPruned applies to a claim that a new
// render no longer produces, whenDeleted to the claims of a deleted
// ModuleInstance. Each is Retain (the meaning of an absent value) or Delete.
// +optional
PersistentVolumeClaimRetentionPolicy *ClaimRetentionPolicy `json:"persistentVolumeClaimRetentionPolicy,omitempty"`
```

```text
$ kubectl explain moduleinstance.spec.persistentVolumeClaimRetentionPolicy
FIELD: persistentVolumeClaimRetentionPolicy <Object>
FIELDS:
  whenDeleted   <string>   enum: Retain, Delete
  whenPruned    <string>   enum: Retain, Delete
```

For: the same name and values as `StatefulSet.spec.persistentVolumeClaimRetentionPolicy`, and prune and deletion are set apart. Against: two switches where the CLI has one, so `--delete-data` maps to neither cleanly; the most surface for a need nobody has stated (Principle VII).

**Recommendation: A.** The decisive point is one name across both managers. The cost, a boolean that cannot grow, is acceptable while the API is `v1alpha1` in beta, and option C can be added later as a new field that takes precedence.

Considered and rejected: changing `spec.prune` to an enum or a struct (changes an existing field); a per-claim annotation such as `opmodel.dev/prune: disabled` in place of a field (the default would not protect, and the brief wants the opt-out on the ModuleInstance); a manager flag (cluster-wide, not per instance).

No CRD validation ties the field to `spec.prune`. `deleteData: true` with prune off is a documented no-op. A CEL rule that refuses the pair would mirror the CLI's refusal of `--delete-data` with `--no-prune`, but it would also refuse the edit "turn prune off" on an object that has `deleteData` set (open question 4).

### 2. The guard lives in `apply.Prune`

```go
// PruneOptions tunes one prune run.
type PruneOptions struct {
    // DeleteData allows the deletion of PersistentVolumeClaims.
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
- With `deleteData: true`: the stale claim is deleted like any other stale object.

The entry leaves the inventory, where the CLI keeps it. The alternative, keeping the entry as the CLI does, was rejected for the operator:

| | Entry leaves the inventory (chosen) | Entry stays in the inventory |
| --- | --- | --- |
| No-op detection | unchanged | the stored inventory digest never equals the rendered one, so every reconcile applies; needs a second digest or a second list |
| Health | unchanged | the health judgement reads a claim no workload mounts; a pending claim keeps `Healthy=False` |
| A later `deleteData: true` | does not reach claims kept earlier; `kubectl delete pvc` does | deletes them on the next reconcile |
| Handover | no inventory names the claim, so no manager can delete it | the hazard the cli#345 review named: a record that lists a kept claim makes it deletable by whoever takes over |
| Consistency inside the operator | same as a skipped Namespace or CRD, and as every stale entry under `spec.prune: false` | a new kind of entry |

The cost of the chosen row is that the operator and the CLI differ in one respect: after a kept prune, a CLI-managed instance still lists the claim and an operator-managed one does not. The docs state it.

**Instance deletion (`spec.prune: true`).**

- The claims: every tracked claim is left in the cluster.
- The inventory: it goes with the ModuleInstance. The claims are untracked.
- The finalizer: removed in the same reconcile, as soon as every other entry is deleted. A kept claim is not an error, so it cannot hold the finalizer. A failure on another entry still holds it, as today, and the retry keeps the claims again.
- Conditions: none are written; the object is going away. The stall conditions of the deletion path (`DeletionSAMissing`, `ImpersonationFailed`) are unchanged.
- What the user sees: one `Normal` event, reason `ClaimsKept`, action `Delete`, on the ModuleInstance, emitted before the finalizer is removed, and an info log line. An event outlives its object for the event TTL of the cluster.
- With `deleteData: true`: claims are deleted with the rest. The operator sends the delete and does not wait: a claim that a pod still mounts stays in `Terminating` under `kubernetes.io/pvc-protection` until the pod is gone. That is the behaviour before this change.

**The event.**

```text
Normal  ClaimsKept  Kept 2 PersistentVolumeClaim(s) and the data on them: media/config, media/cache. They are no longer tracked; delete one with kubectl delete pvc <name> -n <namespace>, or set spec.deleteData to delete claims with the instance.
```

At most ten names are listed, and fewer when long names would take the note past the 1024-character limit of `events.k8s.io/v1`; the rest is "and N more". The event carries no enhancement reference.

**Taking a kept claim back.** A kept claim keeps its `app.kubernetes.io/managed-by` and instance UUID labels. If a later render of the same instance produces a claim of the same name, the server-side apply adopts the live object and it is in the inventory again. An integration test proves this before the docs say it.

### 4. Handover between the CLI and the operator

| Case | What happens |
| --- | --- |
| CLI to operator (`spec.owner` changes from `cli` to `operator`) | The CLI's record can list a stale claim that a CLI prune kept. The operator's first reconcile computes the stale set from that record. With `spec.prune` false: nothing is deleted. With `spec.prune` true and no `deleteData`: the claim is kept, reported with `ClaimsKept`, and leaves the inventory. Only `spec.prune: true` with `deleteData: true` deletes it. This closes the gap of cli#345 review question 3. |
| Operator to CLI (`spec.owner` changes to `cli`) | The operator stops. The inventory lists the rendered claims only. The CLI protects them by default; `--delete-data` on a CLI command decides. `spec.deleteData` has no effect on a CLI-owned instance, because the operator does nothing there, and the CLI does not read it. |
| `opm instance delete --delete-data` on an operator-managed instance | Unchanged in the CLI: the flag is ignored with a warning, and the operator decides. After this change the operator decides by `spec.deleteData`. To delete the data with the instance, the user sets `spec.deleteData: true` (and `spec.prune: true`) before the delete. |
| A deleting CLI-owned instance that still carries the operator's finalizer | Unchanged: the operator releases the finalizer and prunes nothing. |

**The cli#345 prompt.** For an operator-managed instance with `spec.prune` set, the CLI at origin/main prints: "spec.prune is set, so the operator deletes its tracked resources, PersistentVolumeClaims and the data on them included" (`cli/internal/cmd/instance/delete.go:487`). After this change that sentence is true only when `spec.deleteData` is true. With the field absent the prompt over-warns: it announces a data loss that does not happen. That is the safe direction, but it is not true, and the CLI's docs page `kept-volume-claims.md` ("it does not keep PersistentVolumeClaims") is wrong in the same way.

No operator-side design keeps that sentence true while the default protects. The fix is a small CLI change: read `spec.deleteData` from the record the delete already reads before it prompts, and say "kept" or "deleted" accordingly; correct the docs page. The CLI is outside this change (open question 3).

### 5. Upgrade

- The CRD change is additive: one optional field, no default, no stored-version change, no conversion. Existing objects are valid as they are.
- An existing instance with `spec.prune: true` and tracked claims is protected from the first reconcile of the upgraded operator: the field is absent, so claims are kept. Nothing is written to the object on upgrade; the inventory still lists its rendered claims, because they are rendered.
- Rollback to the previous operator: the old binary ignores the field and deletes claims again under `spec.prune`. Claims kept in between are untracked and stay. If the old CRD is applied too, the API server drops the field from objects on their next write.
- A user who wants the old behaviour sets `spec.deleteData: true`.

Release note (the PR body carries it; the CHANGELOG entry links the PR):

> **Breaking: the operator keeps PersistentVolumeClaims.** With `spec.prune: true`, the operator no longer deletes a PersistentVolumeClaim when a render drops it or when the ModuleInstance or ModulePackage is deleted. The claim and its data stay in the cluster and are no longer tracked. To have the operator delete claims as before, set `spec.deleteData: true`. Instances without `spec.prune` are not affected. Claims created by a StatefulSet were never deleted by the operator and still are not.

### 6. What is not protected, and what is not touched

- **Claims of a StatefulSet's `volumeClaimTemplates`.** The StatefulSet controller creates them; they are in no render and in no inventory. The operator never tracked them, so it never deleted them, and `deleteData: true` does not delete them either. Kubernetes keeps them when the StatefulSet is deleted unless the StatefulSet sets `persistentVolumeClaimRetentionPolicy`. The field's doc comment and the docs page say this, so that nobody reads `deleteData: true` as "removes all data of the instance".
- **A claim of another API group or another kind** (PersistentVolume, VolumeSnapshot, a CRD-backed volume claim): pruned as before. Only `PersistentVolumeClaim` of the core group is kept.
- **A claim the instance does not own** (not OPM-managed, or another instance's UUID): skipped by the existing ownership guard and not reported as kept.
- **`spec.prune` false or absent:** nothing is deleted at all, as before; no `ClaimsKept` event, because nothing was up for deletion.

### 7. ModulePackage

Same path: the ModulePackage reconciler applies directly, records its own `status.inventory`, has its own `spec.prune`, and calls the same `apply.Prune` at both sites. It is **in**: the default protects it because the guard is in `apply.Prune`, and `ModulePackageSpec` gains the same field with the same doc comment so that the opt-out exists there too. Leaving it out would need an explicit bypass in the two ModulePackage call sites to keep a behaviour the owner called a hazard (open question 2).

### 8. Reconcile phase impact

| Phase | Impact |
| --- | --- |
| Source, Render | none |
| Apply | none |
| Prune | claims kept unless `spec.deleteData`; `ClaimsKept` event; outcome `Applied` when only claims were stale |
| Status | no new field; the inventory is the rendered set as before |
| Deletion | claims kept unless `spec.deleteData`; `ClaimsKept` event; finalizer removed |

### 9. Security

- Asset: the data on the volumes of tracked claims.
- Trust boundary: none new. Whoever may update a ModuleInstance can already set `spec.prune` and remove every object the instance renders, claims included; `spec.deleteData` gives that principal nothing it lacks today, and the default takes a destructive power away until it is asked for by name.
- Identity: deletes still run as the impersonated ServiceAccount. Keeping a claim needs no permission.
- Threat considered: a user with edit rights on the ModuleInstance sets `deleteData: true` to destroy data. Same principal, same power as before the change; RBAC on `moduleinstances` is the control, and the Kubernetes audit log records the field change.
- Residual risk: kept claims accumulate and hold storage until an administrator deletes them. Owner: the cluster administrator. The event and the docs page are the signal.
- Baseline: the repo's ADR-011 (safety exclusions from pruning), which this extends.

### 10. Record

A new ADR, `adr/020-data-claims-kept-by-default.md`, records the decision beside ADR-011: context, the three field options, the choice, consequences. ADR-011 is not edited; ADR-020 refers to it.

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

- [A user relied on the operator to delete claims] → the `!` title, the release note and the docs page; `deleteData: true` restores it.
- [Kept claims pile up and cost storage] → the `ClaimsKept` event names them; the docs give the `kubectl` command. No status list (open question 5).
- [The event expires and nothing in status says a claim was kept] → accepted; the claim itself carries the instance labels, so `kubectl get pvc -l module-instance.opmodel.dev/name=<name>` finds it. The docs give that command.
- [The operator and the CLI differ: after a kept prune the CLI still lists the claim, the operator does not] → stated in the docs.
- [The CLI prompt over-warns until the CLI follow-up ships] → safe direction; named as a follow-up.
- [A new caller of `apply.Prune` passes `DeleteData: true` by mistake] → the option has one source, the spec field; a unit test per call site.
- [`deleteData: true` read as "removes all data"] → the doc comment and the docs name StatefulSet claims.

## Migration Plan

1. Merge and release as the next beta with the `!` title.
2. Users with `spec.prune: true` who want claims deleted set `spec.deleteData: true`.
3. Rollback: release the previous behaviour again; the field becomes inert. Claims kept in between stay.

No decommission step: nothing old remains.

## Open Questions

These are owner decisions. Questions 1, 2 and 4 change the specs or the tasks and are answered at the proposal gate, before any code; the artifacts are written for the recommended answer.

1. **The field.** A: `spec.deleteData` boolean. B: `spec.dataPolicy` enum `Keep`/`Delete`. C: `spec.persistentVolumeClaimRetentionPolicy` with `whenPruned` and `whenDeleted`. Recommended: A.
2. **ModulePackage.** In, with the same field (recommended); or in with the default only and no opt-out; or out, unprotected.
3. **The CLI follow-up.** A CLI change that reads the new field for the delete prompt and corrects `kept-volume-claims.md`, shipped with the CLI release that pins this operator (recommended); or accept the over-warning prompt until later.
4. **Validation.** No rule, `deleteData` without `prune` is a documented no-op (recommended); or a CEL rule that refuses `deleteData: true` unless `prune` is true.
5. **A status record of kept claims.** None, the event and the labels are the record (recommended); or a `status.keptClaims` list, which would also let a later `deleteData: true` delete claims kept earlier, at the price of a second inventory.
