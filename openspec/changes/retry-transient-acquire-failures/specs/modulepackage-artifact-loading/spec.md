## MODIFIED Requirements

### Requirement: CUE evaluation with registry resolution
The Release reconciler MUST evaluate the CUE package at `spec.path` using `CUE_REGISTRY` set from the controller's `--registry` flag or `OPM_REGISTRY` environment variable. A failure to load the package that carries no typed terminal cause (see `reconcile-backoff`, "Acquisition failures without a typed terminal cause are transient") MUST retry on the exponential backoff, not stall.

#### Scenario: Successful CUE evaluation
- **WHEN** `CUE_REGISTRY` is configured and the CUE package at `spec.path` evaluates successfully (all module dependencies resolve from the registry)
- **THEN** the reconciler receives a concrete CUE value representing the release

#### Scenario: Module dependency resolution failure
- **WHEN** a CUE module dependency referenced in the package's `cue.mod/module.cue` cannot be resolved from the registry
- **THEN** the reconciler sets `Ready=False` with reason `ResolutionFailed`, does not set `Stalled=True`, and requeues on the exponential backoff capped at 5 minutes

#### Scenario: CUE evaluation error
- **WHEN** the package loads but its render fails evaluation for a cause that is not a resolution-class failure
- **THEN** the reconciler sets `Ready=False` with reason `RenderFailed` and `Stalled=True`

#### Scenario: Structurally invalid package stalls
- **WHEN** the package at `spec.path` loads but is structurally invalid or lacks a required identity field (the library's `ErrInvalidPackage` or `ErrMissingRequiredField`)
- **THEN** the reconciler sets `Ready=False` with reason `ResolutionFailed` and `Stalled=True`
