## Purpose

Defines how the Release reconciler fetches Flux artifacts, navigates to `spec.path` within the extracted tree, and evaluates the CUE package there — covering unpack, path navigation, registry-aware CUE evaluation, and cleanup.

## Requirements

### Requirement: Artifact unpacking and path navigation
The Release reconciler MUST fetch the Flux source artifact, unpack it to a temporary directory, and navigate to `spec.path` within the extracted tree to locate the release CUE package.

#### Scenario: Valid path with release.cue
- **WHEN** the artifact is unpacked and `spec.path` resolves to a directory containing `release.cue`
- **THEN** the reconciler loads the CUE package from that directory

#### Scenario: Path does not exist in artifact
- **WHEN** `spec.path` does not exist in the extracted artifact
- **THEN** the reconciler sets `Ready=False` with reason `PathNotFound` and `Stalled=True`

#### Scenario: Path exists but no release.cue
- **WHEN** `spec.path` resolves to a directory that does not contain `release.cue`
- **THEN** the reconciler sets `Ready=False` with reason `ReleaseFileNotFound` and `Stalled=True`

### Requirement: Temporary directory cleanup
The Release reconciler MUST clean up the temporary directory used for artifact extraction after CUE evaluation completes, regardless of success or failure.

#### Scenario: Cleanup on success
- **WHEN** CUE evaluation succeeds
- **THEN** the temporary directory is removed via deferred cleanup

#### Scenario: Cleanup on failure
- **WHEN** any phase fails after artifact extraction
- **THEN** the temporary directory is still removed via deferred cleanup

### Requirement: No CUE module validation at artifact root
The Release reconciler MUST NOT require `cue.mod/module.cue` at the artifact root. The CUE module structure is expected at `spec.path`, not at the root of the Flux artifact.

#### Scenario: Git repository artifact without root cue.mod
- **WHEN** the artifact is a GitRepository containing a CUE module at `spec.path` but no `cue.mod/` at the repository root
- **THEN** artifact fetching succeeds and the reconciler navigates to `spec.path` for CUE evaluation

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
