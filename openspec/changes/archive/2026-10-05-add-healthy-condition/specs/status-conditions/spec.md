## ADDED Requirements

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
