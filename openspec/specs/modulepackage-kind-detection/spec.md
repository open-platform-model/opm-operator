## Purpose

Defines how the Release reconciler inspects the evaluated CUE `kind` field at runtime and dispatches to the appropriate render pipeline.

## Requirements

### Requirement: Runtime kind detection
After CUE evaluation, the Release reconciler MUST inspect the `kind` field of the evaluated CUE value. Only `ModuleRelease` is a renderable kind; any other kind value MUST be rejected without applying anything.

#### Scenario: ModuleRelease detected
- **WHEN** the evaluated CUE value has `kind: "ModuleRelease"`
- **THEN** the reconciler dispatches to the ModuleRelease render pipeline

#### Scenario: Unsupported kind
- **WHEN** the evaluated CUE value has a `kind` field that is not `ModuleRelease`
- **THEN** the reconciler sets `Ready=False` with reason `UnsupportedKind` and `Stalled=True`
- **AND** nothing is applied

#### Scenario: Missing kind field
- **WHEN** the evaluated CUE value does not have a `kind` field
- **THEN** the reconciler sets `Ready=False` with reason `RenderFailed` and `Stalled=True`
