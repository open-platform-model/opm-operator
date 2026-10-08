## MODIFIED Requirements

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
