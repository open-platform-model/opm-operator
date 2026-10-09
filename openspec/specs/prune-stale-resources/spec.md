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

### Requirement: Prune asks the library's delete verdict
For every entry it may delete, the prune MUST read the live object with the client that would delete it and MUST ask the library's delete verdict (`opm/k8s/ownership`) with that object and an identity of the instance. When the instance has more than one identity to judge with, it MUST ask with each in turn, and the object counts as the instance's own when the verdict says proceed for one of them. It MUST delete the object only then, and it MUST NOT decide ownership with a label or annotation comparison of its own. Source: 0012:D4:R1, 0012:D8:R8.

The prune MUST act on each answer as follows:

- Proceed: the object is deleted, unless it is a PersistentVolumeClaim that `spec.dataPolicy` keeps. A claim counts as kept only after the verdict said proceed.
- The object does not exist: success, as before.
- For every identity, the object is not managed by OPM, belongs to another instance, or carries an adopt annotation that names another instance: the object is left in the cluster, counted as skipped and named in the prune result with the library's reason and message. It is not an error.
- The read fails with an error other than NotFound: the entry is a failed prune and the remaining entries are still attempted, as before. A PersistentVolumeClaim that cannot be read while `spec.dataPolicy` keeps claims is kept without an error, as before.

An object without a UUID label MUST still be deleted when OPM manages it, and every OPM manager label value MUST be accepted, as before.

#### Scenario: Object not managed by OPM is left
- **GIVEN** a stale entry for ConfigMap `team-a/example` whose live object has no `app.kubernetes.io/managed-by` label
- **WHEN** the controller prunes the stale set
- **THEN** the ConfigMap still exists
- **AND** the prune result names it with the reason `not-opm-managed`

#### Scenario: Object of another instance is left
- **GIVEN** a stale entry for ConfigMap `team-a/example` whose live object is managed by OPM and carries the UUID label of another instance
- **WHEN** the controller prunes the stale set with this instance's identity
- **THEN** the ConfigMap still exists
- **AND** the prune result names it with the reason `owner-mismatch`

#### Scenario: Object being adopted by another instance is left
- **GIVEN** a stale entry for ConfigMap `team-a/example` whose live object carries this instance's UUID label and the annotation `opmodel.dev/adopt` with the UUID of another instance
- **WHEN** the controller prunes the stale set
- **THEN** the ConfigMap still exists
- **AND** the prune result names it with the reason `adopted-elsewhere`

#### Scenario: Own object is deleted
- **GIVEN** a stale entry for ConfigMap `team-a/example` whose live object is managed by OPM and carries this instance's UUID label and no adopt annotation
- **WHEN** the controller prunes the stale set
- **THEN** the ConfigMap is deleted and counted as deleted

#### Scenario: Object without a UUID label is deleted
- **GIVEN** a stale entry for ConfigMap `team-a/legacy` whose live object carries `app.kubernetes.io/managed-by=open-platform-model` and no UUID label
- **WHEN** the controller prunes the stale set
- **THEN** the ConfigMap is deleted

#### Scenario: Object still carrying the cli manager identity is deleted
- **GIVEN** a stale entry for ConfigMap `team-a/example` whose live object carries `app.kubernetes.io/managed-by=opm-cli` and this instance's UUID label
- **WHEN** the controller prunes the stale set
- **THEN** the ConfigMap is deleted

#### Scenario: A failed read fails the entry and not the run
- **GIVEN** a stale set of two ConfigMaps and an API server that refuses the read of the first
- **WHEN** the controller prunes the stale set
- **THEN** the second ConfigMap is deleted
- **AND** the prune returns an error that names the first

### Requirement: Kinds OPM never deletes are the library's
The prune MUST NOT delete an object of a kind the library's ownership package excludes from deletion: a `Namespace` of the core group and a `CustomResourceDefinition` of `apiextensions.k8s.io`. The match MUST be on group and kind. Such an entry MUST be left without a read, counted as skipped and named in the prune result with the reason `safety-excluded`.

#### Scenario: Namespace in the stale set
- **WHEN** a core `Namespace` is in the stale set
- **THEN** it is not deleted and the prune result names it with the reason `safety-excluded`

#### Scenario: CustomResourceDefinition in the stale set
- **WHEN** a `CustomResourceDefinition` of `apiextensions.k8s.io` is in the stale set
- **THEN** it is not deleted and the prune result names it with the reason `safety-excluded`

#### Scenario: The same kind name in another group
- **GIVEN** a stale entry of kind `Namespace` in the group `example.com`, live and owned by the instance
- **WHEN** the controller prunes the stale set
- **THEN** the object is deleted

### Requirement: Deletes carry the UID precondition
Every DELETE the prune sends MUST carry a precondition on the UID of the live object the verdict judged. A DELETE that the API server refuses on that precondition MUST be a failed prune for that entry: it MUST NOT be counted as deleted, the object that now holds the name MUST NOT be deleted in that run, and the remaining entries MUST still be attempted.

#### Scenario: The judged object is deleted
- **GIVEN** a stale ConfigMap owned by the instance
- **WHEN** the controller prunes it
- **THEN** the DELETE request names the UID of the object that was read

#### Scenario: An object replaced since the read survives
- **GIVEN** a stale ConfigMap that is deleted and created again under the same name after the prune read it and before the prune deletes it
- **WHEN** the prune sends its DELETE
- **THEN** the new ConfigMap still exists
- **AND** the prune returns an error for that entry and counts nothing as deleted for it

### Requirement: The prune judges with the recorded identities
The prune of stale resources MUST judge with the identities the status held when the apply of the same reconcile started: `status.instanceUUID`, and `status.previousInstanceUUID` when it is set. A stale object that carries either identity, and that the verdict lets the operator delete under it, MUST be deleted. Source: owner decisions of 2026-10-08 (prune judges with the identity stored in the instance's record, also after the instance identity changed; the status keeps both identities until the prune succeeded).

When `status.instanceUUID` was empty when the reconcile started, the prune MUST judge with the render's identity, so that a stale object that carries another instance's UUID label is left in the cluster, as before the field existed. A ModulePackage, and only a ModulePackage, MUST then ask the verdict once more with no identity: before the field existed its prune compared no UUID label, and it MUST NOT lose a delete it had.

#### Scenario: Stale object after the instance identity changed
- **GIVEN** a ModuleInstance with `status.instanceUUID` `A` and `spec.prune=true`, whose `spec.module.path` changes so that its render carries identity `B` and no longer holds ConfigMap `team-a/old`, which carries the UUID label `A`
- **WHEN** the reconcile applies and prunes
- **THEN** ConfigMap `team-a/old` is deleted
- **AND** after the reconcile `status.instanceUUID` is `B` and `status.previousInstanceUUID` is empty

#### Scenario: A failed prune leaves nothing orphaned on the retry
- **GIVEN** the same change of identity, a second stale ConfigMap `team-a/older` with the UUID label `A`, and a prune whose DELETE of `team-a/old` fails
- **WHEN** the reconcile ends and the next reconcile runs
- **THEN** after the first reconcile `status.instanceUUID` is `B`, `status.previousInstanceUUID` is `A` and `status.inventory` is unchanged
- **AND** the second reconcile deletes `team-a/old`, and `team-a/older` if it still exists
- **AND** after it `status.previousInstanceUUID` is empty

#### Scenario: A stale object that was already relabelled
- **GIVEN** an identity change from `A` to `B` that is not settled, and a later render of identity `B` that drops Deployment `team-a/app`, which the earlier apply relabelled to `B`
- **WHEN** the reconcile applies and prunes
- **THEN** Deployment `team-a/app` is deleted

#### Scenario: Stale object that carries a third identity
- **GIVEN** a stale ConfigMap whose live UUID label is neither of the recorded identities nor empty
- **WHEN** the controller prunes the stale set
- **THEN** the ConfigMap still exists and the prune result names it with the reason `owner-mismatch`

#### Scenario: No recorded identity, ModuleInstance, object of another identity
- **GIVEN** a ModuleInstance with an inventory and an empty `status.instanceUUID`, whose render carries identity `B`, and a stale ConfigMap that is managed by OPM, carries the UUID label `X` and has no adopt annotation
- **WHEN** the reconcile applies and prunes
- **THEN** the ConfigMap still exists and the prune result names it with the reason `owner-mismatch`

#### Scenario: No recorded identity, ModulePackage, object of an unknown earlier identity
- **GIVEN** a ModulePackage with an inventory and an empty `status.instanceUUID`, whose render carries identity `B`, and a stale ConfigMap that is managed by OPM, carries the UUID label `X` and has no adopt annotation
- **WHEN** the reconcile applies and prunes
- **THEN** the ConfigMap is deleted

#### Scenario: No recorded identity, object annotated for this instance
- **GIVEN** the same object, and a stale ConfigMap that is managed by OPM, carries no UUID label and carries the annotation `opmodel.dev/adopt` with the value `B`
- **WHEN** the reconcile applies and prunes
- **THEN** the ConfigMap is deleted

#### Scenario: No recorded identity, object annotated for another instance
- **GIVEN** the same object, and a stale ConfigMap that carries the annotation `opmodel.dev/adopt` with the value `C`
- **WHEN** the reconcile applies and prunes
- **THEN** the ConfigMap still exists and the prune result names it with the reason `adopted-elsewhere`

### Requirement: The prune result names what was left behind
The prune result MUST name every entry the prune left in the cluster because the verdict skipped it, a safety-excluded kind included, with the library's reason and message for it. An entry that was already absent and a kept PersistentVolumeClaim MUST NOT be named there. An entry left behind MUST leave `status.inventory` as a deleted entry does, and MUST NOT make the reconcile fail.

#### Scenario: Mixed prune
- **GIVEN** a stale set with an own ConfigMap, a ConfigMap of another instance, a core Namespace and an entry that no longer exists
- **WHEN** the controller prunes the stale set
- **THEN** the result counts one deleted and two skipped
- **AND** it names the ConfigMap of another instance and the Namespace, each with its reason and message

#### Scenario: Inventory after an object was left behind
- **GIVEN** a ModuleInstance whose prune left ConfigMap `team-a/example` behind
- **WHEN** the reconcile completes
- **THEN** `status.inventory.entries` does not list `team-a/example`
- **AND** the `Ready` condition is True

### Requirement: The prune runs the library's deletion plan
The prune of stale resources MUST delete a stale object only when the library's deletion transition (`opm/k8s/lifecycle`) names that delete as the next action. Source: 0012:D4:R1.

The prune MUST send each delete in the order the plan gives, descending kind weight, and MUST add no ordering of its own. Each delete MUST carry the propagation policy the action names, Foreground, and the action's UID precondition.

With more than one identity to judge with, the prune MUST run one plan per identity, in the order the identities are asked today: the entries a plan skips because the object belongs to, or is being adopted by, another instance form the plan of the next identity.

A PersistentVolumeClaim that `spec.dataPolicy` keeps MUST NOT be an entry of any plan; it is still read and judged, so that it is reported as kept, left behind or gone as before.

The prune MUST NOT wait for a deleted object to disappear. A stale entry whose delete the API server accepted leaves `status.inventory`, as before.

#### Scenario: Stale objects are deleted in descending kind weight
- **GIVEN** a stale set that lists a ConfigMap before a Deployment
- **WHEN** the controller prunes the stale set
- **THEN** the DELETE of the Deployment is sent before the DELETE of the ConfigMap

#### Scenario: Stale deletes use Foreground propagation
- **GIVEN** a stale Deployment owned by the instance
- **WHEN** the controller prunes it
- **THEN** the DELETE request carries the propagation policy `Foreground`

#### Scenario: The reconcile does not wait for a stale object
- **GIVEN** a stale Deployment whose delete is accepted and which still exists with a `deletionTimestamp`
- **WHEN** the reconcile ends
- **THEN** `Ready` is True and `status.inventory.entries` does not list the Deployment

#### Scenario: A kept stale claim is not in the plan
- **GIVEN** a ModuleInstance with no `spec.dataPolicy` and a stale PersistentVolumeClaim of its own
- **WHEN** the controller prunes the stale set
- **THEN** no DELETE is sent for the claim and it is reported as kept
