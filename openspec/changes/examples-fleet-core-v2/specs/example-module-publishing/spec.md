# example-module-publishing — Delta

## MODIFIED Requirements

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
