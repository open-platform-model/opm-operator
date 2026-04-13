## ADDED Requirements

### Requirement: Suspended reconciliation is skipped
When `spec.suspend` is `true`, the controller MUST skip all reconcile phases and return without requeueing.

#### Scenario: Suspend skips reconciliation
- **GIVEN** a ModuleRelease with `spec.suspend=true`
- **WHEN** the controller reconciles the resource
- **THEN** phases 1-7 are not executed
- **AND** the controller returns without requeueing

### Requirement: Condition reflects suspended state
When suspended, the controller MUST set `Ready=False` with reason `Suspended`.

#### Scenario: Conditions during suspend
- **GIVEN** a ModuleRelease with `spec.suspend=true`
- **WHEN** the controller reconciles the resource
- **THEN** the `Ready` condition is `False` with reason `Suspended` and message "Reconciliation is suspended"
- **AND** the `Reconciling` condition is removed
- **AND** the `Stalled` condition is removed

### Requirement: Status preserved during suspend
Existing status fields MUST NOT be cleared when entering suspend.

#### Scenario: Inventory and digests preserved
- **GIVEN** a ModuleRelease with populated `status.inventory` and digest fields
- **WHEN** `spec.suspend` is set to `true` and the controller reconciles
- **THEN** `status.inventory`, all digest fields, and `status.history` remain unchanged

### Requirement: Resume triggers immediate reconcile
When `spec.suspend` transitions from `true` to `false`, the controller MUST perform a full reconcile.

#### Scenario: Unsuspend triggers reconcile
- **GIVEN** a ModuleRelease with `spec.suspend=true` that was previously suspended
- **WHEN** `spec.suspend` is set to `false`
- **THEN** the controller performs a full reconcile (phases 0-7)

### Requirement: Suspend does not block deletion
Deletion cleanup MUST proceed even when `spec.suspend=true`.

#### Scenario: Deletion during suspend
- **GIVEN** a ModuleRelease with `spec.suspend=true` and a non-zero `DeletionTimestamp`
- **WHEN** the controller reconciles the resource
- **THEN** deletion cleanup proceeds normally (the suspend check is bypassed for deletion)
