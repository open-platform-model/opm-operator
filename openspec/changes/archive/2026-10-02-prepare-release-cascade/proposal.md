## Why

The workspace is moving to a release cascade (workspace RELEASING.md, sections "The cascade" and
"Gates"): each upstream release opens one rolling `deps/cascade` PR in every downstream repo, and a
human merges it and then the release PR. opm-operator sits at tier 2 of the release order
(workspace RELEASING.md, section "Release order"): it consumes library (a shipped Go pin,
`go.mod:14`) and the opm CLI (a release-tool pin, installed in four workflows), and the cli embeds
its published `install.yaml`. Four things in this repo work against the cascade today:

- Nothing stops a release PR from shipping a `replace` directive, a pseudo-version of an OPM Go
  module, a `-0.dev.` CUE pin in a published fixture, or a tracked `cue.mod/local-module.cue`.
  Every one of those would publish an operator image or fixture built against something no
  consumer can reproduce.
- Dependabot proposes `github.com/open-platform-model/library` bumps as `build(deps)`, which never
  releases (`.github/dependabot.yml:9-23`, prefix at line 16). That races the cascade PR and lands a library bump
  that the cli never hears about.
- `docs` is a visible changelog section (`release-please-config.json:24`), so a doc-only commit
  cuts an operator release, which would then cascade a pointless bump into the cli. `docs` is
  hidden in library, opm-operator and cli by owner decision 2026-10-01 (RELEASING.md, Pin
  classes). `AGENTS.md:146` still lists `docs` as releasing and is corrected in the same
  section.
- The opm CLI version is hard-coded in four `go install` lines
  (`.github/workflows/test.yml:59`, `test-e2e.yml:98`, `publish-fixtures.yml:66`,
  `release.yml:274`), all still at `v1.0.0-beta.2` while the cli had published `v1.0.0-beta.4` at planning time
  (beta.5 since; the pin moves there with `task deps:pins:opm-cli` as its own `ci(deps)` PR after
  this change merges).
  The cascade App deliberately has no Workflows permission, so it can never move a pin that lives
  in `.github/workflows/` (workspace RELEASING.md, section "Cascade files").

This change prepares the repo; it adds no cascade automation. The receiver task and the
reusable workflows come in later changes (see "Depends on / gates").

## What Changes

- **G1 release-pin gate.** A `task deps:release-check` backed by a new `hack/release-pin-check.sh`
  fails on: Go `replace` directives in `go.mod`; an OPM Go pin (`github.com/open-platform-model/*`)
  that is a pseudo-version or is not an existing tag of its repo; a `-0.dev.` dependency pin in a
  published fixture's `cue.mod/module.cue` (`test/fixtures/modules/*`,
  `test/fixtures/modulepackages/*`); any tracked `cue.mod/local-module.cue`. It runs as a step
  inside the existing `lint` job of `.github/workflows/lint.yml`, only on release-please PRs
  (`${{ github.head_ref || github.ref_name }}` starts with `release-please--`). That job is
  renamed from `Run on Ubuntu` (a name `test.yml` and `test-e2e.yml` also use) to `Lint`, so its
  check name is unique and a ruleset can require it.
- **Dependabot ignore.** The `gomod` entry in `.github/dependabot.yml` ignores
  `github.com/open-platform-model/*`; the cascade owns those bumps.
- **Docs commits stop releasing.** `release-please-config.json` hides the `docs` section
  (`"hidden": true`). `refactor` stays visible and keeps releasing, so library rewrites still
  integrate early. Past CHANGELOG entries are untouched. `AGENTS.md` "Commit type decides the
  release" is rewritten to match.
- **One opm CLI pin file.** A repo-root `.opm-cli-version` (one line, `v1.0.0-beta.4`) replaces the
  four hard-coded versions; every workflow installs `cli/cmd/opm@` the version read from that
  file. This also moves the pin from `v1.0.0-beta.2` to `v1.0.0-beta.4`, folding in the operator's
  Phase 1 opm CLI catch-up (workspace RELEASING.md, section "Rollout and changes").

Release class: none. Every commit is `ci` or `ci(deps)`, a hidden section, so the change cuts no
operator release on its own; after GA it would still be no release (tooling only). No API type, CRD,
controller or reconcile phase changes.

## Depends on / gates

- **Depends on workspace `docs/release-cascade`** (workspace RELEASING.md plus
  `.tasks/deps/opm-cli.sh` writing `.opm-cli-version`). The current workspace script bumps the
  operator by rewriting `cli/cmd/opm@v…` in workflow files; once this change lands those literals
  are gone and the old script silently skips the operator. The workspace branch must merge first,
  or in the same sitting.
- **Gated by owner settings** (workspace RELEASING.md, section "Owner settings"): G1 is advisory
  until the opm-operator ruleset on `main` requires the check `Lint` (the `lint` job of
  `lint.yml`, renamed in task 2.3 so no other job reports that name). This change works without
  it.
- **Not dependent on** library `prepare-release-cascade`, cli `prepare-release-cascade` or catalog_opm
  `prepare-release-cascade`; they are parallel Phase 1 changes with the same shape.
- **Gates later changes:** opm-operator `add-deps-cascade-task` (writes `.opm-cli-version` and
  extends `.tasks/deps.yaml`) and opm-operator `join-release-cascade` (which also needs `.github`
  `add-release-cascade-workflows`) assume the file, the gate and the Dependabot ignore exist.
- **Docs hiding waits for opmodel.dev.** Depends on: opmodel.dev change
  `build-docs-from-branch-head` (branch `feat/build-docs-from-branch-head`) merged before the
  commit of section 4 ("Stop doc-only commits from releasing") merges. That change builds the
  operator docs from the release-branch head, as the site already does for core and catalog_opm;
  without it, hiding `docs` delays every docs-only fix on opmodel.dev until the next operator
  release and the cli release that embeds it.
- **Folds in the opm CLI catch-up.** This change is opm-operator's Phase 1 opm CLI catch-up; do not
  run `deps:pins:opm-cli` against the operator before it merges.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `release-automation`: docs commits no longer open a Release PR or appear in the changelog; a
  release PR must pass the release-pin gate; Dependabot leaves OPM Go modules alone; CI installs
  the opm CLI from `.opm-cli-version`.

## Impact

- **Files**: `.github/workflows/{lint,test,test-e2e,publish-fixtures,release}.yml`,
  `.github/dependabot.yml`, `release-please-config.json`, new `.opm-cli-version`, new
  `hack/release-pin-check.sh`, new `.tasks/deps.yaml` included from `Taskfile.yml`, `AGENTS.md`
  (the "Commit type decides the release" bullet and two notes on the pin file and the gate).
- **Untouched**: `hack/fixtures.sh` stays byte-identical to the cli copy (`.tasks/examples.yaml:11-12`).
- **Release behaviour**: after merge, a `docs`-only window since the last tag opens no Release
  PR; `docs` commits that ride with a releasable commit stop appearing in CHANGELOG.md.
- **opmodel.dev**: today the site builds opm-operator docs at exactly the operator version the
  newest cli tag pins (`opmodel.dev/site/versions.conf:7-9`). Until opmodel.dev
  `build-docs-from-branch-head` merges, a docs-only fix in `docs/` reaches the site only with the
  next operator release (and the cli release that embeds it), unless a normal PR sets `release-as`
  in `release-please-config.json` (owner decision 2026-10-02: squash commits carry only the PR
  title, so no footer reaches `main`). Section 4 is therefore gated on that change (see "Depends
  on / gates").
- **Library bumps are manual for a while.** Until opm-operator `join-release-cascade` goes live, a
  library release reaches the operator only through a hand-made `fix(deps)` PR
  (`go get github.com/open-platform-model/library@vX && go mod tidy`); Dependabot no longer
  proposes it.
- **Delivery**: one PR; the OpenSpec archive commit rides that PR and nothing is pushed to `main`
  (owner decision 2026-10-01 (RELEASING.md, Owner settings): main takes changes only through
  PRs).
