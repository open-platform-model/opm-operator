## ADDED Requirements

### Requirement: Condition type constants
The `internal/status` package MUST define string constants for condition types: `ReadyCondition`, `ReconcilingCondition`, `StalledCondition`, `SourceReadyCondition`.

#### Scenario: Constants available
- **WHEN** code imports `internal/status`
- **THEN** all four condition type constants are available and match the design doc values

### Requirement: Reason constants
The `internal/status` package MUST define string constants for condition reasons: `Suspended`, `SourceNotReady`, `SourceUnavailable`, `ArtifactFetchFailed`, `ArtifactInvalid`, `RenderFailed`, `ApplyFailed`, `PruneFailed`, `ReconciliationSucceeded`.

#### Scenario: Reason constants available
- **WHEN** code imports `internal/status`
- **THEN** all reason constants are available as exported string constants

### Requirement: Condition helper functions
The `internal/status` package MUST provide helper functions for common condition transitions.

#### Scenario: Mark reconciling
- **WHEN** `MarkReconciling` is called on a ModuleRelease with a reason and message
- **THEN** the `Reconciling` condition is set to `True` and `Ready` is set to `Unknown`

#### Scenario: Mark stalled
- **WHEN** `MarkStalled` is called on a ModuleRelease with a reason and message
- **THEN** the `Stalled` condition is set to `True` and `Ready` is set to `False`

#### Scenario: Mark ready
- **WHEN** `MarkReady` is called on a ModuleRelease with a message
- **THEN** the `Ready` condition is set to `True` and `Reconciling` and `Stalled` are removed

#### Scenario: Mark source ready
- **WHEN** `MarkSourceReady` is called with artifact revision info
- **THEN** the `SourceReady` condition is set to `True`

#### Scenario: Mark source not ready
- **WHEN** `MarkSourceNotReady` is called with a reason
- **THEN** the `SourceReady` condition is set to `False`

### Requirement: Flux condition interface compliance
`ModuleRelease` MUST implement `conditions.Getter` and `conditions.Setter` interfaces from `fluxcd/pkg/runtime/conditions`.

#### Scenario: Interface satisfaction
- **WHEN** a `*ModuleRelease` is passed to Flux condition helpers
- **THEN** the helpers compile and function correctly
