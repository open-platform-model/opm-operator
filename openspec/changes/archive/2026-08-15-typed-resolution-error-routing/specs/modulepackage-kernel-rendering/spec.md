# modulepackage-kernel-rendering — Delta

## ADDED Requirements

### Requirement: Unresolved platform demands classify as resolution failures

When kernel rendering of a ModuleRelease package fails because the module
demands contracts the materialized platform does not provide, the reconciler
MUST report `Ready=False` with reason `ResolutionFailed` and `Stalled=True` —
not `RenderFailed`. The classification MUST hold when the unresolved-demands
failure is reported together with unmatched-component failures.

Kernel-rendering failures that are not resolution-class (ordinary CUE
evaluation errors) MUST keep reason `RenderFailed`, and the
platform-not-materialized state MUST keep reason `PlatformNotReady`.

#### Scenario: Package demands contracts the platform does not provide
- **WHEN** a ModuleRelease package renders against a materialized platform that does not provide contracts the module demands
- **THEN** the ModulePackage reports `Ready=False` with reason `ResolutionFailed` and `Stalled=True`, and a Warning event carries the unresolved-demands message

#### Scenario: Ordinary evaluation error keeps RenderFailed
- **WHEN** kernel rendering fails for a cause that is not a resolution-class failure
- **THEN** the ModulePackage reports `Ready=False` with reason `RenderFailed`
