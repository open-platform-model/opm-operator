## ADDED Requirements

### Requirement: A ModulePackage records its instance identity
A ModulePackage MUST carry the identity of the instance it renders in the optional status field `status.instanceUUID`, the UUID label value of its rendered objects. The field MUST be written as on a ModuleInstance: with the status commit of a reconcile whose apply and prune succeeded, and by a reconcile that applies nothing when the field is empty. A ModulePackage stored before the field existed MUST be valid unchanged and MUST gain the field with no change to its spec. Source: 0012:D8:R4.

The prune of stale resources and the deletion cleanup of a ModulePackage MUST hand the delete verdict an identity as a ModuleInstance's do: the recorded identity, and on the apply path the render's identity when none is recorded.

#### Scenario: The identity is recorded
- **GIVEN** a new ModulePackage that renders and applies with success
- **WHEN** the status is committed
- **THEN** `status.instanceUUID` holds the UUID label value of the rendered objects

#### Scenario: An existing package gains the field
- **GIVEN** a ModulePackage with an inventory and no `status.instanceUUID`, reconciled by an earlier operator release
- **WHEN** the upgraded operator reconciles it with unchanged inputs
- **THEN** `status.instanceUUID` is set and nothing is applied

#### Scenario: A package does not delete another instance's object
- **GIVEN** a ModulePackage with `status.instanceUUID` `A` and `spec.prune=true`, whose stale set holds a ConfigMap that is managed by OPM and carries the UUID label `B`
- **WHEN** the controller prunes
- **THEN** the ConfigMap still exists

#### Scenario: Deletion of a package judges with the recorded identity
- **GIVEN** a ModulePackage with `status.instanceUUID` `A` being deleted, whose inventory holds a ConfigMap with the UUID label `A` and a Secret with the UUID label `B`
- **WHEN** the deletion cleanup runs
- **THEN** the ConfigMap is deleted, the Secret still exists and the finalizer is removed
