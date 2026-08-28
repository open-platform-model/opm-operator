# example-module-publishing Specification

## Purpose
TBD - created by archiving change add-example-test-modules. Update Purpose after archive.
## Requirements
### Requirement: Example modules published to GHCR on release

On an operator release, CI SHALL publish each example module to `ghcr.io/open-platform-model` under its `testing.opmodel.dev/modules/operator/<m>` path, using a registry mapping that routes BOTH `testing.opmodel.dev` (the fixtures) and `opmodel.dev` (their core and catalog dependencies) to GHCR. A mapping missing either domain falls through to a public mirror and fails with 401.

Publishing SHALL go through `opm module publish`, installed in CI from a pinned cli release, so every publish gate runs over each fixture and a fixture that violates a gate fails the release instead of shipping. The declared version SHALL be read from the module's own `identity/` package; it SHALL NOT be passed in by the caller, and a module without an identity package SHALL be a hard error rather than a silent skip.

#### Scenario: Release triggers module publish

- **WHEN** a release is created
- **THEN** the release job publishes each example module via `opm module publish` to `ghcr.io/open-platform-model` under its `testing.opmodel.dev/modules/operator/<m>` path

#### Scenario: No publish without a release

- **WHEN** a commit lands on the default branch without creating a release
- **THEN** no example module is published

#### Scenario: Fixture missing an identity package

- **WHEN** a directory under the fixtures root carries no `identity/` package
- **THEN** the publish step SHALL fail, naming the directory
- **AND** SHALL NOT report success

### Requirement: Independent per-module versioning

Each example module SHALL declare its own semver in its `identity/` package, independent of the operator's release version. The publish step SHALL publish that version and no other.

#### Scenario: Module version differs from operator version

- **WHEN** the operator releases `v0.5.0` and `podinfo`'s identity package declares `0.1.0`
- **THEN** the publish step publishes `testing.opmodel.dev/modules/operator/podinfo` at `v0.1.0`, not `v0.5.0`

### Requirement: Idempotent re-publish

The publish step SHALL be safe to re-run across releases. It SHALL publish a module only when that module's declared version is not already present in the registry, and SHALL treat an already-present version as success (no failure, no overwrite).

#### Scenario: Unchanged module already published
- **WHEN** a release runs and a module's declared version already exists in `ghcr.io/open-platform-model`
- **THEN** the step skips that module and the job does not fail

#### Scenario: Module version bumped since last release
- **WHEN** a module's `module.cue` version was bumped since the previous release tag
- **THEN** the step publishes the new version

### Requirement: Example manifests uploaded as release artifacts

The release SHALL attach the example `moduleinstance.yaml` manifests, each referencing a version the same release published.

#### Scenario: Attached manifests reference published modules

- **WHEN** a user downloads an attached example manifest
- **THEN** its `spec.module.path` references a `testing.opmodel.dev/modules/operator/<m>@v0` version that the same release published

### Requirement: Version bumps are load-bearing on a schema crossing

Because published CUE module versions are immutable and the publish task treats an already-existing tag as success, any change to a fixture module's content — in particular a schema-line crossing — SHALL bump that module's declared `metadata.version` in the same change. A crossing without a bump publishes nothing and MUST be treated as a defect. The publish task's version source (the first `version:` literal in `module.cue`) SHALL be documented beside the task.

#### Scenario: Crossing republishes

- **WHEN** a fixture module is reauthored to a new schema line with its version bumped
- **THEN** the next release's publish job ships the new bytes at the new tag
- **AND** the previous tag continues to resolve the previous bytes

#### Scenario: Unbumped crossing is caught

- **WHEN** a fixture's content changes without a version bump
- **THEN** the publish is a no-op against the old bytes and review flags the missing bump

### Requirement: Bundle packages the modulepackage manifests

The examples bundle SHALL package the `test/fixtures/modulepackages/` OCIRepository and ModulePackage manifests alongside the ModuleInstance manifests (the bundle's release directory points at the directory that exists).

#### Scenario: Bundle content

- **WHEN** the release bundles examples
- **THEN** `dist/opm-examples.tar.gz` contains the ModuleInstance manifests and the modulepackage OCIRepository/ModulePackage manifests

### Requirement: Pre-release publishes move the declared version with the tag

When publishing with a pre-release identifier, the publish step SHALL tag the artifact `v<version>-<prerelease>` AND SHALL make the artifact's own declared version equal that same value, because the acquire-time identity check refuses a module whose declared version differs from the fetched tag.

The declared version SHALL be rewritten on a staged copy of the tree using the identity-package writer, leaving the working tree unmodified. The publish command's `--version` flag SHALL NOT be used for this: it asserts an already-declared version rather than filling one, and these fixtures declare a defaulted version.

#### Scenario: Pre-release tag and declared version agree

- **WHEN** the fixtures are published with pre-release identifier `e2e.gabc1234`
- **THEN** each is tagged `v<version>-e2e.gabc1234`
- **AND** the published artifact's declared version is `<version>-e2e.gabc1234`

#### Scenario: Working tree unmodified by a pre-release publish

- **WHEN** a pre-release publish completes
- **THEN** no file under the fixture trees has changed on disk
