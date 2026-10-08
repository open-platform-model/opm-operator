## ADDED Requirements

### Requirement: The ClaimConflict reason
The status package SHALL define the reason constant `ClaimConflict`. A ModuleInstance or ModulePackage reconcile whose apply refused the forced recreate of a PersistentVolumeClaim MUST set `Ready=False` with reason `ClaimConflict`. The message MUST name the claim as `<namespace>/<name>`, the refused field, and the API server's message, MUST say that the claim was kept and that nothing was applied, and MUST name the ways out, `spec.dataPolicy: Delete` among them. The reconcile MUST NOT set `Stalled`: it MUST retry on the bounded backoff, because the conflict can be resolved on the claim, which changes nothing on the reconciled object. The reconcile MUST NOT prune, MUST NOT record a new inventory and MUST NOT record the rendered digests as applied.

#### Scenario: A refused claim on a ModuleInstance
- **GIVEN** a ModuleInstance with `spec.rollout.forceConflicts: true` and no `spec.dataPolicy`, whose render changes `storageClassName` of a live PersistentVolumeClaim `media/config`
- **WHEN** the controller reconciles
- **THEN** `Ready` is `False` with reason `ClaimConflict` and a message that contains `media/config` and `spec`
- **AND** `Stalled` is not set, and the result asks for a retry after a backoff
- **AND** the claim has the UID it had
- **AND** no other object of the render was changed

#### Scenario: The conflict clears when the render fits the claim again
- **GIVEN** that ModuleInstance with reason `ClaimConflict`
- **WHEN** the render goes back to the claim's `storageClassName` and the controller reconciles
- **THEN** `Ready` is `True`

#### Scenario: Delete recreates the claim
- **GIVEN** a ModuleInstance with `spec.rollout.forceConflicts: true` and `spec.dataPolicy: Delete`, whose render changes `storageClassName` of a live PersistentVolumeClaim
- **WHEN** the controller reconciles
- **THEN** the claim is deleted and created again with the new `storageClassName`, and `Ready` is `True`
