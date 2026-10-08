## ADDED Requirements

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
- **THEN** the ConfigMap is deleted, the Secret still exists and the finalizer is removed

#### Scenario: A package that never rendered is deleted as before
- **GIVEN** a suspended ModulePackage with an inventory and no `status.instanceUUID`
- **WHEN** it is deleted with `spec.prune=true`
- **THEN** every inventory object that is managed by OPM and has no adopt annotation is deleted, whatever its UUID label
