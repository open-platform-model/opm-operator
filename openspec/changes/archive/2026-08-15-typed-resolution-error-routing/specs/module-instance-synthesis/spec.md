# module-instance-synthesis — Delta

## MODIFIED Requirements

### Requirement: Status reporting

The `status.source` field MAY be updated to reflect:

- The CUE module path and version (from `spec.module`).
- Whether module resolution from the registry succeeded.

The `status.conditions` MUST report:

- `Ready=True` when the module is successfully resolved, rendered, and applied.
- `Ready=False` with reason `ResolutionFailed` when the module cannot be resolved
  into a usable, trustworthy input for rendering. This covers:
  - CUE cannot resolve the module from the registry.
  - The acquired module's declared identity (module path or version in its
    metadata) disagrees with the coordinate it was fetched by.
  - The module demands contracts that the materialized platform does not
    provide.
- `Ready=False` with reason `RenderFailed` when CUE evaluation or rendering fails
  for a cause that is not a resolution-class failure.
- `Stalled=True` when the failure is not transient (e.g. module path does not exist).

#### Scenario: Success reported
- **WHEN** the module resolves, renders, and applies successfully
- **THEN** `status.conditions` reports `Ready=True`

#### Scenario: Resolution failure reported
- **WHEN** CUE cannot resolve the module from the registry
- **THEN** `status.conditions` reports `Ready=False` with reason `ResolutionFailed` and `Stalled=True` when the failure is not transient

#### Scenario: Identity mismatch reported as resolution failure
- **WHEN** the acquired module's declared identity disagrees with the coordinate it was fetched by (mismatched module path or version)
- **THEN** `status.conditions` reports `Ready=False` with reason `ResolutionFailed` and `Stalled=True`, and a Warning event carries the mismatch message

#### Scenario: Unresolved platform demands reported as resolution failure
- **WHEN** the module demands contracts the materialized platform does not provide — including when that failure is reported together with unmatched-component failures
- **THEN** `status.conditions` reports `Ready=False` with reason `ResolutionFailed` and `Stalled=True`, and a Warning event carries the unresolved-demands message

#### Scenario: Render failure reported
- **WHEN** CUE evaluation or rendering fails for a cause that is not a resolution-class failure
- **THEN** `status.conditions` reports `Ready=False` with reason `RenderFailed` and `Stalled=True` when user input must change to resolve the failure
