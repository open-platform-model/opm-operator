## ADDED Requirements

### Requirement: The ClaimConflict reason
The status package SHALL define the reason constant `ClaimConflict`. A ModuleInstance or ModulePackage reconcile whose apply refused the forced recreate of a PersistentVolumeClaim MUST set `Ready=False` with reason `ClaimConflict`. The message MUST name the claim as `<namespace>/<name>`, MUST say that the claim was kept, and MUST name the ways out, `spec.dataPolicy: Delete` among them. When the check before the apply refused the claim, the message MUST also name the refused field and the API server's message and MUST say that nothing was applied. When the resource manager's delete guard kept the claim (the claim changed after the check), no refusal is at hand and a stage of the apply is already under way: the message MUST NOT name a field and MUST NOT say that nothing was applied. The reconcile MUST NOT set `Stalled`: it MUST retry on the bounded backoff, because the conflict can be resolved on the claim, which changes nothing on the reconciled object. The reconcile MUST NOT prune, MUST NOT record a new inventory and MUST NOT record the rendered digests as applied.

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

#### Scenario: A claim kept by the delete guard
- **GIVEN** an apply whose check passed and whose staged apply then reached for the delete of the PersistentVolumeClaim `media/config`
- **WHEN** the controller reports the reconcile
- **THEN** `Ready` is `False` with reason `ClaimConflict` and a message that contains `media/config` and `spec.dataPolicy`
- **AND** the message names no refused field and does not say that nothing was applied
