## ADDED Requirements

### Requirement: The Drifted condition can be Unknown

The status package SHALL define the reason constant `DriftCheckForbidden` and a helper that sets the `Drifted` condition to `Unknown` with a given reason and message. The helper SHALL NOT change the `Ready`, `Reconciling` or `Stalled` conditions. The reasons used with it are `DriftCheckForbidden` (the API server refused the dry-run of drift detection) and `ImpersonationFailed` (the effective ServiceAccount could not be impersonated, so drift detection did not run).

#### Scenario: Mark drift unknown

- **WHEN** the helper is called on a ModuleInstance with `Ready=True`, reason `DriftCheckForbidden` and a message
- **THEN** `Drifted` is `Unknown` with that reason and message
- **AND** `Ready` is still `True`

#### Scenario: A verdict replaces Unknown

- **GIVEN** a ModuleInstance with `Drifted=Unknown`
- **WHEN** `ClearDrifted` is called
- **THEN** the `Drifted` condition is removed
