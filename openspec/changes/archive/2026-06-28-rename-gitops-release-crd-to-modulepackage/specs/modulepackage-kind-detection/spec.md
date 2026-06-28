## ADDED Requirements

<!-- Renamed from `release-kind-detection` (0002 D2/D10); the spec dir is `git mv`'d at archive. -->

### Requirement: Runtime kind detection
After CUE evaluation, the ModulePackage reconciler MUST inspect the `kind` field of the evaluated CUE value. Only `ModuleInstance` is a renderable kind; any other kind value MUST be rejected without applying anything.

#### Scenario: ModuleInstance detected
- **WHEN** the evaluated CUE value has `kind: "ModuleInstance"`
- **THEN** the reconciler dispatches to the ModuleInstance render pipeline

#### Scenario: Unsupported kind
- **WHEN** the evaluated CUE value has a `kind` field that is not `ModuleInstance`
- **THEN** the reconciler sets `Ready=False` with reason `UnsupportedKind` and `Stalled=True`
- **AND** nothing is applied

#### Scenario: Missing kind field
- **WHEN** the evaluated CUE value does not have a `kind` field
- **THEN** the reconciler sets `Ready=False` with reason `RenderFailed` and `Stalled=True`
