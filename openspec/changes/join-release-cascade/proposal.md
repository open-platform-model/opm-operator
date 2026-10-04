## Why

Phase 3 of the release cascade (workspace RELEASING.md, section "Rollout and changes", the
Phase 3 row) wires each repo into the cascade code that `.github`
`add-release-cascade-workflows` added (`.github` PR #9, squash
`2376ffae4bfc665f327d51581350dea694c01504` on `main`). opm-operator has both sides:

- **It is an upstream.** The cli pins the operator through the `release` resolver kind, which
  needs a published, non-draft release with the `install.yaml` asset (workspace RELEASING.md,
  "Notify after publish"). Today nothing tells the cli an operator release exists; the cli bumps
  by hand.
- **It is a downstream.** `task -x deps:cascade` (archived change `add-deps-cascade-task`)
  already moves library, the opm catalog and core, and `.opm-cli-version`. Nothing runs it
  except a human, so a library or catalog release reaches the operator only when someone
  remembers.

The design is fixed by workspace RELEASING.md (sections "The cascade", "Pinning the cascade
code", "Gates", "Stop switches", "Moving the cascade pin", "Owner settings", "Rollout and
changes"), the `.github` README ("Cascade workflows", "Pinning and bumps"), and the Phase 3
wiring contract (version 3.1), merged as
`.github/openspec/changes/archive/2026-10-04-add-release-cascade-workflows/contract.md` in the
`.github` repo and cited as "wiring contract §N", together with the supervisor's addendum for
the five join changes (cited as "the addendum"). This repo writes only thin callers: every
script, validation and push lives in `.github`. The contract's §10.1 is this change's
checklist.

This change was first written to contract version 2 (a reusable notify workflow and a
reusable publish job at `@main`). The sandbox cycle refuted that shape (E1: a reusable-workflow
job does not see the caller's Environment secret), and owner decision 24 pins the cascade code
by SHA. This revision rebuilds the change on version 3.1.

## What Changes

- **Notify job in `.github/workflows/release.yml`** (wiring contract §4.3, §4.5, §4.6). The job
  `notify-downstream` is this repo's own job: it declares `environment: cascade` and runs one
  step, the action
  `open-platform-model/.github/.github/actions/cascade-notify@2376ffae4bfc665f327d51581350dea694c01504 # .github main`,
  passing the release tag, `vars.CASCADE_APP_CLIENT_ID` and `secrets.CASCADE_APP_PRIVATE_KEY`
  as its inputs. The action mints the App token, scoped to the cli, the only repo opm-operator
  dispatches to (§3.1).
  - It needs `[release-please, publish-release]`, the job that publishes the draft once
    `install.yaml` and the example assets are attached, and runs only when
    `releases_created == 'true'` and `CASCADE_NOTIFY` is not `off`.
  - It grants only `contents: read`, has no checkout and no `run:` step, and `publish-docs` does
    not gate it.
- **Receiver `.github/workflows/deps-cascade.yml`** (wiring contract §5, §5.1, §5.2). Two jobs:
  - `cascade` calls the reusable `cascade-receive.yml` at the pinned SHA (compute and gates; it
    holds no secret) with `setup-go: true`, the §5 `dry-run` expression, `gates-only`, `g2-mode`
    and `g3-mode`;
  - `publish` is this repo's own job in the `cascade` Environment: it runs the pinned
    `cascade-publish` action with the same `dry-run` expression, `labels-managed: false` and the
    key and client id as inputs. The action publishes only when `dry-run` is exactly `false`.
  - Triggers: `repository_dispatch` (`upstream-released`), a daily schedule `47 5 * * *`, and
    `workflow_dispatch` with `dry_run` and `gates_only`. `permissions: {}` at the top; the §5
    concurrency expression.
- **Per-PR gates caller `.github/workflows/cascade-gates.yml`** (wiring contract §8.3). It runs on
  `pull_request_target` and calls the reusable `cascade-gates.yml` at the pinned SHA. It posts
  `cascade/freshness` and `cascade/settled` on every PR, and on a release PR dispatches a
  gates-only receiver run. It checks nothing out.
- **`cascade-task.yml` checks the resolver out at the pinned SHA** (wiring contract §2.4, §10.1
  item 3), not at `main`, so CI tests the resolver the receiver runs. Its fallback that skipped
  S5 when the resolver was missing goes: at a pinned commit that has the resolver, a missing
  resolver is an error.
- **One `.github` SHA everywhere.** The five cascade references (notify, receive, publish,
  gates, the resolver `ref:`) carry the same full SHA and the comment `# .github main`. A
  `.github` change reaches this repo only through a
  `ci(deps): pin the cascade to .github <sha7>` PR (RELEASING.md, "Moving the cascade pin").
- **The wiring check** (wiring contract §10.1 item 6, the addendum). `.tasks/cascade/wiring-check.sh`,
  run by `task cascade:wiring:check` as a step of the required `Lint` job, refuses any change
  to the key-holding jobs' shape, a second SHA or a missing pin comment, a key read or a
  `cascade` Environment anywhere else, `secrets:` on a call into `.github`, a `release.yml`
  workflow `env` key outside `REGISTRY`, `IMAGE_NAME` and `CUE_VERSION`, and a key-holding job
  whose `runs-on` is not `ubuntu-latest`. It guards against mistakes; review and the `main`
  ruleset guard against a deliberate edit.
- **Dependabot** ignores `open-platform-model/.github*` in its `github-actions` entry, so it never
  moves one reference alone.
- **The release-automation spec** names `publish-release` as the publish point, no longer the
  last job, adds the notify requirement, and extends the Dependabot requirement.
- **`AGENTS.md`** describes the notify job, the receiver, the gates caller, the pin and the wiring
  check.

The receiver lands dry. The repo variable `CASCADE_DRY_RUN` is already `true` (supervisor,
2026-10-04). The receiver goes live only when the variable is exactly `false`, which is Phase 4.

Release class: none. Every commit is `ci`, `docs` or `chore`, all hidden sections, so this
change cuts no operator release. No API type, CRD, controller or reconcile phase changes.

## Depends on / gates

- **`.github` `add-release-cascade-workflows` is merged** (`.github` PR #9, squash
  `2376ffae4bfc665f327d51581350dea694c01504`); `gh api
  repos/open-platform-model/.github/compare/2376ffae4bfc665f327d51581350dea694c01504...main --jq .status`
  prints `identical` (2026-10-04). The sandbox results this change relies on are recorded in A's
  archived `design.md`: E1 failed for a reusable notify or publish job, so both are composite
  actions run by caller-owned jobs (wiring contract §13.1, applied); E1b showed the `main`-only
  Environment refuses a branch run; E6 showed `sha_pinning_required` refuses an action named by
  branch. Owner decision 24 pins every cascade reference by SHA, so opm-operator's
  `sha_pinning_required: true` stays on and refuses nothing.
- **Depends on opm-operator `add-deps-cascade-task`** (archived) for `task -x deps:cascade`,
  `deps:cascade:title` and `deps:cascade:body`, and on `prepare-release-cascade` (archived) for
  `releases_created` and `tag_name`.
- **Repo variable `CASCADE_DRY_RUN=true`**, set by the supervisor (owner decision 22) and read
  back before merge (wiring contract §10.1, pre-merge check step 8).
- **Phase 3 gate, after merge (supervisor):**
  - `gh workflow run deps-cascade.yml -R open-platform-model/opm-operator -f dry_run=true` shows
    mode `fresh` and action `noop` (or the diff `task -x deps:cascade` gives locally on `main`),
    `Publish` is skipped, and the `Compute` log shows
    `scripts from open-platform-model/.github 2376ffae4bfc665f327d51581350dea694c01504`;
  - `cascade/freshness` and `cascade/settled` appear on the next PR;
  - the next `cascade-task.yml` run checks the resolver out at the pinned SHA.
- **Not dependent on** the core, catalog_opm, library or cli `join-release-cascade` changes. A
  dispatch to a repo with no receiver returns 204 and starts nothing (wiring contract §1).
- **Gates later:** Phase 4 sets `CASCADE_DRY_RUN=false` here, after one non-noop dry-run summary
  has been read (wiring contract §1, §15 item 5). Phase 5's `require-pin-freshness-gate` makes
  the two contexts required.

## Capabilities

### New Capabilities

- `cascade-receiver`: the opm-operator side of the cascade wiring:
  - the `deps-cascade.yml` receiver: its triggers, the caller-owned `publish` job, the dry run
    that fails closed, its concurrency and its per-repo inputs;
  - the `cascade-gates.yml` per-PR status caller;
  - the one-SHA pin of every cascade reference and the wiring check in the `Lint` job.

### Modified Capabilities

- `release-automation`:
  - "Release published once after every release job" names `publish-release` as the publish
    point. It is no longer the final job.
  - "Git tag and GitHub Release on merge" names `publish-release`.
  - "Dependabot leaves OPM Go modules to the release cascade" also keeps Dependabot away from the
    cascade references.
  - Adds "Release notifies downstream after it is published".

## Impact

- **New files:** `.github/workflows/deps-cascade.yml`, `.github/workflows/cascade-gates.yml`,
  `.tasks/cascade/wiring-check.sh`.
- **Changed files:**
  - `.github/workflows/release.yml`: one job, appended after `publish-release`;
  - `.github/workflows/cascade-task.yml`: the resolver `ref:` and the S5 step;
  - `.github/workflows/lint.yml`: one step, "Verify the cascade wiring", in the `Lint` job;
  - `.github/dependabot.yml`: one `ignore` entry;
  - `Taskfile.yml`: the `cascade:wiring:check` task (opm-operator has no aggregate `check` task,
    so CI is the only caller; wiring contract §10);
  - `AGENTS.md`.
- **Untouched:** `.tasks/cascade/` scripts other than the new check, `.tasks/deps.yaml`, every
  Go package. The `Lint` context, which the `main` ruleset requires, keeps its name; it gains one
  step. No new job becomes required.
- **Repo variables** (set by the supervisor or owner, never by this change): `CASCADE_DRY_RUN`
  (`true` at merge), `CASCADE_NOTIFY` (unset means on), `CASCADE_G2_MODE` and `CASCADE_G3_MODE`
  (unset means `warn`).
- **Settings it relies on, already in place (Phase 0):** the `cascade` Environment (`main` only)
  with the secret `CASCADE_APP_PRIVATE_KEY` and the variable `CASCADE_APP_CLIENT_ID`; the
  `opm-cascade` App installed here; `default_workflow_permissions: read`, so every new job
  declares `permissions:`; `sha_pinning_required: true`.
- **Delivery:** one PR titled `ci: join the release cascade` (wiring contract §1). Under the
  `BLANK` squash message only the title reaches `main`, and `ci` cuts no release. The OpenSpec
  archive commit rides that PR, and nothing is pushed to `main` (workspace RELEASING.md,
  "Rulesets on main").
