## ADDED Requirements

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

`spec.dataPolicy` MUST have no effect unless `spec.prune` is true, and the pair `dataPolicy: Delete` without `spec.prune` MUST be admitted, with a field description that says it has no effect: with `spec.prune` false or absent the controller deletes nothing, as before. `spec.prune` MUST keep its meaning for every kind other than PersistentVolumeClaim.

The field's description, which `kubectl explain` and the resource reference show, MUST say that claims a StatefulSet creates from its `volumeClaimTemplates` are never tracked and never deleted by the operator.

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
