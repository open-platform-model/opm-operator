# release-automation (delta)

Release PRs bump the source-burned version constant (slice A6 of enhancement 0006).

## ADDED Requirements

### Requirement: Release PR bumps the annotated version constant

The release-please configuration SHALL list `internal/version/version.go` under the root package's `extra-files`, so every Release PR rewrites the `x-release-please-version`-annotated `Version` constant to the proposed version in the same commit the release tag will point at.

#### Scenario: Release PR includes the constant bump

- **WHEN** release-please opens or updates a Release PR proposing version `X.Y.Z-alpha.N`
- **THEN** the PR's diff sets `internal/version/version.go`'s `Version` constant to `X.Y.Z-alpha.N`
