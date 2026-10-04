## Why

Phase 3 of the release cascade (workspace RELEASING.md, section "Rollout and changes", the
Phase 3 row) wires each repo into the shared notify and receive workflows that `.github`
`add-release-cascade-workflows` adds. opm-operator has both sides:

- **It is an upstream.** The cli pins the operator through the `release` resolver kind, which
  needs a published, non-draft release with the `install.yaml` asset (workspace RELEASING.md,
  "Notify after publish"). Today nothing tells the cli an operator release exists; the cli bumps
  by hand.
- **It is a downstream.** `task -x deps:cascade` (archived change `add-deps-cascade-task`)
  already moves library, the opm catalog and core, and `.opm-cli-version`. Nothing runs it
  except a human, so a library or catalog release reaches the operator only when someone
  remembers.

The design is fixed by workspace RELEASING.md (sections "The cascade", "Gates", "Stop switches",
"Owner settings", "Rollout and changes") and by the Phase 3 wiring contract, version 2
(`/var/home/emil/.cache/claude-tmp/claude-1000/-var-home-emil-dev-open-platform-model/2ee0ca8e-268c-4b20-8bd9-b5e4f0d96717/scratchpad/p3-wiring-contract.md`,
cited as "wiring contract §N"). The contract binds this change and its siblings in core,
catalog_opm, library and cli to the three reusable workflows in `.github`. This repo writes only
thin callers; every script, validation and push lives in `.github`.

## What Changes

- **Notify job in `.github/workflows/release.yml`** (wiring contract §4.3, §4.5). A new job
  `notify-downstream` calls `open-platform-model/.github/.github/workflows/cascade-notify.yml@main`
  with the release tag.
  - It runs after `publish-release` (`release.yml:337-360`), the job that publishes the draft
    once `install.yaml` and the example assets are attached. Its `needs` are
    `[release-please, publish-release]`, and its `if` is
    `needs.release-please.outputs.releases_created == 'true' && vars.CASCADE_NOTIFY != 'off'`.
  - It grants only `contents: read`. The reusable job holds the `cascade` Environment and mints
    the App token scoped to the cli, the only repo opm-operator dispatches to (wiring contract
    §3.1).
  - `publish-docs` does not gate it.
- **Receiver `.github/workflows/deps-cascade.yml`** (wiring contract §5, §5.1). It triggers on
  `repository_dispatch` (`upstream-released`), a daily schedule `47 5 * * *`, and
  `workflow_dispatch` with the `dry_run` and `gates_only` inputs. It calls
  `cascade-receive.yml@main` with `setup-go: true` and `labels-managed: false`, and it passes
  `dry-run`, `gates-only`, `g2-mode` and `g3-mode` from the repo variables. `permissions: {}` sits
  at the top, the per-job grant is `contents: read`, `pull-requests: read` and `statuses: write`,
  and the §5 concurrency expression applies.
- **Per-PR gates caller `.github/workflows/cascade-gates.yml`** (wiring contract §8.3). It runs on
  `pull_request_target` (`opened`, `reopened`, `synchronize`) and calls
  `cascade-gates.yml@main`. It posts `cascade/freshness` and `cascade/settled` on every PR, and
  on a release PR dispatches a gates-only receiver run. It checks nothing out.
- **The release-automation spec** says that `publish-release` is the publish point, no longer
  the last job, and adds the notify requirement.
- **`AGENTS.md`** gains one bullet naming the receiver, the gates caller, the notify job and
  their repo variables (`CASCADE_DRY_RUN`, `CASCADE_NOTIFY`, `CASCADE_G2_MODE`,
  `CASCADE_G3_MODE`).

The receiver lands dry. The supervisor sets the repo variable `CASCADE_DRY_RUN=true` before the
PR merges (wiring contract §1, §9.1). The receiver goes live only when the variable is exactly
`false`, which is Phase 4.

Release class: none. Every commit is `ci` or `docs`, both hidden sections, so this change cuts no
operator release. After GA it would still be no release, because it only adds CI wiring. No API
type, CRD, controller or reconcile phase changes.

## Depends on / gates

- **`.github` `add-release-cascade-workflows` merged first** (wiring contract §1; workspace
  RELEASING.md, "Changes", the Phase 3 rows). Every caller here uses `@main`, so this PR must not
  merge before the three reusable workflows exist on `.github` `main`. A missing called workflow
  makes the run fail at startup. For `release.yml` that would stop every operator release.
- **The sandbox results, recorded in A's `design.md`:**
  - **E1 and E1b** show that `environment: cascade` inside a called workflow reads this repo's
    Environment and that the `main`-only policy refuses a branch.
  - **E6** shows whether `sha_pinning_required: true`, which opm-operator has and no other
    cascade repo has, refuses a `uses: …/cascade-*.yml@main` call.
  - If E1 fails, wiring contract §13.1 replaces notify and publish with composite actions, and
    this change's callers change shape. If E6 refuses `@main`, the owner chooses between turning
    the setting off and pinning opm-operator's calls to a `.github` SHA (wiring contract §15
    item 2). Either result stops this change until the supervisor reports the decision. Section
    1 of tasks.md is that check.
- **Depends on opm-operator `add-deps-cascade-task`** (archived:
  `openspec/changes/archive/2026-10-04-add-deps-cascade-task`) for `task -x deps:cascade`,
  `deps:cascade:title` and `deps:cascade:body`. It also depends on `prepare-release-cascade`
  (archived) for `releases_created` and `tag_name` (`release.yml:33-35`).
- **Repo variable `CASCADE_DRY_RUN=true`** is set by the supervisor before merge (owner decision
  22).
- **Phase 3 gate, after merge (supervisor):**
  - `gh workflow run deps-cascade.yml -R open-platform-model/opm-operator -f dry_run=true` shows
    mode `fresh` and action `noop`, or the diff `task -x deps:cascade` gives locally on `main`.
  - `cascade/freshness` and `cascade/settled` appear on the next PR.
- **Not dependent on** the core, catalog_opm, library or cli `join-release-cascade` changes. A
  dispatch to a repo with no receiver returns 204 and starts nothing (wiring contract §1).
- **Gates later:** Phase 4 sets `CASCADE_DRY_RUN=false` here, after one non-noop dry-run summary
  has been read (wiring contract §1, §15 item 5). Phase 5's `require-pin-freshness-gate` makes
  the two contexts required.

## Capabilities

### New Capabilities

- `cascade-receiver`: the opm-operator side of the cascade wiring:
  - the `deps-cascade.yml` receiver: its triggers, the dry run that fails closed, its
    concurrency and its per-repo inputs;
  - the `cascade-gates.yml` per-PR status caller.

### Modified Capabilities

- `release-automation`:
  - "Release published once after every release job" now names `publish-release` as the publish
    point. It is no longer the final job.
  - Adds "Release notifies downstream after it is published".

## Impact

- **New files:** `.github/workflows/deps-cascade.yml` and `.github/workflows/cascade-gates.yml`.
- **Changed files:**
  - `.github/workflows/release.yml`: one job, appended after `publish-release`;
  - `AGENTS.md`: one bullet.
- **Untouched:**
  - `.tasks/cascade/` and `.tasks/deps.yaml`. The receiver runs the task as it is.
  - `lint.yml`, `test.yml`, `cascade-task.yml` and `dependabot.yml`.
  - Every required check. No new job becomes required. The `Lint` context, which the `main`
    ruleset requires, is unchanged.
- **Repo variables** (set by the supervisor or owner, never by this change):
  - `CASCADE_DRY_RUN`: `true` at merge;
  - `CASCADE_NOTIFY`: unset means on;
  - `CASCADE_G2_MODE` and `CASCADE_G3_MODE`: unset means `warn`.
- **Settings it relies on, already in place (Phase 0):**
  - the `cascade` Environment (`main` only) with `CASCADE_APP_PRIVATE_KEY` and
    `CASCADE_APP_CLIENT_ID`;
  - the `opm-cascade` App installed here;
  - `default_workflow_permissions: read`, so every new job declares `permissions:`.
- **Delivery:** one PR titled `ci: join the release cascade` (wiring contract §1). Under the
  `BLANK` squash message only the title reaches `main`, and `ci` cuts no release. The OpenSpec
  archive commit rides that PR, and nothing is pushed to `main` (workspace RELEASING.md, "Rulesets
  on main").
