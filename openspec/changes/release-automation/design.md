## Context

The controller follows Conventional Commits v1 and SemVer 2.0.0 (Constitution VI). CI runs lint, unit, and e2e tests on push/PR. No release automation exists — no version tracking, no changelog, no git tags, no GitHub Releases.

The repo is `github.com/open-platform-model/poc-controller`. Docker images are built via `make docker-build` with a hardcoded `controller:latest` tag. No multi-arch publishing workflow exists yet.

## Goals / Non-Goals

**Goals:**
- Automate MINOR and PATCH version bumps from Conventional Commits on `main`.
- Auto-generate a changelog grouped by commit type.
- Create git tags and GitHub Releases on release PR merge.
- Keep MAJOR bumps manual and intentional.

**Non-Goals:**
- Docker image build/push automation (separate future change).
- Binary release artifacts, SBOMs, or signing (separate future change).
- Version injection into Go binary via ldflags (separate future change).
- Enforcing Conventional Commits in CI (already followed by convention).
- Branch-based release strategies (single `main` branch for now).

## Decisions

### 1. Tool: release-please over semantic-release

**Choice**: Google's `release-please` GitHub Action.

**Why over semantic-release**:
- release-please creates a **Release PR** that accumulates changes — gives human review before cutting a release. semantic-release publishes immediately on merge to `main` with no review gate.
- release-please is config-file driven (JSON), no plugin ecosystem to manage.
- Native support for Go release type and changelog generation.
- Simpler CI footprint — single workflow file, no npm dependencies.

**Why over goreleaser**:
- goreleaser handles artifact building (binaries, Docker images), not version determination. Complementary, not competing — goreleaser can be added later triggered by the tag release-please creates.

### 2. MAJOR bumps: manual-only via `release-as`

**Choice**: Do not use `feat!:` or `BREAKING CHANGE:` footer in commits. MAJOR bumps are triggered by adding `release-as: X.0.0` to the release-please config or editing the Release PR.

**Why**: Breaking changes are rare and high-stakes for a Kubernetes controller. Automated MAJOR bumps risk accidental API-breaking releases. Human judgment on when to cross a major version boundary is worth the small friction.

### 3. Changelog: CHANGELOG.md in repo root

**Choice**: release-please maintains `CHANGELOG.md` at repo root.

**Why**: Standard location. release-please updates it as part of the Release PR, so changes are reviewable before merge. Grouped by type (Features, Bug Fixes, etc.).

### 4. Initial version: 0.1.0

**Choice**: Start at `0.1.0` in `.release-please-manifest.json`.

**Why**: The controller is pre-v1 (v1alpha1 API). Starting at 0.1.0 signals pre-stability per SemVer. release-please increments from this baseline.

### 5. Commit types that trigger releases

**Choice**:
- `feat` → MINOR bump
- `fix`, `perf` → PATCH bump
- `chore`, `docs`, `test`, `ci`, `refactor` → no version bump (included in next release's changelog under "Miscellaneous")

**Why**: Matches Conventional Commits specification. Non-user-facing changes don't warrant a release on their own but are captured when the next feat/fix triggers one.

## Risks / Trade-offs

**Release PR noise** → The release PR stays open and updates on every push to `main`. Could feel noisy. Mitigation: this is normal release-please behavior; team labels or filters can manage it.

**Pre-1.0 SemVer semantics** → At 0.x, SemVer says MINOR can include breaking changes. This is acceptable for the current maturity level. Mitigation: upgrade to 1.0.0 intentionally when API stabilizes.

**No enforcement of Conventional Commits** → If a commit doesn't follow the convention, release-please ignores it (no bump). Mitigation: team discipline is strong; can add commitlint CI later if needed.

**Single-package repo assumption** → release-please is configured for one releasable unit. If the repo gains multiple release targets, the manifest config supports it but would need updating. Mitigation: unlikely given controller architecture.
