## Purpose

Defines how the Release reconciler inspects the evaluated CUE `kind` field at runtime and dispatches to the appropriate render pipeline (ModuleRelease vs BundleRelease).

## Requirements

### Requirement: Runtime kind detection
After CUE evaluation, the Release reconciler MUST inspect the `kind` field of the evaluated CUE value to determine whether it represents a `ModuleRelease` or `BundleRelease`, and dispatch to the appropriate render pipeline.

#### Scenario: ModuleRelease detected
- **WHEN** the evaluated CUE value has `kind: "ModuleRelease"`
- **THEN** the reconciler dispatches to the ModuleRelease render pipeline (`ParseModuleRelease` → `ProcessModuleRelease`)

#### Scenario: BundleRelease detected
- **WHEN** the evaluated CUE value has `kind: "BundleRelease"`
- **THEN** the reconciler dispatches to the BundleRelease render pipeline
- **AND** if BundleRelease rendering is not yet implemented, returns `Ready=False` with reason `UnsupportedKind` and `Stalled=True`

#### Scenario: Unknown kind
- **WHEN** the evaluated CUE value has a `kind` field that is neither `ModuleRelease` nor `BundleRelease`
- **THEN** the reconciler sets `Ready=False` with reason `UnsupportedKind` and `Stalled=True`

#### Scenario: Missing kind field
- **WHEN** the evaluated CUE value does not have a `kind` field
- **THEN** the reconciler sets `Ready=False` with reason `RenderFailed` and `Stalled=True`
