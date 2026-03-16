## Why

The `ModuleRelease` reconciler needs to resolve its `spec.sourceRef` to an actual Flux `OCIRepository` object, validate that the source is ready, and extract artifact metadata (URL, revision, digest). This is Phase 1 of the reconcile loop and blocks all downstream phases. The current controller stub ignores the source reference entirely.

## What Changes

- Implement `Resolve` function in `internal/source` that looks up an `OCIRepository` by reference, validates readiness, and returns structured artifact metadata.
- Add OCIRepository watch to the `ModuleReleaseReconciler` so that source artifact changes trigger reconciliation.
- Expand `ArtifactRef` to carry all metadata needed by downstream phases (URL, revision, digest).

## Capabilities

### New Capabilities

- `source-resolution`: Resolve a Flux OCIRepository from a ModuleRelease source reference, validate readiness, and extract artifact metadata.

### Modified Capabilities

## Impact

- `internal/source/` — new `Resolve` function and expanded `ArtifactRef`.
- `internal/controller/modulerelease_controller.go` — add `.Watches` for OCIRepository.
- SemVer: MINOR — new capability, no breaking changes.
- Depends on: change 1 (CLI dependency) being merged for the project to build, though this change itself does not import CLI packages.
