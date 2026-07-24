## Purpose

Define the operator's build-time version identity: a source-burned constant
maintained by release automation, exposed at startup and via
`Platform.status.operatorVersion`. Slice A6 of enhancement 0006 (D24, operator
half).

## Requirements

### Requirement: Version constant is burned into source and maintained by release automation

The operator SHALL carry its version as a Go constant in `internal/version` annotated with `x-release-please-version`, and `release-please-config.json` SHALL list that file under `extra-files` so every Release PR bumps the constant in the same commit the release tag points at. No build path (Dockerfile, Makefile, CI) is required to inject a version.

#### Scenario: Tagged commit self-describes

- **WHEN** the source at a release tag is built by any means (container image, local `go build`, `go install`)
- **THEN** the binary reports the tag's version without any build flags

#### Scenario: Annotation guarded against reformat

- **WHEN** the `internal/version` unit tests run
- **THEN** they fail if the `x-release-please-version` annotation no longer sits on the `Version` const line or the constant no longer parses as semver

### Requirement: Version value contract

The published version string SHALL be the `v`-prefixed constant (matching release tags, e.g. `v1.0.0-alpha.2`). When the binary was built from a VCS checkout exposing build info, the string MAY carry a `+g<short-revision>` suffix, with `.dirty` appended for modified worktrees. Consumers (the CLI's D24 ceiling check) MUST tolerate and strip the `+…` build-metadata suffix before comparison.

#### Scenario: Release image reports the clean tag

- **WHEN** the released container image (built without `.git` in the build stage) runs
- **THEN** it reports exactly `v<constant>` with no suffix

#### Scenario: Dev build carries provenance

- **WHEN** the operator is built from a local git checkout with uncommitted changes
- **THEN** the reported version is `v<constant>+g<short-sha>.dirty`

### Requirement: Version logged at startup

The manager SHALL log the operator version once at startup, before the manager starts.

#### Scenario: Startup log line

- **WHEN** the operator process starts
- **THEN** the setup log contains the resolved version string
