## ADDED Requirements

### Requirement: Dependabot leaves cuelang.org/go to library releases
The `gomod` entry of `.github/dependabot.yml` SHALL ignore the dependencies `cuelang.org/go` and `cuelabs.dev/go/oci/ociregistry` (the CUE team's OCI module, whose version the library's `go.mod` sets, `cuelang.org/go` setting only a floor), so the operator's CUE version moves only through a library release: the library bumps CUE in its own pull request, where the closedness canary runs, and the operator picks the new version up when the release cascade (or a hand-made `fix(deps)` pull request) moves the library pin, whose `go get` of the library raises `cuelang.org/go` by minimal version selection (kept by `go mod tidy`). Other third-party Go modules SHALL keep their Dependabot updates. Source: owner decision j4 of the kernel beta.1 plan walkthrough (2026-10-03); workspace RELEASING.md, section "Pin classes".

#### Scenario: New CUE release opens no Dependabot PR
- **WHEN** a new `cuelang.org/go` version is published and Dependabot's weekly `gomod` run executes
- **THEN** Dependabot opens no pull request for `cuelang.org/go` or `cuelabs.dev/go/oci/ociregistry` in opm-operator

#### Scenario: CUE arrives with a library release
- **WHEN** a library release that requires a newer `cuelang.org/go` is pinned by the cascade's library bump
- **THEN** that bump pull request moves `cuelang.org/go` in `go.mod` and `go.sum` to at least the version the library requires, and the cascade reports the move as a warning (`.tasks/cascade/cascade.sh`, Phase C, `warn_deps`)

#### Scenario: Other third-party modules still update
- **WHEN** a newer `github.com/fluxcd/pkg/runtime` is published
- **THEN** Dependabot still proposes the bump

## MODIFIED Requirements

### Requirement: Dependabot leaves OPM Go modules to the release cascade
The `gomod` entry of `.github/dependabot.yml` SHALL ignore every dependency matching `github.com/open-platform-model/*`. Those pins move only through the release cascade, which titles a shipped bump `fix(deps)` so it releases (workspace RELEASING.md, section "Pin classes"). The `github-actions` entry SHALL ignore `open-platform-model/.github*`: the cascade references move together, one `.github` SHA for the repo, only through a `ci(deps): pin the cascade to .github <sha7>` pull request (Phase 3 wiring contract (version 3.1) §2.4, §10.1 item 7). Third-party Go modules, except `cuelang.org/go` and `cuelabs.dev/go/oci/ociregistry` (see "Dependabot leaves cuelang.org/go to library releases"), and other GitHub Actions SHALL keep their Dependabot updates.

#### Scenario: Library release opens no Dependabot PR
- **WHEN** library publishes a new tag
- **THEN** Dependabot opens no PR bumping `github.com/open-platform-model/library` in opm-operator

#### Scenario: Third-party bumps continue
- **WHEN** a new `k8s.io/api` release exists
- **THEN** Dependabot still proposes the grouped Kubernetes bump

#### Scenario: A .github main commit opens no Dependabot PR
- **WHEN** a commit lands on `open-platform-model/.github` `main` after the repo's pinned SHA
- **THEN** Dependabot opens no PR moving any `open-platform-model/.github` reference, and the five references keep one SHA
