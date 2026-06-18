## ADDED Requirements

### Requirement: Example modules published to GHCR on release

The release workflow SHALL, when release-please cuts a release, publish each example test module as a CUE module to `ghcr.io/open-platform-model` using the registry mapping `CUE_REGISTRY='opmodel.dev=ghcr.io/open-platform-model,registry.cue.works'` (matching how `core` and `catalog` publish). Publication SHALL run only on a successful release cut, not on every push to `main`.

#### Scenario: Release triggers module publish
- **WHEN** a Release PR is merged and release-please creates a release tag
- **THEN** the release job publishes each example module via `cue mod publish` to `ghcr.io/open-platform-model` under its `opmodel.dev/modules/test/<m>` path

#### Scenario: No publish without a release
- **WHEN** commits land on `main` but release-please does not cut a release
- **THEN** no example module is published

### Requirement: Independent per-module versioning

Each example module SHALL carry its own version in its `cue.mod/module.cue` (the `@vN` major in `module:` plus the published semver tag), independent of the operator's release version. The publish step SHALL use each module's declared version, NOT the operator's release tag.

#### Scenario: Module version differs from operator version
- **WHEN** the operator releases `v0.5.0` and the podinfo module declares version `v0.1.0`
- **THEN** the publish step publishes `opmodel.dev/modules/test/podinfo` at `v0.1.0`, not `v0.5.0`

### Requirement: Idempotent re-publish

The publish step SHALL be safe to re-run across releases. It SHALL publish a module only when that module's declared version is not already present in the registry, and SHALL treat an already-present version as success (no failure, no overwrite).

#### Scenario: Unchanged module already published
- **WHEN** a release runs and a module's declared version already exists in `ghcr.io/open-platform-model`
- **THEN** the step skips that module and the job does not fail

#### Scenario: Module version bumped since last release
- **WHEN** a module's `module.cue` version was bumped since the previous release tag
- **THEN** the step publishes the new version

### Requirement: Example manifests uploaded as release artifacts

The release job SHALL attach the example `ModuleRelease` manifests (and any accompanying `Release`/`OCIRepository` manifests) to the GitHub Release as downloadable assets, mirroring the existing `install.yaml` upload, so users can apply an example without cloning the repo.

#### Scenario: Manifests attached to release
- **WHEN** the release job completes for tag `vX.Y.Z`
- **THEN** the GitHub Release for `vX.Y.Z` includes the example ModuleRelease manifest assets

#### Scenario: Attached manifests reference published modules
- **WHEN** a user downloads an attached example manifest and applies it
- **THEN** its `spec.module.path` references a `opmodel.dev/modules/test/<m>@v0` version that the same release published
