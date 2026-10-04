## Purpose

Automate versioning and releases with release-please: Release PRs driven by
Conventional Commits, changelog generation, git tags and GitHub Releases on
merge, and the source-burned version constant bump.

## Requirements

### Requirement: Changelog generation
The workflow SHALL generate and maintain a `CHANGELOG.md` file at the repository root. Entries SHALL be grouped under the visible sections that `release-please-config.json` declares (Features, Bug Fixes, Performance Improvements, Reverts, Dependencies, Code Refactoring). Commits of a hidden type (`docs`, `chore`, `test`, `ci`, `build`) SHALL NOT appear. Documentation entries already in CHANGELOG.md from earlier releases SHALL stay as they are.

#### Scenario: Changelog includes all commit types
- **WHEN** the Release PR is created or updated
- **THEN** `CHANGELOG.md` SHALL list every commit since the last release whose type has a visible section in `release-please-config.json`, grouped under that section, with commit messages as entries, and SHALL list no commit of a hidden type

#### Scenario: Changelog preserves history
- **WHEN** a new release is cut
- **THEN** the new changelog section SHALL be prepended to existing content, preserving prior release entries

### Requirement: Git tag and GitHub Release on merge
When the Release PR is merged to `main`, release-please SHALL create the git tag (e.g., `v0.2.0`) eagerly at the release commit and a GitHub Release in draft state with the changelog section as release notes. The release-please package SHALL be configured with `draft: true` and `force-tag-creation: true`. The draft SHALL become public only through the publish job `publish-release` defined in "Release published once after every release job".

#### Scenario: Release PR merged
- **WHEN** the Release PR is merged to `main`
- **THEN** release-please creates a git tag matching the version (prefixed with `v`) at the release commit and a draft GitHub Release with the changelog for that version as the body, and the release is not visible as published until every release job has succeeded

#### Scenario: Release PR closed without merge
- **WHEN** the Release PR is closed without merging
- **THEN** no tag or release SHALL be created; the next push to `main` re-opens or creates a new Release PR

#### Scenario: Config enables draft-first
- **WHEN** `release-please-config.json` is inspected on `main`
- **THEN** package `"."` SHALL set `"draft": true` and `"force-tag-creation": true`

### Requirement: Release PR bumps the annotated version constant
The release-please configuration SHALL list `internal/version/version.go` under the root package's `extra-files`, so every Release PR rewrites the `x-release-please-version`-annotated `Version` constant to the proposed version in the same commit the release tag will point at. The constant SHALL NOT be edited by hand; only the Release PR changes it.

#### Scenario: Release PR includes the constant bump
- **WHEN** release-please opens or updates a Release PR proposing version `X.Y.Z-beta.N` (or `X.Y.Z` once the line reaches GA)
- **THEN** the PR's diff sets `internal/version/version.go`'s `Version` constant to exactly that version

### Requirement: Version bump determination per release line
The workflow SHALL determine the proposed version from the commits since the last release tag using Conventional Commits v1 semantics, applied per release line. On the prerelease line (`versioning: prerelease`, `prerelease: true`, current version `X.0.0-beta.N`), every releasable commit, breaking or not, SHALL propose `X.0.0-beta.(N+1)`: the bump never changes the label and never moves the major. On a stable line (after GA, no prerelease suffix), `feat` SHALL propose a MINOR bump, a breaking change a MAJOR bump, and any other releasable commit (`fix`, `perf`, `deps`, `refactor`, `revert`) a PATCH bump. A breaking change is a `!` after the type or scope in the commit subject, which for a squash-merged PR is the PR title, the only text the squash commit carries (`squash_merge_commit_message: BLANK`). A `docs` commit is not releasable and proposes no bump.

#### Scenario: fix commit on the beta line advances the counter
- **WHEN** the current version is `1.0.0-beta.1` and only `fix` commits are releasable
- **THEN** the proposed version SHALL be `1.0.0-beta.2`

#### Scenario: feat commit on the beta line advances the counter
- **WHEN** the current version is `1.0.0-beta.2` and the releasable commits include a `feat`
- **THEN** the proposed version SHALL be `1.0.0-beta.3`, not `1.1.0-beta.1`

#### Scenario: Breaking-change commit on the beta line advances the counter
- **WHEN** the current version is `1.0.0-beta.3` and the releasable commits include a squash commit whose subject (the PR title) is `feat(api)!: ...`
- **THEN** the proposed version SHALL be `1.0.0-beta.4`, never `2.0.0` or `2.0.0-beta.1`

#### Scenario: feat commit on a stable line triggers MINOR bump
- **WHEN** the current version is `1.2.3` and the releasable commits include a `feat`
- **THEN** the proposed version SHALL be `1.3.0`

#### Scenario: fix-only commits on a stable line trigger PATCH bump
- **WHEN** the current version is `1.2.3` and the releasable commits include `fix` or `perf` but no `feat`
- **THEN** the proposed version SHALL be `1.2.4`

#### Scenario: Breaking-change commit on a stable line triggers MAJOR bump
- **WHEN** the current version is `1.2.3` and the releasable commits include a squash commit whose subject (the PR title) carries `!`, such as `feat!:` or `fix(deps)!:`
- **THEN** the proposed version SHALL be `2.0.0`

### Requirement: Beta prerelease line
The release-please package SHALL be configured with `versioning: prerelease`, `prerelease: true` and `prerelease-type: beta` while the operator is on its beta line. From its first beta, a prerelease line (opmodel.dev/core@v2, library, cli, opm-operator) is on the path to GA. A breaking change is still allowed during beta, but only as a `!` in the PR title (`feat!:`), which becomes the CHANGELOG entry; its migration note goes in the PR body, which the entry links and which never reaches `main`. It advances the `-beta.N` counter and never moves the module path to a new major. Stable lines (opmodel.dev/catalogs/opm@v4 and the module fleets) keep the normal SemVer rule: a break is a new major. A core beta break that would force a catalogs/opm major needs owner sign-off. An operator `feat!` that the released cli cannot drive SHALL merge only after the cli release that can drive it, and no `release-as` value SHALL hop the operator to a new minor or major (such as `1.1.0-beta.1`) during beta. Every beta GitHub Release SHALL be flagged Pre-release. Changing `prerelease-type` alone SHALL NOT be relied on to change the label of a version that already carries a suffix; a label change needs a `release-as` value (see "Forced version via the release-as config key"). GA drops the suffix: `prerelease: false` plus a visible carrier commit per package, in dependency order.

#### Scenario: Beta releases are flagged Pre-release
- **WHEN** the Release PR for `1.0.0-beta.N` is merged
- **THEN** release-please creates tag `v1.0.0-beta.N` and a GitHub Release marked Pre-release, and the image and `install.yaml` asset are published under that tag

#### Scenario: Breaking change during beta carries its migration note
- **WHEN** a pull request titled `feat(api)!: ...`, whose body describes the migration, is squash-merged on the beta line
- **THEN** the next Release PR proposes the next `-beta.N`, its CHANGELOG section lists the PR title as a breaking-change entry linking the pull request, and the migration note is read from that pull request

#### Scenario: Operator break waits for a cli that can drive it
- **WHEN** an operator `feat!` changes behavior the newest released cli cannot drive
- **THEN** it merges only after a cli release that can drive it, ships as the next `1.0.0-beta.N`, and no `release-as` value moves the operator to `1.1.0-beta.1` or any other minor or major

#### Scenario: Label flip alone keeps the old label
- **WHEN** only `prerelease-type` changes from `alpha` to `beta` while the manifest reads `1.0.0-alpha.22` and `release-please-config.json` carries no `release-as`
- **THEN** the next Release PR proposes `1.0.0-alpha.23`, which is why the crossing sets `"release-as": "1.0.0-beta.1"` in `release-please-config.json`

#### Scenario: GA drops the suffix
- **WHEN** the config sets `prerelease: false` and a visible carrier commit lands on `main` while the manifest reads `1.0.0-beta.N`
- **THEN** the Release PR proposes `1.0.0` and the GitHub Release is not marked Pre-release

### Requirement: Release runs serialized
The release workflow SHALL declare a workflow-level concurrency group keyed by the pushed ref with `cancel-in-progress: false`, so at most one release run executes at a time. Before any asset upload or publication, the workflow SHALL confirm that exactly one GitHub Release carries the release tag, and SHALL fail without uploading or publishing otherwise.

#### Scenario: Two pushes in quick succession
- **WHEN** the Release PR for `v1.0.0-beta.3` merges and another commit lands on `main` seconds later
- **THEN** the second run waits for the first to finish, and exactly one draft release exists for `v1.0.0-beta.3`

#### Scenario: Duplicate release for a tag
- **WHEN** two releases carry the tag `v1.0.0-beta.3`
- **THEN** every upload step and the publish job fail with a message naming the count, and neither release is published

### Requirement: Release assets upload only to a draft release
Every step that uploads an asset to the GitHub Release (`install.yaml`, `opm-examples.tar.gz`, the example manifests) SHALL first confirm that the single release for the tag is a draft and SHALL fail without uploading when it is published. Replacing an existing asset (`--clobber`) SHALL be used only on a draft. Every GitHub CLI call in the release workflow SHALL name the repository explicitly. Source: 0021:D10:R8.

#### Scenario: Upload to the draft
- **WHEN** the image job uploads `install.yaml` for a release that is still a draft
- **THEN** the upload succeeds, and re-running the job replaces the asset on the draft

#### Scenario: Upload to a published release refused
- **WHEN** an upload step runs for a tag whose release is already published
- **THEN** the step fails before uploading and tells the operator to release the next version instead

### Requirement: Release published once after every release job
The release workflow SHALL contain a publish job (`publish-release`) that depends on every job producing a release artifact (the image job and the example publishing job), runs only when a release was cut, holds `contents: write` and no other write permission, checks out only the files it runs, confirms the required assets (`install.yaml`, `opm-examples.tar.gz`) are attached to the draft, and then publishes the draft. It SHALL leave the Pre-release flag set by release-please unchanged. When the release is already published it SHALL succeed without changing anything. The only job that SHALL depend on it is the cascade notify job, which produces no release artifact.

#### Scenario: All release jobs succeed
- **WHEN** the image and example jobs finish successfully for `v1.0.0-beta.3`
- **THEN** `publish-release` publishes the draft, and the GitHub Release is public, flagged Pre-release and carries every asset

#### Scenario: A release job fails
- **WHEN** the example publishing job fails for `v1.0.0-beta.3`
- **THEN** `publish-release` does not run, the release stays a draft, and "Re-run failed jobs" on the same workflow run completes and publishes it

#### Scenario: Required asset missing
- **WHEN** `publish-release` runs and the draft lacks `install.yaml`
- **THEN** the job fails and the release stays a draft

### Requirement: Release tags are never moved, deleted or re-created
No workflow, task or script in this repository SHALL move, delete or re-create a git tag or delete a GitHub Release. A failed run before publish SHALL be recovered by re-running its failed jobs; a wrong published release SHALL be fixed by releasing the next version. Source: 0021:D10:R1.

#### Scenario: Workflows carry no tag mutation
- **WHEN** `.github/workflows/`, `.github/scripts/`, `Taskfile.yml`, `.tasks/` and `hack/` are searched for `git tag`, a `git push` of a tag ref (`refs/tags/`, `--tags`, `--mirror`, a delete or force refspec), `gh release delete`, `gh release edit --tag`/`--target`, or a write to the git refs API
- **THEN** no match exists

#### Scenario: Broken published release
- **WHEN** a published release turns out to ship a broken image or asset
- **THEN** a `fix` commit cuts the next `1.0.0-beta.N`, and the broken release and its tag stay as they are

### Requirement: Release PR opens for releasable commits on push to main
The release-please workflow SHALL run on every push to the `main` branch. It SHALL open a Release PR if releasable commits exist since the last release tag. Releasable commits are those whose type has a visible changelog section in `release-please-config.json` (`feat`, `fix`, `perf`, `revert`, `deps`, `refactor`). `docs`, `chore`, `test`, `ci` and `build` are hidden and SHALL NOT by themselves open a Release PR, and a `release-as` value in `release-please-config.json` does not make them releasable. If a Release PR already exists, it SHALL update the PR with the latest accumulated changes. Hiding `docs` keeps a doc-only commit from cutting an operator release that would cascade a pointless bump into the cli (owner decision 2026-10-01 (RELEASING.md, Pin classes)).

#### Scenario: First feat commit after a release
- **WHEN** a `feat(scope): description` commit is pushed to `main` and no open Release PR exists
- **THEN** release-please opens a new Release PR proposing the next version per "Version bump determination per release line", with an updated CHANGELOG.md

#### Scenario: Subsequent fix commit with open Release PR
- **WHEN** a `fix(scope): description` commit is pushed to `main` and a Release PR already exists
- **THEN** release-please updates the existing Release PR to include the new fix in the changelog, adjusting the proposed version if needed

#### Scenario: Only non-releasable commits
- **WHEN** only `docs`, `chore`, `test`, `ci` or `build` commits are pushed since the last release, whether or not `release-please-config.json` carries a `release-as` value
- **THEN** release-please SHALL NOT open a Release PR (no version bump warranted)

#### Scenario: Docs-only commits do not cut a release
- **WHEN** only `docs` commits are pushed since the last release
- **THEN** release-please SHALL NOT open a Release PR, and `release-please-config.json` SHALL declare the `docs` section with `"hidden": true`

#### Scenario: Refactor commits still cut a release
- **WHEN** only `refactor` commits are pushed since the last release
- **THEN** release-please opens a Release PR listing them under Code Refactoring

### Requirement: Release PR passes the release-pin gate
On every Release PR, the release-pin gate (G1, workspace RELEASING.md, section "Gates") SHALL run as a step inside an existing CI job that runs on every pull request, never as a job of its own, so a skipped job can never report a pass. A PR is a Release PR when `${{ github.head_ref || github.ref_name }}` starts with `release-please--`. The gate SHALL fail the job when any of the following holds on the PR head:
- `go.mod` carries a `replace` directive;
- a required module under `github.com/open-platform-model/` is pinned to a Go pseudo-version, or to a version that is not an existing tag of that module's repository;
- a published fixture's `cue.mod/module.cue` (`test/fixtures/modules/*`, `test/fixtures/modulepackages/*`) pins a dependency version containing `-0.dev.`;
- any `cue.mod/local-module.cue` is tracked by git.

Each failure SHALL name the offending file and pin. On pull requests that are not Release PRs the gate step SHALL be skipped. The same check SHALL be runnable locally as one task. The job carrying the gate SHALL report a check name no other workflow job in the repo uses (`Lint`), so a ruleset can require exactly that check.

#### Scenario: Replace directive blocks the release
- **WHEN** a Release PR head has `replace github.com/open-platform-model/library => ../library` in `go.mod`
- **THEN** the gate step fails and its output names the replace directive

#### Scenario: Pseudo-version blocks the release
- **WHEN** a Release PR head pins `github.com/open-platform-model/library v1.0.0-beta.2.0.20261001120000-abcdef123456`
- **THEN** the gate step fails and names the module and version

#### Scenario: Untagged version blocks the release
- **WHEN** a Release PR head pins `github.com/open-platform-model/library v1.0.0-beta.9` and the library repository has no tag `v1.0.0-beta.9`
- **THEN** the gate step fails and names the module and version

#### Scenario: Dev CUE pin in a published fixture blocks the release
- **WHEN** a Release PR head has `test/fixtures/modules/hello/cue.mod/module.cue` pinning `v: "v2.0.0-0.dev.20261001"`
- **THEN** the gate step fails and names the file

#### Scenario: Tracked local-module.cue blocks the release
- **WHEN** a Release PR head tracks `test/fixtures/modules/hello/cue.mod/local-module.cue`
- **THEN** the gate step fails and names the file

#### Scenario: Clean release PR passes
- **WHEN** a Release PR head has no replace directive, pins `github.com/open-platform-model/library` to an existing tag, and no published fixture carries a dev pin or a tracked `local-module.cue`
- **THEN** the gate step passes

#### Scenario: The gate's check name is unique
- **WHEN** the `name:` of every job under `.github/workflows/` is listed
- **THEN** the job carrying the release-pin gate reports `Lint`, and no other job reports that name

#### Scenario: Ordinary PRs skip the gate
- **WHEN** a pull request's head branch does not start with `release-please--`
- **THEN** the gate step is skipped and the job's result depends only on its other steps

### Requirement: Dependabot leaves OPM Go modules to the release cascade
The `gomod` entry of `.github/dependabot.yml` SHALL ignore every dependency matching `github.com/open-platform-model/*`. Those pins move only through the release cascade, which titles a shipped bump `fix(deps)` so it releases (workspace RELEASING.md, section "Pin classes"). The `github-actions` entry SHALL ignore `open-platform-model/.github*`: the cascade references move together, one `.github` SHA for the repo, only through a `ci(deps): pin the cascade to .github <sha7>` pull request (Phase 3 wiring contract (version 3.1) §2.4, §10.1 item 7). Third-party Go modules and other GitHub Actions SHALL keep their Dependabot updates.

#### Scenario: Library release opens no Dependabot PR
- **WHEN** library publishes a new tag
- **THEN** Dependabot opens no PR bumping `github.com/open-platform-model/library` in opm-operator

#### Scenario: Third-party bumps continue
- **WHEN** a new `k8s.io/api` release exists
- **THEN** Dependabot still proposes the grouped Kubernetes bump

#### Scenario: A .github main commit opens no Dependabot PR
- **WHEN** a commit lands on `open-platform-model/.github` `main` after the repo's pinned SHA
- **THEN** Dependabot opens no PR moving any `open-platform-model/.github` reference, and the five references keep one SHA

### Requirement: The opm CLI version is pinned in one file
The opm CLI release that CI and the release workflow install SHALL be declared once, in a repo-root file `.opm-cli-version` holding exactly one line: the cli tag (for example `v1.0.0-beta.4`). Every workflow step that installs the opm CLI SHALL read the version from that file and SHALL fail before installing when the file is missing or its content is not a `v`-prefixed SemVer tag. No workflow file SHALL carry an opm CLI version literal. The pin is a release-tool pin: moving it is a `ci(deps)` commit and cuts no release (workspace RELEASING.md, section "Pin classes").

#### Scenario: Every install reads the file
- **WHEN** the workflows under `.github/workflows/` are searched for `cli/cmd/opm@v`
- **THEN** there is no match, and each `go install` of `github.com/open-platform-model/cli/cmd/opm` takes its version from `.opm-cli-version`

#### Scenario: Malformed pin fails fast
- **WHEN** `.opm-cli-version` contains `latest` or is empty
- **THEN** the install step fails with a message naming `.opm-cli-version`, before any `go install` runs

#### Scenario: Release job installs the pin of the release tag
- **WHEN** the release workflow publishes example modules for tag `vX`
- **THEN** it installs the opm CLI version recorded in `.opm-cli-version` at tag `vX`

### Requirement: Forced version via the release-as config key
A forced version SHALL be a `release-as` value on package `"."` of `release-please-config.json`, set by a normal pull request and removed by the next pull request once the GitHub Release for that version is cut. While the value stays, release-please proposes exactly that version on every Release PR, so a value left in place after its release re-proposes an already-published version. The key is the required mechanism to cross a boundary the automated bump cannot reach (changing the prerelease label of a suffixed version, for example alpha to beta) and MAY pin an exact version at any crossing. A `Release-As:` commit footer SHALL NOT be relied on: the squash commit on `main` carries only the PR title (`squash_merge_commit_message: BLANK`, workspace RELEASING.md, section "Owner settings"), so a footer in the PR body or a branch commit never reaches `main`. GA needs no forced version: `prerelease: false` plus a visible carrier commit is enough. A `release-as` value sets the proposed version but does not by itself open a Release PR; one opens only once a commit of a visible type has landed since the last release.

#### Scenario: Forced version set by a normal PR
- **WHEN** a pull request that sets `"release-as": "1.0.0-beta.1"` on package `"."` merges while the manifest reads `1.0.0-alpha.22`, and a commit of a visible type has landed since the last release
- **THEN** the Release PR proposes `1.0.0-beta.1` regardless of the commit types since the last release

#### Scenario: The next PR removes the value once the release is cut
- **WHEN** the GitHub Release `v1.0.0-beta.1` has been cut while `release-please-config.json` still sets `"release-as": "1.0.0-beta.1"`
- **THEN** the next pull request removes the key, and after it merges the next releasable commit proposes `1.0.0-beta.2` per "Version bump determination per release line"

#### Scenario: Config carries release-as only while a forced release is pending
- **WHEN** `release-please-config.json` is inspected on `main` and the version its `release-as` would force is already a release tag and the pull request after that release's cut has merged, or no forced version is pending
- **THEN** it SHALL contain no `release-as` key at the root or in any package

#### Scenario: A footer forces nothing
- **WHEN** a pull request whose body or branch commits carry `Release-As: 1.1.0-beta.1` is squash-merged under the Merge settings of workspace RELEASING.md "Owner settings", and `release-please-config.json` carries no `release-as`
- **THEN** the squash commit on `main` is the PR title alone, and the next Release PR proposes the automated bump, not `1.1.0-beta.1`

#### Scenario: A hidden-type carrier waits for a visible commit
- **WHEN** the only commit since the last release is the `ci:` pull request that set `release-as`
- **THEN** release-please opens no Release PR; the forced version is proposed once a commit of a visible type lands

### Requirement: The Release PR rewrites the install page's operator version

`release-please-config.json` SHALL list `docs/site/start/install-the-operator.md` under the root package's `extra-files`, and every passage of that page that names the operator's version SHALL sit inside an `x-release-please-start-version` ... `x-release-please-end` block of HTML comment markers, each on its own line outside code fences and at the content indentation of its enclosing block (three spaces inside an ordered-list item), so the page renders as it would without them. A block SHALL name no version other than the operator's. A test in `internal/version` SHALL fail when the page has no block, when a block is not closed, or when a version inside a block differs from the `Version` constant.

#### Scenario: A Release PR moves the page with the constant

- **WHEN** release-please opens the Release PR for `1.0.0-beta.6`
- **THEN** the PR rewrites `Version` in `internal/version/version.go` and every version inside the install page's blocks to `1.0.0-beta.6`, and the release's docs bundle names `v1.0.0-beta.6` in its install commands

#### Scenario: Another project's version inside a block is refused

- **WHEN** a commit puts a core or catalog version inside one of the install page's blocks
- **THEN** `task dev:test` fails, naming the version and the expected operator version

#### Scenario: A detached marker is refused

- **WHEN** an edit removes every block from the install page, or leaves a start marker without its end
- **THEN** `task dev:test` fails, naming the page

### Requirement: Release notifies downstream after it is published
The release workflow SHALL contain a job `notify-downstream`, owned by this repo, that needs `release-please` and `publish-release`, runs on `ubuntu-latest`, declares `environment: cascade`, has a 20-minute timeout, grants `contents: read` and no other permission, and has exactly one step: the action `open-platform-model/.github/.github/actions/cascade-notify` at the repo's pinned `.github` `main` SHA, with the inputs `tag: ${{ needs.release-please.outputs.tag_name }}`, `client-id: ${{ vars.CASCADE_APP_CLIENT_ID }}` and `private-key: ${{ secrets.CASCADE_APP_PRIVATE_KEY }}` and no other input. It SHALL run only when `releases_created` is `true`, `publish-release` succeeded, and the repo variable `CASCADE_NOTIFY` is not `off`. It SHALL NOT wait for, or be skipped by, `publish-docs`. The key SHALL be read only in the caller-owned `notify-downstream` or `publish` job, which declares `environment: cascade` and passes `secrets.CASCADE_APP_PRIVATE_KEY` only as the `private-key` input of the SHA-pinned cascade action; that job SHALL have no checkout or `run:` of its own, and no `env:`, `container:` or `services:`; no reusable call SHALL pass `secrets:` or `secrets: inherit`. The action mints the App token and SHALL dispatch `upstream-released` only to `cli` (Phase 3 wiring contract (version 3.1) §3.1, §4.3, §4.6, §10.1 item 9; workspace RELEASING.md, "Notify after publish").

#### Scenario: Published release notifies the cli
- **WHEN** the release run for `v1.0.0-beta.9` publishes the draft with `install.yaml` attached
- **THEN** `notify-downstream` runs after `publish-release` in the `cascade` Environment, and the cli receives a `repository_dispatch` of type `upstream-released` with source `opm-operator` and tag `v1.0.0-beta.9`

#### Scenario: Release left a draft does not notify
- **WHEN** `publish-examples` fails, so `publish-release` does not run and the release stays a draft
- **THEN** `notify-downstream` does not run, and "Re-run failed jobs" that completes and publishes the release also runs `notify-downstream`

#### Scenario: Failed notify is recovered without republishing
- **WHEN** `publish-release` succeeded and `notify-downstream` failed, for example because the App installation was suspended
- **THEN** "Re-run failed jobs" re-runs only `notify-downstream`, and the release stays as it is

#### Scenario: Notify switched off
- **WHEN** the repo variable `CASCADE_NOTIFY` is `off` and a release is published
- **THEN** `notify-downstream` is skipped, the release is still published, and the cli's daily sweep picks the release up

#### Scenario: Docs failure does not hold notify
- **WHEN** `publish-docs` fails and `publish-release` succeeds
- **THEN** `notify-downstream` still runs

#### Scenario: Run without a release
- **WHEN** a push to `main` cuts no release
- **THEN** `notify-downstream` is skipped

#### Scenario: A step added beside the action is refused
- **WHEN** a pull request adds a checkout or a `run:` step to `notify-downstream`
- **THEN** the `Lint` job's "Verify the cascade wiring" step fails naming `release.yml:notify-downstream step count`
