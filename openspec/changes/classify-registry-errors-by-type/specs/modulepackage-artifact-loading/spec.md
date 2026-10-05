## REMOVED Requirements

### Requirement: CUE evaluation with registry resolution
**Reason**: Its scenario "Package that fails to load without a typed cause retries" turns false: with the library's typed fetch failures (library `v1.0.0-beta.6`, 0021:D8:R12) a package load that fails for a cause other than a registry fetch (a CUE syntax error, a non-concrete package) stalls. OpenSpec refuses a MODIFIED that drops a scenario, so the requirement is replaced under a new name.
**Migration**: "CUE evaluation with typed registry failure classification" below keeps the registry configuration, the successful evaluation, the dependency resolution failure, the evaluation error and the structurally invalid package, and replaces the retry of an untyped load failure with its stall.

## ADDED Requirements

### Requirement: CUE evaluation with typed registry failure classification
The Release reconciler MUST evaluate the CUE package at `spec.path` using `CUE_REGISTRY` set from the controller's `--registry` flag or `OPM_REGISTRY` environment variable. A package load that fails with a typed registry fetch failure MUST retry on the exponential backoff, not stall; a package load that fails for any other cause MUST stall (see `reconcile-backoff`, "Registry fetch failures are transient wherever they occur").

#### Scenario: Successful CUE evaluation
- **WHEN** `CUE_REGISTRY` is configured and the CUE package at `spec.path` evaluates successfully (all module dependencies resolve from the registry)
- **THEN** the reconciler receives a concrete CUE value representing the release

#### Scenario: Module dependency resolution failure
- **WHEN** a CUE module dependency referenced in the package's `cue.mod/module.cue` cannot be fetched from the registry
- **THEN** the reconciler sets `Ready=False` with reason `ResolutionFailed`, does not set `Stalled=True`, and requeues on the exponential backoff capped at 5 minutes

#### Scenario: Package with an author defect stalls
- **WHEN** loading the package at `spec.path` fails for a cause that is not a registry fetch failure, for example a CUE syntax error, values that conflict with `#config`, or a non-concrete package surfaced by the loader
- **THEN** the reconciler sets `Ready=False` with reason `ResolutionFailed` and `Stalled=True`, and requeues on the 30-minute recheck

#### Scenario: CUE evaluation error
- **WHEN** the package loads but its render fails evaluation for a cause that is neither a resolution-class failure nor a registry fetch failure
- **THEN** the reconciler sets `Ready=False` with reason `RenderFailed` and `Stalled=True`

#### Scenario: Structurally invalid package stalls
- **WHEN** the package at `spec.path` loads but is structurally invalid or lacks a required identity field (the library's `ErrInvalidPackage` or `ErrMissingRequiredField`)
- **THEN** the reconciler sets `Ready=False` with reason `ResolutionFailed` and `Stalled=True`
