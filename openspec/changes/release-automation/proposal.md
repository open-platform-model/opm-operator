## Why

The controller has no release automation. Version bumps, changelog generation, git tags, and GitHub Releases are all manual. Conventional Commits are already enforced but not leveraged for automated versioning. Adding release-please automates MINOR and PATCH releases while keeping MAJOR bumps under human control.

## What Changes

- Add a release-please GitHub Actions workflow that opens/updates a Release PR on each push to `main`.
- Add a release-please configuration file scoping behavior to this repository.
- Conventional Commits determine bump level: `feat` → MINOR, `fix`/`perf` → PATCH.
- MAJOR bumps are manual-only — `feat!:` and `BREAKING CHANGE:` footers are not used; major versions are set explicitly via `release-as` override when needed.
- release-please auto-generates `CHANGELOG.md` grouped by commit type.
- On Release PR merge, release-please creates a git tag and GitHub Release with release notes.

## Capabilities

### New Capabilities

- `release-automation`: GitHub Actions workflow and configuration for automated semantic version bumps, changelog generation, and GitHub Release creation via release-please.

### Modified Capabilities
<!-- None — this change adds CI/workflow files only, no existing spec behavior changes. -->

## Impact

- **New files**: `.github/workflows/release.yml`, `release-please-config.json`, `.release-please-manifest.json`.
- **No Go code changes**: No changes to controller source, APIs, or reconciliation logic.
- **No dependency changes**: release-please runs as a GitHub Action, not a Go dependency.
- **Team process**: Contributors continue using Conventional Commits as-is. Release cadence shifts from manual tagging to PR-merge-driven.
- **SemVer**: PATCH (additive CI tooling, no behavioral change to the controller).
