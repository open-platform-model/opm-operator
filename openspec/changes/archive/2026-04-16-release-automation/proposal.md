## Why

The controller has no release automation. Version bumps, changelog generation, git tags, and GitHub Releases are all manual. Conventional Commits are already enforced but not leveraged for automated versioning. Adding release-please automates MAJOR, MINOR, and PATCH releases driven entirely by commit semantics.

## What Changes

- Add a release-please GitHub Actions workflow that opens/updates a Release PR on each push to `main`.
- Add a release-please configuration file scoping behavior to this repository.
- Conventional Commits determine bump level: `feat!:` or `BREAKING CHANGE:` footer → MAJOR, `feat` → MINOR, `fix`/`perf` → PATCH.
- Pre-1.0 (0.x): SemVer allows breaking changes in MINOR; `bump-minor-pre-major` demotes breaking commits to MINOR bumps until the baseline crosses 1.0.0.
- A maintainer MAY still override the proposed version explicitly via `release-as` when automated bump inference is wrong.
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
