## ADDED Requirements

### Requirement: Events emitted on successful apply
The controller MUST emit a `Normal` event when apply succeeds.

#### Scenario: Apply success event
- **GIVEN** a ModuleRelease whose reconcile reaches Phase 5 and applies resources
- **WHEN** apply completes successfully
- **THEN** a `Normal` event with reason `Applied` is emitted
- **AND** the event message includes the count of created/updated/unchanged resources

### Requirement: Events emitted on apply failure
The controller MUST emit a `Warning` event when apply fails.

#### Scenario: Apply failure event
- **GIVEN** a ModuleRelease whose Phase 5 apply fails
- **WHEN** the reconcile completes
- **THEN** a `Warning` event with reason `ApplyFailed` is emitted
- **AND** the event message includes the error description

### Requirement: Events emitted on prune
The controller MUST emit a `Normal` event when prune succeeds.

#### Scenario: Prune success event
- **GIVEN** a ModuleRelease with stale resources pruned in Phase 6
- **WHEN** prune completes successfully
- **THEN** a `Normal` event with reason `Pruned` is emitted with deleted count

### Requirement: Events emitted on source not ready
The controller MUST emit a `Warning` event when the source is not ready.

#### Scenario: Source not ready event
- **GIVEN** a ModuleRelease whose OCIRepository source is not ready
- **WHEN** Phase 1 detects the source is not ready
- **THEN** a `Warning` event with reason `SourceNotReady` is emitted

### Requirement: Events emitted on render failure
The controller MUST emit a `Warning` event when CUE rendering fails.

#### Scenario: Render failure event
- **GIVEN** a ModuleRelease whose CUE evaluation fails in Phase 3
- **WHEN** the reconcile completes
- **THEN** a `Warning` event with reason `RenderFailed` is emitted

### Requirement: Events emitted on suspend/resume
The controller MUST emit `Normal` events when entering or exiting suspend.

#### Scenario: Suspend event
- **GIVEN** a ModuleRelease with `spec.suspend=true`
- **WHEN** the controller reconciles and detects suspend
- **THEN** a `Normal` event with reason `Suspended` is emitted

### Requirement: Events emitted on overall success
The controller MUST emit a `Normal` event on full reconcile success.

#### Scenario: Reconciliation succeeded event
- **GIVEN** a ModuleRelease whose full reconcile (phases 0-7) completes successfully
- **WHEN** Phase 7 commits status
- **THEN** a `Normal` event with reason `ReconciliationSucceeded` is emitted
