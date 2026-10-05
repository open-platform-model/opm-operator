## Purpose

Define the condition types and reason constants the operator writes on its
custom resources, and the helpers that set them, so every reconciler reports
Ready, Reconciling, Stalled and source readiness in one shared vocabulary.

## Requirements

### Requirement: Condition type constants
The `internal/status` package MUST define string constants for condition types: `ReadyCondition`, `ReconcilingCondition`, `StalledCondition`, `SourceReadyCondition`.

#### Scenario: Constants available
- **WHEN** code imports `internal/status`
- **THEN** all four condition type constants are available and match the design doc values

### Requirement: Reason constants

The status package SHALL define the reason constants used on the Ready condition, including `SkewRefused` (Ready=False: the platform's skew policy is `Refuse` and the module requires a newer catalog build than the platform pins), `DuplicateIdentities` (Ready=False: two or more rendered objects share one Kubernetes apply identity, so the render is refused before apply naming every producing component; 0015:D15), `ReconcilePanic` (Ready=False, Reconciling=True: a ModuleInstance or ModulePackage reconcile panicked and the attempt was recorded as a failure before the panic propagated) and `RenderTimedOut` (Ready=False, Reconciling=True, not stalled: a ModuleInstance or ModulePackage render did not finish within `--render-timeout`, and the attempt retries on the backoff) beside the existing `ResolutionFailed`, `RenderFailed`, `PlatformNotReady` and the Platform reasons `Generated`, `BuildFailed`, `GenerateFailed`.

#### Scenario: Reason constants available
- **WHEN** code imports `internal/status`
- **THEN** all reason constants are available as exported string constants

#### Scenario: Skew refusal reason is available

- **WHEN** a render is refused under the `Refuse` policy
- **THEN** the reconciler marks `Ready=False` with reason `SkewRefused`

#### Scenario: Duplicate-identity reason is available

- **WHEN** a render is refused because two compiled objects share one apply identity
- **THEN** the reconciler marks `Ready=False` with reason `DuplicateIdentities`

#### Scenario: Reconcile-panic reason is available

- **WHEN** a ModuleInstance or ModulePackage reconcile panics
- **THEN** the reconciler marks `Ready=False` and `Reconciling=True` with reason `ReconcilePanic` and removes `Stalled`

#### Scenario: Render-timeout reason is available

- **WHEN** a ModuleInstance or ModulePackage render does not finish within `--render-timeout`
- **THEN** the reconciler marks `Ready=False` and `Reconciling=True` with reason `RenderTimedOut` and removes `Stalled`

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

### Requirement: The Healthy condition vocabulary

The `internal/status` package SHALL define the condition type `HealthyCondition` (`"Healthy"`) and the reasons `RolledOut` (`Healthy=True`), `NotRolledOut` (`Healthy=False`), `ProgressDeadlineExceeded` (`Healthy=False`, a Deployment's rollout is stalled) and `HealthUnknown` (`Healthy=Unknown`). It SHALL provide one helper that sets `Healthy` from a status, a reason and a message, and touches no other condition. `Healthy` is independent of `Ready`: no helper that sets `Ready`, `Reconciling` or `Stalled` changes it, except that `MarkManagedExternally` and `MarkSelfManagementRefused` remove it.

#### Scenario: The constants are available

- **WHEN** code imports `internal/status`
- **THEN** `HealthyCondition`, `RolledOutReason`, `NotRolledOutReason`, `ProgressDeadlineExceededReason` and `HealthUnknownReason` are exported string constants with those values

#### Scenario: Marking Ready leaves Healthy alone

- **WHEN** `MarkReady`, `MarkReconciling`, `MarkStalled` or `MarkSuspended` is called on an object carrying `Healthy=False`
- **THEN** `Healthy` is unchanged

#### Scenario: The helper sets only Healthy

- **WHEN** the helper sets `Healthy=True` with reason `RolledOut` on an object carrying `Ready=True`
- **THEN** `Healthy` is `True` with reason `RolledOut` and every other condition is unchanged
