## ADDED Requirements

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

### Requirement: CUE evaluation with registry resolution
The Release reconciler MUST evaluate the CUE package at `spec.path` using `CUE_REGISTRY` set from the controller's `--registry` flag or `OPM_REGISTRY` environment variable.

#### Scenario: Successful CUE evaluation
- **WHEN** `CUE_REGISTRY` is configured and the CUE package at `spec.path` evaluates successfully (all module dependencies resolve from the registry)
- **THEN** the reconciler receives a concrete CUE value representing the release

#### Scenario: Module dependency resolution failure
- **WHEN** a CUE module dependency referenced in the package's `cue.mod/module.cue` cannot be resolved from the registry
- **THEN** the reconciler sets `Ready=False` with reason `ResolutionFailed`

#### Scenario: CUE evaluation error
- **WHEN** the CUE package contains syntax errors or fails validation
- **THEN** the reconciler sets `Ready=False` with reason `RenderFailed` and `Stalled=True`

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
