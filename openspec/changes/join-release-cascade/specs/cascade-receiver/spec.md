## Purpose

The opm-operator side of the release cascade wiring: the `deps-cascade.yml` receiver that runs `task -x deps:cascade` through the shared receive workflow on every upstream release, a daily sweep and by hand, and publishes through a caller-owned job; the `cascade-gates.yml` caller that keeps the G2 and G3 commit statuses on every pull request; and the one-SHA pin of the cascade code, checked on every pull request.

## ADDED Requirements

### Requirement: The receiver runs the shared receive workflow on dispatch, sweep and demand
The repo SHALL have `.github/workflows/deps-cascade.yml`, named `Deps cascade`, triggered by `repository_dispatch` of type `upstream-released`, by the schedule `47 5 * * *`, and by `workflow_dispatch` with the boolean inputs `dry_run` and `gates_only` (both default `false`). Its top-level keys SHALL be exactly `name`, `on`, `permissions`, `concurrency` and `jobs`, with `permissions: {}`. Its job `cascade` SHALL call `open-platform-model/.github/.github/workflows/cascade-receive.yml` at the repo's pinned `.github` SHA, granting `contents: read`, `pull-requests: read` and `statuses: write` and nothing else, passing no `secrets`, and passing `setup-go: true`, the dry-run expression `${{ inputs.dry_run == true || vars.CASCADE_DRY_RUN != 'false' }}`, `gates-only`, and `g2-mode` and `g3-mode` from the repo variables `CASCADE_G2_MODE` and `CASCADE_G3_MODE`, each defaulting to `warn`. Payload validation, the task run and the plan belong to the called workflow, whose jobs hold no secret (Phase 3 wiring contract (version 3.1) §5, §5.1, §5.2, §6).

#### Scenario: A library release reaches the operator
- **WHEN** library publishes `v1.0.0-beta.5`, its notify job dispatches `upstream-released` to opm-operator, and `CASCADE_DRY_RUN` is `false`
- **THEN** a `Deps cascade` run starts on `main`, runs `task -x deps:cascade`, and the `deps/cascade` PR names library `v1.0.0-beta.5`

#### Scenario: Daily sweep
- **WHEN** no dispatch arrived and the schedule fires at 05:47 UTC
- **THEN** a `Deps cascade` run starts with no payload and behaves as a sweep

#### Scenario: Workflow grants nothing at the top
- **WHEN** `deps-cascade.yml` is read
- **THEN** the top-level `permissions` is empty, the `cascade` job grants only `contents: read`, `pull-requests: read` and `statuses: write`, and no job that calls into `open-platform-model/.github` has a `secrets` key

### Requirement: The receiver publishes only through a caller-owned job running the pinned cascade-publish action
`deps-cascade.yml` SHALL have a job `publish` that needs `cascade`, runs on `ubuntu-latest`, declares `environment: cascade`, grants `contents: read` and `pull-requests: read` and nothing else, and has exactly one step: the action `open-platform-model/.github/.github/actions/cascade-publish` at the repo's pinned `.github` SHA, with the inputs `dry-run` (the same expression as the `cascade` job), `labels-managed: false`, `client-id: ${{ vars.CASCADE_APP_CLIENT_ID }}` and `private-key: ${{ secrets.CASCADE_APP_PRIVATE_KEY }}`, and no other input. The key SHALL be read only in the caller-owned `notify-downstream` or `publish` job, which declares `environment: cascade` and passes `secrets.CASCADE_APP_PRIVATE_KEY` only as the `private-key` input of the SHA-pinned cascade action; that job SHALL have no checkout or `run:` of its own, and no `env:`, `container:` or `services:` (the action checks out the repo but never runs it); no reusable call SHALL pass `secrets:` or `secrets: inherit` (Phase 3 wiring contract (version 3.1) §2.2, §5.2, §10.1 item 9).

#### Scenario: The plan becomes a push
- **WHEN** `CASCADE_DRY_RUN` is `false`, the run is on `main`, `compute` succeeded and planned `push`
- **THEN** `publish` runs in the `cascade` Environment, and the `cascade-publish` action verifies the plan, mints the App token and pushes `deps/cascade`

#### Scenario: An env on the publish job is refused
- **WHEN** a pull request adds `env: {BASH_ENV: repo/x.sh}` to the `publish` job
- **THEN** the `Lint` job's "Verify the cascade wiring" step fails naming `deps-cascade.yml:publish keys`

#### Scenario: Another job reads the key
- **WHEN** a pull request adds `secrets.CASCADE_APP_PRIVATE_KEY` to any other job or declares `environment: cascade` on any other job
- **THEN** the wiring check fails naming the unexpected reader or Environment job

### Requirement: The receiver never publishes unless CASCADE_DRY_RUN is exactly false
The receiver SHALL plan a dry run, and SHALL NOT start the `publish` job, unless the repo variable `CASCADE_DRY_RUN` is exactly `false`, the `dry_run` input is not `true`, and the run is on `main`. Unset, deleted or any other value SHALL mean a dry run. The `cascade-publish` action's required `dry-run` input SHALL receive the same expression as the `cascade` job, so the action itself publishes nothing unless the value is exactly `false`, whatever the `publish` job's `if:` says. In a dry run nothing is pushed, and no PR, label or comment is written; the gate statuses are still posted (Phase 3 wiring contract (version 3.1) §5, §9.1; workspace RELEASING.md, "Stop switches").

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

### Requirement: Gates-only and branch runs never displace a pending real run
The receiver SHALL declare `cancel-in-progress: false` and the concurrency group `deps-cascade` for real runs on `main` (dispatch, schedule, manual), `deps-cascade-gates` for gates-only runs on `main`, and `deps-cascade-<ref>` for runs from any other ref (Phase 3 wiring contract (version 3.1) §5; workspace RELEASING.md, "Concurrency").

#### Scenario: Burst of releases
- **WHEN** three dispatches arrive while a real run is active
- **THEN** one run stays active and one is pending in `deps-cascade`, and the pending run resolves the newest versions itself

#### Scenario: Gates-only run during a pending real run
- **WHEN** a release PR head moves while a real run is pending
- **THEN** the gates-only run queues in `deps-cascade-gates`, and the pending real run stays pending

### Requirement: Cascade gate statuses appear on every pull request
The repo SHALL have `.github/workflows/cascade-gates.yml`, named `Cascade gates`, triggered by `pull_request_target` with types `opened`, `reopened` and `synchronize`. It SHALL declare `permissions: {}` at the top, use the concurrency group `cascade-gates-<PR number>` with `cancel-in-progress: true`, and have a single job, `gates`, that calls `open-platform-model/.github/.github/workflows/cascade-gates.yml` at the repo's pinned `.github` SHA. The job SHALL grant `statuses: write` and `actions: write` and nothing else, and SHALL pass `g2-mode` and `g3-mode` from `CASCADE_G2_MODE` and `CASCADE_G3_MODE` (default `warn`). The workflow SHALL contain no step of its own and SHALL check out no pull request code. Neither `cascade/freshness` nor `cascade/settled` SHALL be a required check before Phase 5 (Phase 3 wiring contract (version 3.1) §8.3, §8.4; workspace RELEASING.md, "Gates").

#### Scenario: Ordinary pull request
- **WHEN** a PR from a branch other than `release-please--*` is opened
- **THEN** `cascade/freshness` and `cascade/settled` appear on its head as `success` with `n/a: not a release PR`

#### Scenario: Release PR head moves
- **WHEN** release-please pushes a new head to the open release PR
- **THEN** the caller dispatches `deps-cascade.yml` with `gates_only=true`, and that run posts both statuses on the new head without running the cascade on `main`

#### Scenario: Behind shipped pin in warn mode
- **WHEN** the release PR's `go.mod` pins library `v1.0.0-beta.4` while `v1.0.0-beta.5` is published, and `CASCADE_G2_MODE` is unset
- **THEN** `cascade/freshness` is `success` with a description starting `WARN:` that names library, and the release PR can still merge

### Requirement: The cascade references are pinned to one .github main SHA and checked in the Lint job
Every reference to the cascade code SHALL name one full 40-character SHA of a commit on `open-platform-model/.github` `main`, never a branch or a tag, followed by the comment `# .github main`: the `cascade-notify` step in `release.yml`, the `cascade-receive.yml` call and the `cascade-publish` step in `deps-cascade.yml`, the `cascade-gates.yml` call, and the `ref:` of the `open-platform-model/.github` checkout in `cascade-task.yml`. All five SHALL carry the same SHA, which moves only through a `ci(deps): pin the cascade to .github <sha7>` pull request. `cascade-task.yml` SHALL fail, not skip, when the resolver is missing at the pinned commit. `task cascade:wiring:check` (`.tasks/cascade/wiring-check.sh`) SHALL run as the step "Verify the cascade wiring" of the required `Lint` job on every pull request and SHALL fail when: a key-holding job (`notify-downstream`, `publish`) differs from its contract shape in its keys, step keys, `with` keys, Environment, permissions, step count, action reference, key inputs or a `runs-on` other than `ubuntu-latest`; `release.yml`'s workflow-level `env` is not a map or has a key other than `REGISTRY`, `IMAGE_NAME` and `CUE_VERSION`; `deps-cascade.yml`'s top-level keys, concurrency group, `publish` condition or either `dry-run` value differ from the contract; any other workflow value reads the key or any other job declares `environment: cascade`; a call into `.github` has a `secrets` key; or the set of `.github` references, their SHA or their comment differs. It guards against mistakes; review and the `main` ruleset guard against a deliberate edit (Phase 3 wiring contract (version 3.1) §2.4, §10.1 items 2, 3 and 6; the supervisor's addendum).

#### Scenario: One reference moves alone
- **WHEN** a pull request changes only the `cascade-gates.yml` call to another SHA
- **THEN** the "Verify the cascade wiring" step fails with `one .github SHA` naming both SHAs, and the `Lint` check is red

#### Scenario: A branch ref
- **WHEN** a pull request names `cascade-notify@main`
- **THEN** the wiring check fails on the notify `uses` and on the pin comment

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
