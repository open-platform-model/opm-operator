## ADDED Requirements

### Requirement: A ModulePackage deletes and waits as a ModuleInstance does
The deletion cleanup and the prune of stale resources of a ModulePackage MUST follow the same rules as a ModuleInstance's (capabilities `finalizer-and-deletion` and `prune-stale-resources`): the library's deletion plan, the hold verdict, the wait until deleted objects are gone, the reasons `DeletionInProgress` and `DeletionBlocked`, the release when the deleting identity is lost after every delete was sent, and the release of an inventory of kept claims only.

#### Scenario: A terminating object holds the finalizer of a ModulePackage
- **GIVEN** a ModulePackage with `spec.prune=true` being deleted, whose Deployment still exists with a `deletionTimestamp`
- **WHEN** the controller reconciles it
- **THEN** the finalizer is still present and `Ready` is False with reason `DeletionInProgress`

#### Scenario: A ModulePackage is released when its ServiceAccount goes during the wait
- **GIVEN** a ModulePackage being deleted with reason `DeletionInProgress`, whose ServiceAccount is deleted
- **WHEN** the controller reconciles it
- **THEN** the finalizer is removed and one `Warning` event with reason `DeletionUnconfirmed` is emitted

## MODIFIED Requirements

### Requirement: ModulePackage keeps PersistentVolumeClaims by default
The ModulePackage reconciler MUST apply the same protection as the ModuleInstance reconciler: with `spec.prune` true it MUST keep core `PersistentVolumeClaim` resources on the prune of stale resources and on deletion cleanup, unless `spec.dataPolicy` on the ModulePackage is `Delete`. It MUST emit the same `ClaimsKept` event, a kept stale claim MUST leave `status.inventory`, and a kept claim MUST NOT hold the finalizer.

#### Scenario: Stale claim of a ModulePackage kept
- **GIVEN** a ModulePackage with `spec.prune=true` and no `spec.dataPolicy`, whose render drops a PersistentVolumeClaim it applied before
- **WHEN** the controller reconciles
- **THEN** the claim still exists in the cluster and is not in `status.inventory`
- **AND** a `Normal` event with reason `ClaimsKept` is emitted

#### Scenario: Deletion of a ModulePackage keeps its claims
- **GIVEN** a ModulePackage with `spec.prune=true` and no `spec.dataPolicy` that is being deleted, with a PersistentVolumeClaim in its inventory
- **WHEN** the controller reconciles
- **THEN** the claim still exists, every other entry is deleted, and the finalizer is removed once those entries are gone

#### Scenario: A ModulePackage that opts out deletes its claims
- **GIVEN** a ModulePackage with `spec.prune=true` and `spec.dataPolicy=Delete` that is being deleted, with a PersistentVolumeClaim in its inventory
- **WHEN** the controller reconciles
- **THEN** the claim is deleted with the other entries

### Requirement: A ModulePackage records its instance identity
A ModulePackage MUST carry the identity of the instance it renders in the optional status field `status.instanceUUID`, the UUID label value of its rendered objects, and the identity it had before an unsettled change in the optional field `status.previousInstanceUUID`. Both fields MUST be written, kept and cleared as on a ModuleInstance, and a second identity change before the first is settled MUST be refused as on a ModuleInstance. A ModulePackage stored before the fields existed MUST be valid unchanged and MUST gain `status.instanceUUID`, with no change to its spec, in its first reconcile that renders: before the apply when that reconcile applies, and in its status commit when it applies nothing. Source: 0012:D8:R4.

The prune of stale resources and the deletion cleanup of a ModulePackage MUST judge with the recorded identities as a ModuleInstance's do. While no identity is recorded, a ModulePackage MUST delete what it deleted before the field existed, except an object that carries an adopt annotation naming another identity than the render's, and on deletion any object that carries an adopt annotation.

#### Scenario: The identity is recorded
- **GIVEN** a new ModulePackage that renders and applies with success
- **WHEN** the status is committed
- **THEN** `status.instanceUUID` holds the UUID label value of the rendered objects

#### Scenario: An existing package gains the field
- **GIVEN** a ModulePackage with an inventory and no `status.instanceUUID`, reconciled by an earlier operator release
- **WHEN** the upgraded operator reconciles it with unchanged inputs
- **THEN** `status.instanceUUID` is set, `status.previousInstanceUUID` is empty and nothing is applied

#### Scenario: The first render after the upgrade also changes the identity
- **GIVEN** a ModulePackage with an inventory and no `status.instanceUUID`, whose source now renders the instance under another module path, and a stale ConfigMap that is managed by OPM and carries the UUID label of the earlier identity
- **WHEN** the upgraded operator reconciles it
- **THEN** the stale ConfigMap is deleted, as before the field existed

#### Scenario: A package does not delete another instance's object
- **GIVEN** a ModulePackage with `status.instanceUUID` `A` and `spec.prune=true`, whose stale set holds a ConfigMap that is managed by OPM and carries the UUID label `B`
- **WHEN** the controller prunes
- **THEN** the ConfigMap still exists

#### Scenario: Deletion of a package judges with the recorded identity
- **GIVEN** a ModulePackage with `status.instanceUUID` `A` being deleted, whose inventory holds a ConfigMap with the UUID label `A` and a Secret with the UUID label `B`
- **WHEN** the deletion cleanup runs
- **THEN** the ConfigMap is deleted, the Secret still exists and the finalizer is removed once the ConfigMap is gone

#### Scenario: A package that never rendered is deleted as before
- **GIVEN** a suspended ModulePackage with an inventory and no `status.instanceUUID`
- **WHEN** it is deleted with `spec.prune=true`
- **THEN** every inventory object that is managed by OPM and has no adopt annotation is deleted, whatever its UUID label
