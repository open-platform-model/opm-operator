## ADDED Requirements

### Requirement: A forced recreate deletes only what the delete verdict allows
With `spec.rollout.forceConflicts: true`, the apply MUST NOT delete and recreate an object unless the library's delete verdict says proceed for the live object, judged with the identity the prune of the same reconcile uses. The delete MUST carry a precondition on the UID of the object that was judged. The verdict's read MUST be made by the client that applies. Source: 0012:D4:R1.

When the verdict skips a live object and the API server would refuse its update, the apply MUST apply nothing, and the reconcile MUST report `Ready=False` with reason `RecreateRefused`, not Stalled, and retry on its backoff. The message MUST name the object, the fields whose update was refused, and the library's message for the skip. An object the verdict skips whose update the API server accepts MUST be applied as any other object.

The rule for PersistentVolumeClaims MUST be checked first and is not changed: a claim that `spec.dataPolicy` keeps is reported as `ClaimConflict`.

#### Scenario: An own object with a changed immutable field is recreated
- **GIVEN** a ModuleInstance with `spec.rollout.forceConflicts: true` whose render changes an immutable field of a live Job that carries the instance's labels
- **WHEN** the controller reconciles
- **THEN** the Job is deleted and created again
- **AND** the DELETE request names the UID of the Job that was read

#### Scenario: An object adopted by another instance is not recreated
- **GIVEN** the same instance, and a live Job whose annotation `opmodel.dev/adopt` names another instance and whose update the API server would refuse
- **WHEN** the controller reconciles
- **THEN** the Job keeps its UID and no object of the render is applied
- **AND** the `Ready` condition is False with reason `RecreateRefused`

#### Scenario: An object without OPM labels is not recreated
- **GIVEN** the same instance, and a live Job without an `app.kubernetes.io/managed-by` label whose update the API server would refuse
- **WHEN** the controller reconciles
- **THEN** the Job keeps its UID and the `Ready` condition is False with reason `RecreateRefused`

#### Scenario: A skipped object that needs no recreate is applied
- **GIVEN** the same instance, and a live ConfigMap without OPM labels whose update the API server accepts
- **WHEN** the controller reconciles
- **THEN** the ConfigMap is updated and the apply succeeds

#### Scenario: A claim is still reported as a claim conflict
- **GIVEN** the same instance without `spec.dataPolicy`, and a live PersistentVolumeClaim whose update the API server would refuse
- **WHEN** the controller reconciles
- **THEN** the `Ready` condition is False with reason `ClaimConflict`
