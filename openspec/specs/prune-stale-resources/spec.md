## Purpose

Defines how the `internal/apply` package deletes resources that a previous inventory recorded and the current render no longer contains, and the live-state ownership guard that keeps it from deleting objects the instance does not own.

## Requirements

### Requirement: Delete stale resources
The `internal/apply` package MUST provide a `Prune` function that deletes resources identified as stale (present in previous inventory, absent from current desired set).

#### Scenario: Stale resource deleted
- **WHEN** a resource is in the stale set and exists in the cluster
- **THEN** the resource is deleted from the cluster

#### Scenario: Stale resource already gone
- **WHEN** a resource is in the stale set but does not exist in the cluster
- **THEN** the prune treats this as success (no error)

#### Scenario: Empty stale set
- **WHEN** the stale set is empty
- **THEN** the prune is a no-op and returns zero deleted

### Requirement: Namespace safety exclusion
The `Prune` function MUST NOT delete resources of kind `Namespace`, regardless of stale set membership.

#### Scenario: Namespace in stale set
- **WHEN** a Namespace resource is in the stale set
- **THEN** the Namespace is skipped and counted in the skipped total

### Requirement: CRD safety exclusion
The `Prune` function MUST NOT delete resources of kind `CustomResourceDefinition`, regardless of stale set membership.

#### Scenario: CRD in stale set
- **WHEN** a CustomResourceDefinition resource is in the stale set
- **THEN** the CRD is skipped and counted in the skipped total

### Requirement: Prune result
The `Prune` function MUST return a `PruneResult` with counts of deleted and skipped resources.

#### Scenario: Mixed prune
- **WHEN** the stale set contains both pruneable and excluded resources
- **THEN** the `PruneResult` correctly reflects deleted and skipped counts

### Requirement: Continue on individual delete failure
The `Prune` function MUST continue deleting remaining stale resources if one delete fails (fail-slow).

#### Scenario: Partial failure
- **WHEN** one stale resource deletion fails but others succeed
- **THEN** all remaining deletions are attempted and the error is returned alongside the partial result

### Requirement: Live-state UUID-based ownership guard
The `Prune` function MUST verify ownership of each candidate resource against the live cluster state before deletion, using the `module-instance.opmodel.dev/uuid` label as the primary identity signal. The guard is defense-in-depth — inventory remains the primary mechanism for deciding what to prune (Constitution Principle III) — but a final live-state check prevents stale-set computation defects from causing destruction and protects against cross-ModuleInstance ownership collisions.

`Prune` MUST accept the reconciling ModuleInstance's instance UUID as a parameter (its signature changes from `Prune(ctx, c, stale)` to `Prune(ctx, c, ownerUUID, stale)`). Callers supply the UUID from the freshly-rendered resources (apply path) or from `ModuleInstanceStatus.InstanceUUID` (deletion path).

For each entry in the stale set that passes safety exclusions (Namespace, CRD), the function MUST:

1. `Get` the live object by GVK, Namespace, Name.
2. If `Get` returns NotFound, treat as success (already-deleted) and continue. (Existing behavior, preserved.)
3. If `Get` returns any other error, append to the error collection and continue with the next entry. (Existing fail-slow behavior, preserved.)
4. If the live object's `app.kubernetes.io/managed-by` label value is not recognized by `labels.IsOPMManagedBy` from the library's `opm/k8s/labels` (i.e., the live object is not OPM-managed), skip the deletion, increment `PruneResult.Skipped`, log a structured warning, and continue.
5. If the live object carries a non-empty `module-instance.opmodel.dev/uuid` label whose value differs from the supplied `ownerUUID`, skip the deletion, increment `PruneResult.Skipped`, log a structured warning, and continue. (An empty live UUID label is tolerated for backward compatibility with resources applied before the UUID label was stamped.)
6. Otherwise, proceed with `Delete`.

#### Scenario: Skip resource missing OPM managed-by label
- **GIVEN** a stale entry for ConfigMap `team-a/example` and a live ConfigMap with no `app.kubernetes.io/managed-by` label (or a value not recognized by `labels.IsOPMManagedBy`)
- **WHEN** the controller runs Prune with any `ownerUUID`
- **THEN** the ConfigMap is NOT deleted
- **AND** `PruneResult.Skipped` is incremented
- **AND** a warning is logged with kind, namespace, name, and reason `not OPM-managed`

#### Scenario: Skip resource whose instance UUID disagrees with reconciling instance
- **GIVEN** a stale entry for ConfigMap `team-a/example` and a live ConfigMap with `app.kubernetes.io/managed-by=opm-controller` and `module-instance.opmodel.dev/uuid=<UUID-A>`
- **WHEN** the controller runs Prune with `ownerUUID=<UUID-B>` (different ModuleInstance)
- **THEN** the ConfigMap is NOT deleted
- **AND** `PruneResult.Skipped` is incremented
- **AND** a warning is logged with kind, namespace, name, expected `ownerUUID`, and observed `module-instance.opmodel.dev/uuid`

#### Scenario: Delete resource whose instance UUID matches reconciling instance
- **GIVEN** a stale entry for ConfigMap `team-a/example` and a live ConfigMap with `app.kubernetes.io/managed-by=opm-controller` and `module-instance.opmodel.dev/uuid=<UUID-A>`
- **WHEN** the controller runs Prune with `ownerUUID=<UUID-A>` (same ModuleInstance)
- **THEN** the ConfigMap is deleted
- **AND** `PruneResult.Deleted` is incremented

#### Scenario: Tolerate legacy resource with empty UUID label
- **GIVEN** a stale entry for ConfigMap `team-a/legacy` and a live ConfigMap with `app.kubernetes.io/managed-by=open-platform-model` (legacy value) and no `module-instance.opmodel.dev/uuid` label (resource was applied before UUID labels were introduced)
- **WHEN** the controller runs Prune with any `ownerUUID`
- **THEN** the ConfigMap is deleted (legacy resources predate the UUID label and are trusted as OPM-owned via the managed-by label)
- **AND** `PruneResult.Deleted` is incremented

#### Scenario: Delete resource still carrying the CLI manager identity

- **GIVEN** a stale entry for ConfigMap `team-a/example` and a live ConfigMap with `app.kubernetes.io/managed-by=opm-cli` and a UUID label matching the reconciling instance (the post-handoff window: applied by the CLI, removed from the module before any relabeling reconcile ran — 0006:D40)
- **WHEN** the controller runs Prune with the matching `ownerUUID`
- **THEN** the ConfigMap is deleted (all OPM manager identities are accepted by `labels.IsOPMManagedBy`)
- **AND** `PruneResult.Deleted` is incremented

### Requirement: Release UUID persisted on ModuleReleaseStatus
The controller MUST persist the rendered ModuleRelease's release UUID on `ModuleReleaseStatus.ReleaseUUID` after the first successful render. The value is read from any rendered resource's `module-release.opmodel.dev/uuid` label (all rendered resources carry the same UUID). The Status field is consumed by the deletion path to supply `ownerUUID` to `apply.Prune`; the apply/prune happy path may read directly from the freshly-rendered resources.

#### Scenario: Status.ReleaseUUID populated after first successful reconcile
- **GIVEN** a freshly-created ModuleRelease that successfully renders and applies
- **WHEN** the deferred status patcher commits Status
- **THEN** `mr.Status.ReleaseUUID` is set to the rendered release UUID (a non-empty string in UUID format)

#### Scenario: Deletion path reads UUID from Status
- **GIVEN** a ModuleRelease being deleted, with `mr.Status.ReleaseUUID` populated by a prior successful reconcile and `mr.Status.Inventory.Entries` non-empty
- **WHEN** the controller runs deletion cleanup (which calls `apply.Prune`)
- **THEN** `apply.Prune` is invoked with `ownerUUID = mr.Status.ReleaseUUID`
- **AND** the live-state UUID guard correctly distinguishes resources owned by this MR from any others sharing GVK+ns+name

#### Scenario: Deletion of never-successfully-reconciled MR is a no-op
- **GIVEN** a ModuleRelease being deleted, with `mr.Status.ReleaseUUID` empty (never successfully reconciled) and `mr.Status.Inventory.Entries` empty
- **WHEN** the controller runs deletion cleanup
- **THEN** `apply.Prune` is called with an empty stale set (nothing to prune)
- **AND** the finalizer is removed

### Requirement: Prune not attempted while stalled on DeletionSAMissing
The deletion-cleanup prune pass MUST NOT execute while the release is stalled with reason `DeletionSAMissing`. In that state, the impersonated client cannot be built, and prune with any fallback identity is explicitly disallowed.

This requirement complements the existing prune-stale-resources contract: prune executes only when (a) `spec.prune=true`, (b) the inventory has entries to remove, and (c) a valid apply/prune client has been obtained. Condition (c) now explicitly excludes the controller's own client as a fallback on the deletion path.

#### Scenario: Stalled release does not prune
- **GIVEN** a ModuleRelease stalled with reason `DeletionSAMissing`
- **WHEN** reconcile loops fire during the stall window
- **THEN** no delete API calls are made against any resource in `status.inventory`
- **AND** the inventory remains unchanged across reconciles until recovery (SA restore or orphan-exit)

### Requirement: Orphan-exit clears inventory in final status
When the orphan-exit path runs, the reconcile that removes the finalizer MUST also clear `status.inventory` so the last-observed state of the release does not claim ownership of resources the controller has abandoned.

#### Scenario: Inventory cleared on orphan-exit
- **GIVEN** a ModuleRelease stalled with `DeletionSAMissing` and `status.inventory.entries` containing 3 items
- **WHEN** the orphan annotation is set and the next reconcile processes the orphan-exit
- **THEN** the status patch applied in that reconcile sets `status.inventory.entries` to an empty slice (or removes the Inventory struct, whichever matches existing nil semantics)
- **AND** the event message's orphaned-count reflects the pre-clear size (3)

### Requirement: PersistentVolumeClaims are kept unless the instance opts out
The prune MUST NOT delete a `PersistentVolumeClaim` of the core API group unless the object being reconciled sets `spec.dataPolicy` to `Delete`. This holds for the prune of stale resources and for the deletion cleanup, which use the same prune. A kept claim MUST be left in the cluster unchanged, MUST NOT be reported as an error, and MUST NOT count as a prune failure.

The prune result MUST name every claim it kept, so that the caller can report them. A claim counts as kept only when it exists in the cluster and passes the ownership guard:

- A claim that is not found in the cluster is treated as already gone, as for every stale resource, and is not reported as kept.
- A claim whose live object is not OPM-managed, or carries the UUID of another instance, is skipped by the ownership guard and is not reported as kept.
- A claim that cannot be read while it is protected is kept and reported, and the failed read is not an error of the prune.

Only the kind `PersistentVolumeClaim` in the core group is kept. A PersistentVolume, a VolumeSnapshot and a claim-like kind of another API group are pruned like any other resource. Namespaces and CustomResourceDefinitions stay excluded, and `spec.dataPolicy` does not change that.

#### Scenario: Stale claim kept by default
- **GIVEN** a ModuleInstance with `spec.prune=true` and no `spec.dataPolicy`, whose stale set holds PersistentVolumeClaim `media/old-cache` and ConfigMap `media/old-config`, both live and owned by the instance
- **WHEN** the controller prunes the stale set
- **THEN** the ConfigMap is deleted
- **AND** the PersistentVolumeClaim still exists in the cluster
- **AND** the prune result names `media/old-cache` as kept and reports no error

#### Scenario: Stale claim deleted when the instance opts out
- **GIVEN** the same stale set on a ModuleInstance with `spec.prune=true` and `spec.dataPolicy=Delete`
- **WHEN** the controller prunes the stale set
- **THEN** the PersistentVolumeClaim and the ConfigMap are both deleted
- **AND** the prune result names no kept claim

#### Scenario: A claim that is gone is not reported as kept
- **GIVEN** a stale entry for PersistentVolumeClaim `media/old-cache` that does not exist in the cluster
- **WHEN** the controller prunes the stale set with no `spec.dataPolicy`
- **THEN** the prune succeeds and names no kept claim

#### Scenario: A claim of another owner is skipped, not kept
- **GIVEN** a stale entry for PersistentVolumeClaim `media/shared` whose live object carries the UUID label of another ModuleInstance
- **WHEN** the controller prunes the stale set with no `spec.dataPolicy`
- **THEN** the claim is not deleted and is counted as skipped
- **AND** the prune result does not name it as kept

#### Scenario: An unreadable claim does not fail the prune
- **GIVEN** a stale entry for PersistentVolumeClaim `media/old-cache` and an API server that refuses the read of that claim
- **WHEN** the controller prunes the stale set with no `spec.dataPolicy`
- **THEN** the prune returns no error for that entry and names the claim as kept

#### Scenario: Other storage kinds are pruned as before
- **GIVEN** a stale entry of a kind other than core `PersistentVolumeClaim`, live and owned by the instance
- **WHEN** the controller prunes the stale set with no `spec.dataPolicy`
- **THEN** the resource is deleted

### Requirement: The opt-out is an optional field that changes no other field
`ModuleInstance` and `ModulePackage` MUST accept an optional field `spec.dataPolicy` with exactly two values, `Keep` and `Delete`. An absent value MUST mean `Keep`. The API server MUST refuse any other value. The CRD MUST NOT set a default, and an object stored before the field existed MUST be valid unchanged.

For pruning and deletion, `spec.dataPolicy` MUST have no effect unless `spec.prune` is true, and the pair `dataPolicy: Delete` without `spec.prune` MUST be admitted, with a field description that says so: with `spec.prune` false or absent the controller prunes nothing, as before. `spec.prune` MUST keep its meaning for every kind other than PersistentVolumeClaim.

`spec.dataPolicy` MUST also govern the forced recreate of `spec.rollout.forceConflicts`, whatever `spec.prune` says: with `Keep` or no value the controller does not delete and recreate a PersistentVolumeClaim, and with `Delete` it does.

The field's description, which `kubectl explain` and the resource reference show, MUST say that claims a StatefulSet creates from its `volumeClaimTemplates` are never tracked and never deleted by the operator. It MUST also say that with `Keep` the forced recreate of `spec.rollout.forceConflicts` leaves a claim in place and the apply reports a conflict, and that with `Delete` the claim is deleted and recreated. It MUST NOT name the forced recreate as an exception to the protection.

#### Scenario: An object without the field is admitted and protected
- **GIVEN** a ModuleInstance manifest with `spec.prune: true` and no `spec.dataPolicy`
- **WHEN** it is applied to the API server
- **THEN** it is admitted, and the stored object has no `spec.dataPolicy`
- **AND** the controller keeps its PersistentVolumeClaims

#### Scenario: A value outside the enum is refused
- **GIVEN** a ModuleInstance manifest with `spec.dataPolicy: Purge`
- **WHEN** it is applied to the API server
- **THEN** the API server refuses it

#### Scenario: An explicit Keep protects
- **GIVEN** a ModuleInstance with `spec.prune: true` and `spec.dataPolicy: Keep`, whose render drops a PersistentVolumeClaim
- **WHEN** the controller reconciles
- **THEN** the claim is kept

#### Scenario: The field without prune deletes nothing
- **GIVEN** a ModuleInstance with `spec.dataPolicy: Delete` and no `spec.prune`, whose render drops a PersistentVolumeClaim
- **WHEN** the controller reconciles
- **THEN** no resource is deleted

#### Scenario: An existing instance is protected after an operator upgrade
- **GIVEN** a ModuleInstance with `spec.prune: true` that an earlier operator release reconciled, with a PersistentVolumeClaim in `status.inventory`
- **WHEN** the upgraded operator reconciles a render that no longer holds the claim
- **THEN** the claim is kept

#### Scenario: The forced recreate follows the field without prune
- **GIVEN** a ModuleInstance with `spec.rollout.forceConflicts: true`, no `spec.prune` and no `spec.dataPolicy`, whose render changes an immutable field of a live PersistentVolumeClaim
- **WHEN** the controller reconciles
- **THEN** the claim is kept with its UID

### Requirement: A kept stale claim leaves the inventory
After a reconcile whose prune kept a stale PersistentVolumeClaim, `status.inventory` MUST hold the rendered set only, as after every successful apply. The kept claim MUST NOT stay in the inventory. The reconcile MUST end `Ready=True`, and the next reconcile with unchanged inputs MUST be a no-op.

A kept claim keeps its OPM labels. When a later render of the same instance produces a claim of the same name, the apply MUST take the live claim back and record it in the inventory.

#### Scenario: Inventory after a kept claim
- **GIVEN** a ModuleInstance with `spec.prune=true` whose previous inventory holds PersistentVolumeClaim `media/old-cache`, and a render that no longer holds it
- **WHEN** the reconcile succeeds
- **THEN** `status.inventory.entries` does not list `media/old-cache`
- **AND** the `Ready` condition is True
- **AND** the next reconcile with unchanged inputs applies nothing

#### Scenario: A later opt-out does not reach a claim kept earlier
- **GIVEN** a claim that an earlier prune kept and that is in no inventory
- **WHEN** `spec.dataPolicy` is set to `Delete` on the instance
- **THEN** the controller does not delete that claim

#### Scenario: A render takes a kept claim back
- **GIVEN** a kept PersistentVolumeClaim `media/cache` that still carries the instance's labels
- **WHEN** a later render of the same instance holds a claim `media/cache`
- **THEN** the apply succeeds on the live claim
- **AND** `status.inventory.entries` lists `media/cache`
