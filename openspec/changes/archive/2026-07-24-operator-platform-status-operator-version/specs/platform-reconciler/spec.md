# platform-reconciler (delta)

The reconciler self-publishes the operator version on every status patch (slice A6 of enhancement 0006, D24).

## ADDED Requirements

### Requirement: Reconciler stamps status.operatorVersion on every status patch

`PlatformReconciler` SHALL set `status.operatorVersion` to the running operator's version (from `internal/version`) on every status patch it makes — on successful materialization and on materialize failure alike. The field records which operator version is running against the Platform, independent of materialization outcome.

#### Scenario: Stamped on successful materialization

- **WHEN** the singleton Platform reconciles successfully (`Ready=True`, reason `Materialized`)
- **THEN** `status.operatorVersion` equals the running operator's version string

#### Scenario: Stamped on materialize failure

- **WHEN** materialization fails and the reconciler records `Ready=False` (reason `MaterializeFailed`)
- **THEN** `status.operatorVersion` is still set to the running operator's version string
