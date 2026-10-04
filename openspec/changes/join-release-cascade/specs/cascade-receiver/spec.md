## Purpose

The opm-operator side of the release cascade wiring: the `deps-cascade.yml` receiver that runs `task -x deps:cascade` through the shared receive workflow on every upstream release, a daily sweep and by hand, and the `cascade-gates.yml` caller that keeps the G2 and G3 commit statuses on every pull request.

## ADDED Requirements

### Requirement: The receiver runs the shared receive workflow on dispatch, sweep and demand
The repo SHALL have `.github/workflows/deps-cascade.yml`, named `Deps cascade`, triggered by `repository_dispatch` of type `upstream-released`, by the schedule `47 5 * * *`, and by `workflow_dispatch` with the boolean inputs `dry_run` and `gates_only` (both default `false`). It SHALL declare `permissions: {}` at the top and SHALL have a single job, `cascade`, that calls `open-platform-model/.github/.github/workflows/cascade-receive.yml@main`, granting `contents: read`, `pull-requests: read` and `statuses: write` and nothing else, and passing no secrets. The job SHALL pass `setup-go: true` and `labels-managed: false`, and SHALL pass `g2-mode` and `g3-mode` from the repo variables `CASCADE_G2_MODE` and `CASCADE_G3_MODE`, each defaulting to `warn`. The workflow SHALL contain no step of its own: the payload validation, the task run, the push and the PR all belong to the called workflow (Phase 3 wiring contract §5, §5.1, §6).

#### Scenario: A library release reaches the operator
- **WHEN** library publishes `v1.0.0-beta.5`, its notify job dispatches `upstream-released` to opm-operator, and `CASCADE_DRY_RUN` is `false`
- **THEN** a `Deps cascade` run starts on `main`, runs `task -x deps:cascade`, and the `deps/cascade` PR names library `v1.0.0-beta.5`

#### Scenario: Daily sweep
- **WHEN** no dispatch arrived and the schedule fires at 05:47 UTC
- **THEN** a `Deps cascade` run starts with no payload and behaves as a sweep

#### Scenario: Workflow grants nothing at the top
- **WHEN** `deps-cascade.yml` is read
- **THEN** the top-level `permissions` is empty, the `cascade` job grants only `contents: read`, `pull-requests: read` and `statuses: write`, and no `secrets:` key appears

### Requirement: The receiver is a dry run unless CASCADE_DRY_RUN is exactly false
The receiver SHALL pass `dry-run: true` to the called workflow unless the repo variable `CASCADE_DRY_RUN` is exactly `false` and the `dry_run` input is not `true`. Unset, deleted or any other value SHALL mean a dry run. In a dry run nothing is pushed, and no PR, label or comment is written; the gate statuses are still posted (Phase 3 wiring contract §9.1; workspace RELEASING.md, "Stop switches").

#### Scenario: Variable unset
- **WHEN** `CASCADE_DRY_RUN` does not exist and a dispatch arrives
- **THEN** the run computes the diff, writes it to the job summary with "DRY RUN: nothing was pushed", and `deps/cascade` is not created or moved

#### Scenario: Live repo, manual dry run
- **WHEN** `CASCADE_DRY_RUN` is `false` and someone runs `gh workflow run deps-cascade.yml -f dry_run=true`
- **THEN** that run pushes nothing

#### Scenario: Live repo
- **WHEN** `CASCADE_DRY_RUN` is `false` and a dispatch arrives that moves a pin
- **THEN** the run pushes `deps/cascade` and creates or updates the cascade PR

### Requirement: Receiver runs never displace a pending real run
The receiver SHALL declare `cancel-in-progress: false` and the concurrency group `deps-cascade` for real runs on `main` (dispatch, schedule, manual), `deps-cascade-gates` for gates-only runs on `main`, and `deps-cascade-<ref>` for runs from any other ref (Phase 3 wiring contract §5; workspace RELEASING.md, "Concurrency").

#### Scenario: Burst of releases
- **WHEN** three dispatches arrive while a real run is active
- **THEN** one run stays active and one is pending in `deps-cascade`, and the pending run resolves the newest versions itself

#### Scenario: Gates-only run during a pending real run
- **WHEN** a release PR head moves while a real run is pending
- **THEN** the gates-only run queues in `deps-cascade-gates`, and the pending real run stays pending

### Requirement: Cascade gate statuses appear on every pull request
The repo SHALL have `.github/workflows/cascade-gates.yml`, named `Cascade gates`, triggered by `pull_request_target` with types `opened`, `reopened` and `synchronize`. It SHALL declare `permissions: {}` at the top, use the concurrency group `cascade-gates-<PR number>` with `cancel-in-progress: true`, and have a single job, `gates`, that calls `open-platform-model/.github/.github/workflows/cascade-gates.yml@main`. The job SHALL grant `statuses: write` and `actions: write` and nothing else, and SHALL pass `g2-mode` and `g3-mode` from `CASCADE_G2_MODE` and `CASCADE_G3_MODE` (default `warn`). The workflow SHALL contain no step of its own and SHALL check out no pull request code. Neither `cascade/freshness` nor `cascade/settled` SHALL be a required check before Phase 5 (Phase 3 wiring contract §8.3, §8.4; workspace RELEASING.md, "Gates").

#### Scenario: Ordinary pull request
- **WHEN** a PR from a branch other than `release-please--*` is opened
- **THEN** `cascade/freshness` and `cascade/settled` appear on its head as `success` with `n/a: not a release PR`

#### Scenario: Release PR head moves
- **WHEN** release-please pushes a new head to the open release PR
- **THEN** the caller dispatches `deps-cascade.yml` with `gates_only=true`, and that run posts both statuses on the new head without running the cascade on `main`

#### Scenario: Behind shipped pin in warn mode
- **WHEN** the release PR's `go.mod` pins library `v1.0.0-beta.4` while `v1.0.0-beta.5` is published, and `CASCADE_G2_MODE` is unset
- **THEN** `cascade/freshness` is `success` with a description starting `WARN:` that names library, and the release PR can still merge
