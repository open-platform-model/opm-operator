## ADDED Requirements

### Requirement: A ModulePackage keeps claims on a forced recreate
A ModulePackage reconcile MUST pass `spec.dataPolicy` to the apply as it does to the prune. With `spec.rollout.forceConflicts: true` and `spec.dataPolicy` `Keep` or absent, a PersistentVolumeClaim whose update the API server refuses MUST be kept, and the reconcile MUST report `Ready=False` with reason `ClaimConflict` and one `Warning` event with that reason, and retry on its backoff. With `spec.dataPolicy: Delete` the claim MUST be deleted and recreated.

#### Scenario: A refused claim on a ModulePackage
- **GIVEN** a ModulePackage with `spec.rollout.forceConflicts: true` and no `spec.dataPolicy`, whose render changes `storageClassName` of a live PersistentVolumeClaim
- **WHEN** the controller reconciles
- **THEN** the claim has the UID it had
- **AND** `Ready` is `False` with reason `ClaimConflict`, and a `Warning` event with reason `ClaimConflict` names the claim

#### Scenario: Delete recreates the claim of a ModulePackage
- **GIVEN** a ModulePackage with `spec.rollout.forceConflicts: true` and `spec.dataPolicy: Delete`, whose render changes `storageClassName` of a live PersistentVolumeClaim
- **WHEN** the controller reconciles
- **THEN** the claim is deleted and created again, and `Ready` is `True`
