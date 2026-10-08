## ADDED Requirements

### Requirement: Events emitted when a claim recreate is refused
The controller MUST emit one `Warning` event with reason `ClaimConflict` and action `Apply` when an apply refused the forced recreate of a PersistentVolumeClaim. The event MUST name the claim as `<namespace>/<name>` and the refused field, and carry the same text as the `Ready` condition. The controller MUST NOT also emit an `ApplyFailed` event for that apply. The event text MUST NOT carry an enhancement reference.

#### Scenario: Refused claim recreate
- **GIVEN** a ModuleInstance with `spec.rollout.forceConflicts: true` and no `spec.dataPolicy`, whose render changes an immutable field of the live PersistentVolumeClaim `media/config`
- **WHEN** the reconcile completes
- **THEN** a `Warning` event with reason `ClaimConflict` and action `Apply` is emitted
- **AND** its message contains `media/config` and the field `spec`
- **AND** no `ApplyFailed` event is emitted
