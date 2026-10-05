## MODIFIED Requirements

### Requirement: The receiver publishes only through a caller-owned job running the pinned cascade-publish action
`deps-cascade.yml` SHALL have a job `publish` that needs `cascade`, runs on `ubuntu-latest`, declares `environment: cascade`, grants `contents: read` and `pull-requests: read` and nothing else, and has exactly one step: the action `open-platform-model/.github/.github/actions/cascade-publish` at the repo's pinned `.github` SHA, with the inputs `dry-run` (the same expression as the `cascade` job), `gates-only: ${{ inputs.gates_only == true }}`, `labels-managed: false`, `client-id: ${{ vars.CASCADE_APP_CLIENT_ID }}` and `private-key: ${{ secrets.CASCADE_APP_PRIVATE_KEY }}`, and no other input. The key SHALL be read only in the caller-owned `notify-downstream` or `publish` job, which declares `environment: cascade` and passes `secrets.CASCADE_APP_PRIVATE_KEY` only as the `private-key` input of the SHA-pinned cascade action; that job SHALL have no checkout or `run:` of its own, and no `env:`, `container:` or `services:` (the action checks out the repo but never runs it); no reusable call SHALL pass `secrets:` or `secrets: inherit` (Phase 3 wiring contract (version 3.1) §2.2, §5.2, §10.1 item 9; `.github` README at `6938f8e`, "Receiver caller").

#### Scenario: The plan becomes a push
- **WHEN** `CASCADE_DRY_RUN` is `false`, the run is on `main`, `compute` succeeded and planned `push`
- **THEN** `publish` runs in the `cascade` Environment, and the `cascade-publish` action verifies the plan, mints the App token and pushes `deps/cascade`

#### Scenario: An env on the publish job is refused
- **WHEN** a pull request adds `env: {BASH_ENV: repo/x.sh}` to the `publish` job
- **THEN** the `Lint` job's "Verify the cascade wiring" step fails naming `deps-cascade.yml:publish keys`

#### Scenario: Another job reads the key
- **WHEN** a pull request adds `secrets.CASCADE_APP_PRIVATE_KEY` to any other job or declares `environment: cascade` on any other job
- **THEN** the wiring check fails naming the unexpected reader or Environment job

#### Scenario: A gates-only run reaches the action
- **WHEN** the `publish` job's `if:` were bypassed on a run dispatched with `gates_only=true`
- **THEN** `cascade-publish` receives `gates-only: true` and fails before it mints the App token

### Requirement: The receiver never publishes unless CASCADE_DRY_RUN is exactly false
The receiver SHALL plan a dry run, and SHALL NOT start the `publish` job, unless the repo variable `CASCADE_DRY_RUN` is exactly `false`, the `dry_run` input is not `true`, the `gates_only` input is not `true`, and the run is on `main`; the `publish` job's `if:` SHALL carry the clause `inputs.gates_only != true`. Unset, deleted or any other value SHALL mean a dry run. The `cascade-publish` action's required `dry-run` input SHALL receive the same expression as the `cascade` job, so the action itself publishes nothing unless the value is exactly `false`, whatever the `publish` job's `if:` says. In a dry run nothing is pushed, and no PR, label or comment is written; the gate statuses are still posted (Phase 3 wiring contract (version 3.1) §5, §9.1; workspace RELEASING.md, "Stop switches").

#### Scenario: Variable unset
- **WHEN** `CASCADE_DRY_RUN` does not exist and a dispatch arrives
- **THEN** the run computes the diff, writes it to the job summary with "DRY RUN: nothing was pushed", `Publish` is skipped, and `deps/cascade` is not created or moved

#### Scenario: Live repo, manual dry run
- **WHEN** `CASCADE_DRY_RUN` is `false` and someone runs `gh workflow run deps-cascade.yml -f dry_run=true`
- **THEN** that run pushes nothing

#### Scenario: Live repo
- **WHEN** `CASCADE_DRY_RUN` is `false` and a dispatch arrives that moves a pin
- **THEN** the run pushes `deps/cascade` and creates or updates the cascade PR

#### Scenario: A mistyped publish condition
- **WHEN** a pull request changes the `publish` job's `if:` or replaces either `dry-run` value with a literal
- **THEN** the wiring check fails naming the changed value, and even if it merged, the action would publish nothing while `CASCADE_DRY_RUN` is not `false`

#### Scenario: A gates-only run on a live repo
- **WHEN** `CASCADE_DRY_RUN` is `false` and `cascade-gates.yml` dispatches `deps-cascade.yml` with `gates_only=true`
- **THEN** the run posts the gate statuses, `Publish` is skipped, and nothing is pushed

### Requirement: The cascade references are pinned to one .github main SHA and checked in the Lint job
Every reference to the cascade code SHALL name one full 40-character SHA of a commit on `open-platform-model/.github` `main`, never a branch or a tag, followed by the comment `# .github main`: the `cascade-notify` step in `release.yml`, the `cascade-receive.yml` call and the `cascade-publish` step in `deps-cascade.yml`, the `cascade-gates.yml` call, the `ref:` of the `open-platform-model/.github` checkout in `cascade-task.yml`, and the `ref:` of the resolver checkout in `module-deps.yml` (the operator module's own cascade PR, see `deps-cascade`). All six SHALL carry the same SHA, which moves only through a `ci(deps): pin the cascade to .github <sha7>` pull request. `cascade-task.yml` SHALL fail, not skip, when the resolver is missing at the pinned commit. `.tasks/cascade/wiring-check.sh` SHALL be byte-identical to `.github/scripts/cascade/wiring-check.sh` at the pinned SHA and SHALL move only with the pin; this repo's values SHALL live in `.tasks/cascade/wiring-check.yaml` (`receiver: true`, `env-allow` `REGISTRY`, `IMAGE_NAME` and `CUE_VERSION`, `ci` `lint.yml` job `lint`, the notify `needs`, `if` and `tag` below, `labels-managed: false`, `extra-references` naming the `module-deps.yml` resolver, and `publish-workflows` naming every workflow that publishes: `release.yml`, `publish-fixtures.yml`, `docs.yml`, `image-pr.yml`, `test-e2e.yml`, `module-image.yml` and `module-deps.yml`). The required `Lint` job SHALL run it on every pull request as the step "Verify the cascade wiring", exactly `bash .tasks/cascade/wiring-check.sh --pin-on-main` with only `GH_TOKEN: ${{ github.token }}` in its `env`, preceded only by SHA-pinned actions from other repos; `task cascade:wiring:check` runs it offline. It SHALL fail when: a key-holding job (`notify-downstream`, `publish`) differs from its contract shape in its keys, step keys, `with` keys, Environment, permissions, step count, action reference, key inputs or a `runs-on` other than `ubuntu-latest`; the `notify-downstream` job's `needs` differs from `[release-please, publish-release]`, or its `if` from `needs.release-please.outputs.release_created == 'true' && vars.CASCADE_NOTIFY != 'off'` (contract §4.6 prints `releases_created`, which a module-only release also sets), or its `tag` input or `timeout-minutes` (20) from contract §4.6; the `publish` job's `needs` differs from `cascade`, its `if:` or `gates-only` input from the receiver caller shape, or its `timeout-minutes` from 15; `release.yml`'s workflow-level `env` is not a map or has a key other than `REGISTRY`, `IMAGE_NAME` and `CUE_VERSION`; `deps-cascade.yml`'s top-level keys, concurrency group, `publish` condition or either `dry-run` value differ from the contract; any other workflow value reads the key or any other job declares `environment: cascade`; a job reads `RELEASE_APP_PRIVATE_KEY` without declaring exactly `environment: release`, or a job declares `release` without reading it; a workflow listed in `publish-workflows` uses an Actions cache; a `.github` checkout is not `actions/checkout@<SHA>` with exactly `repository`, `ref`, `path` and `persist-credentials: false`; the `Lint` workflow or job `env` names anything outside `CUE_*`, `OPM_*`, `REGISTRY` and `IMAGE_NAME`, or the job has a `container` or `services`; a call into `.github` has a `secrets` key; or the set of `.github` references, their SHA or their comment differs. With `--pin-on-main` it SHALL also fail when the SHA is not on `.github` `main` or the running copy differs from the file at the SHA. It guards against mistakes, not a deliberate edit: code-owner review is the guard against that, and the `main` ruleset does not require review while OPM is in beta (no approvals, workspace PR 29) (Phase 3 wiring contract (version 3.1) §2.4, §10.1 items 2, 3 and 6; the supervisor's addendum; `.github` README at `6938f8e`, "The wiring check").

#### Scenario: One reference moves alone
- **WHEN** a pull request changes only the `cascade-gates.yml` call to another SHA
- **THEN** the "Verify the cascade wiring" step fails with `one .github SHA` naming both SHAs, and the `Lint` check is red

#### Scenario: A branch ref
- **WHEN** a pull request names `cascade-notify@main`
- **THEN** the wiring check fails on the notify `uses` and on `one .github SHA`

#### Scenario: A startup-code variable in release.yml
- **WHEN** a pull request adds `BASH_ENV` or any key other than `REGISTRY`, `IMAGE_NAME` and `CUE_VERSION` to `release.yml`'s workflow-level `env`
- **THEN** the wiring check fails naming the key

#### Scenario: A self-hosted runner for a key job
- **WHEN** a pull request sets `runs-on: self-hosted` on `notify-downstream`
- **THEN** the wiring check fails naming `release.yml:notify-downstream runs-on`

#### Scenario: The resolver is missing at the pin
- **WHEN** `cascade-task.yml` runs and the pinned `.github` commit has no `cascade-resolve.sh`
- **THEN** the run fails with `no cascade resolver at the pinned .github commit` instead of skipping S5

#### Scenario: The wired tree passes
- **WHEN** the `Lint` job runs on a pull request that leaves the cascade wiring as the contract shapes it
- **THEN** the step prints `cascade wiring: ok, .github <SHA> (.github main)`

#### Scenario: The module's resolver checkout follows the pin
- **WHEN** a pull request moves the five other `.github` references to a new SHA and leaves `module-deps.yml`'s resolver checkout behind
- **THEN** the wiring check fails on `one .github SHA`

#### Scenario: A copy left behind at a pin bump
- **WHEN** a pull request moves every reference to a new SHA but keeps the old `wiring-check.sh`
- **THEN** the "Verify the cascade wiring" step fails with `differs from` naming the file at the new SHA

#### Scenario: A run step before the wiring step
- **WHEN** a pull request adds a `run:` step to the `Lint` job above "Verify the cascade wiring"
- **THEN** the wiring check fails naming the step that is not a pinned action

#### Scenario: A cache in a publishing workflow
- **WHEN** a pull request drops `cache: false` from a `setup-go` step in `test-e2e.yml`
- **THEN** the wiring check fails naming `test-e2e.yml cache use`
