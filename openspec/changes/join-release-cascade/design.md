## Context

Sources of truth, highest first (wiring contract, "Sources"):

1. The owner decisions quoted in the supervisor's `owner-selections-verbatim.md`. Those that bind
   this change: 5 (App without a Workflows permission, key in a `main`-only `cascade`
   Environment), 7 (no auto-merge), 11 and 12 (G2 and G3 warn first), 22 (the supervisor applies
   the settings and repo variables), 24 (pin the cascade actions by SHA in all five repos,
   superseding decision 13's `@main` for them) and 26 (no sandbox; a `.github` change is proven
   offline and by a dry-run canary pin bump).
2. Workspace `RELEASING.md` on `main`: "The cascade" (with "Notify after publish", "Pinning the
   cascade code", "The receiver", "Two-job split"), "Gates", "Stop switches", "Moving the cascade
   pin", "Owner settings", "Rollout and changes".
3. The Phase 2 contract (`.github`
   `openspec/changes/archive/2026-10-04-add-cascade-resolver/contract.md`) and the `.github` main
   specs.
4. The Phase 3 wiring contract (version 3.1, changelogs 3.1.1 and 3.1.2),
   `.github/openspec/changes/archive/2026-10-04-add-release-cascade-workflows/contract.md` in the
   `.github` repo, cited as "contract §N", and the `.github` README ("Cascade workflows",
   "Pinning and bumps").
5. The supervisor's addendum to contract 3.1 for the five join changes (cited as "the
   addendum"): the pin SHA and comment, the `release.yml` `env` allow-list and the `runs-on`
   assertion in the wiring check, `CASCADE_DRY_RUN=true` already set, the canary rule, no
   sandbox.

If this change finds a conflict between these sources, the implementer stops and reports to the
supervisor. It does not pick a side.

**Reconcile phase impact: none.** Source, Render, Apply, Prune and Status do not change. No CRD,
API type, controller or Go package changes. The change is four workflow files, the Dependabot
config, one shell script with its task, and `AGENTS.md`.

### What is on `main` today (`36ba373`)

| Fact | Where |
| --- | --- |
| Workflow-level `permissions: contents: write, pull-requests: write` | `.github/workflows/release.yml:8-10` |
| Workflow-level `concurrency: release-${{ github.ref }}`, `cancel-in-progress: false` | `release.yml:17-19` |
| Workflow-level `env`: `REGISTRY`, `IMAGE_NAME`, `CUE_VERSION` (the addendum's allow-list for this repo) | `release.yml:21-27` |
| `release-please` outputs `releases_created` and `tag_name` | `release.yml:33-35` |
| `publish-docs` calls `docs-kit/.github/workflows/publish.yml@v0.4.0`, a reusable workflow at a tag, and needs `release-please` and `image-release`, not `publish-release` | `release.yml:323-335` |
| `publish-release` is the last job; it needs `[release-please, image-release, publish-examples]` and publishes the draft | `release.yml:337-360` |
| `cascade-task.yml` checks the resolver out from `open-platform-model/.github` at `ref: main`, and skips S5 when the resolver is missing | `.github/workflows/cascade-task.yml:39-73` |
| The required `Lint` job installs Task and runs `deps:release-check`, the offline cascade tests and `docs:pins:check` | `.github/workflows/lint.yml:9-49` |
| Dependabot's `github-actions` entry ignores only `open-platform-model/docs-kit*` | `.github/dependabot.yml:1-11` |
| The CUE version the task compares `language.version` with | `.github/workflows/test.yml:19` (`v0.17.1`), read by `.tasks/cascade/cascade.sh:47`; the check itself is `cascade.sh:393-406` |
| The task runs `cue mod get` and `cue mod tidy` on the CUE modules it moves | `.tasks/cascade/cascade.sh:528` |
| The task runs `go get`, `go mod tidy` and `go install` of the opm CLI, so the receiver needs Go | `.tasks/cascade/cascade.sh:480`, `:491-492` |
| No aggregate `check` task: `Taskfile.yml` has `default`, `tools:*` and `docs:*` plus includes; the `Makefile` has no `check` target | `Taskfile.yml`, `Makefile` |
| Repo settings: `sha_pinning_required: true`, `default_workflow_permissions: read` | `gh api repos/open-platform-model/opm-operator/actions/permissions` (contract, Facts) |

### What `.github` `main` carries at the pin (`2376ffae4bfc665f327d51581350dea694c01504`)

Checked 2026-10-04 (task 1.1):

- `compare/2376ffae…...main` prints `identical`.
- `.github/actions/cascade-notify/action.yml`, inputs `tag`, `client-id`, `private-key` (contract
  §4.1).
- `.github/actions/cascade-publish/action.yml`, inputs `dry-run` (required), `labels-managed`
  (default `'false'`), `client-id`, `private-key` (contract §6.4).
- `.github/workflows/cascade-receive.yml`, inputs `dry-run`, `gates-only`, `g2-mode`, `g3-mode`,
  `setup-go`, `setup-cue`, `cue-version`; no `labels-managed`, no `org-github-ref` (contract
  §6.1).
- `.github/workflows/cascade-gates.yml`, inputs `g2-mode`, `g3-mode` (contract §8.3).
- `.github/workflows/cascade-notify.yml` does not exist (contract §2.1).
- `.github/scripts/cascade/cascade-resolve.sh` exists, so the pinned resolver checkout in
  `cascade-task.yml` always finds it.
- Every nested third-party `uses:` is `owner/repo@<40-hex>`: notify 1, publish 3, receive 9,
  gates 0. That is what lets `sha_pinning_required` pass for the steps that run inside this
  repo's runs.

## Goals / Non-Goals

**Goals**

- An opm-operator release that published its `install.yaml` dispatches `upstream-released` to
  the cli, exactly once per published release.
- library, catalog_opm and cli releases, and a daily sweep, run `task -x deps:cascade` here
  through the shared receiver, dry at first.
- `cascade/freshness` and `cascade/settled` appear on every PR, so Phase 5 can require them.
- The jobs that hold the App key keep their reviewed shape: CI refuses an edit that would let
  repo code run beside the key, or that moves one cascade reference alone.

**Non-Goals**

- No logic in this repo beyond the wiring check. Payload validation, mode selection, the push,
  the PR text, labels and gate evaluation all live in `.github` (contract §2.1).
- No change to `.tasks/cascade/` scripts or to any pin value.
- No required check is added or renamed. The wiring check is a step of the existing `Lint` job.
- Going live, which is Phase 4: the supervisor sets `CASCADE_DRY_RUN=false`.
- No aggregate `check` task or Make target (contract §10.1 item 6: opm-operator has none).

## Decisions

### D1. Notify is a caller-owned job that runs the pinned action after `publish-release`

```yaml
  # Caller-owned: this job declares environment: cascade and passes the App key
  # to the SHA-pinned cascade-notify action as an input.
  notify-downstream:
    name: Notify downstream
    needs: [release-please, publish-release]
    if: needs.release-please.outputs.releases_created == 'true' && vars.CASCADE_NOTIFY != 'off'
    runs-on: ubuntu-latest
    environment: cascade
    timeout-minutes: 20
    permissions:
      contents: read
    steps:
      - name: Notify downstream
        uses: open-platform-model/.github/.github/actions/cascade-notify@2376ffae4bfc665f327d51581350dea694c01504 # .github main
        with:
          tag: ${{ needs.release-please.outputs.tag_name }}
          client-id: ${{ vars.CASCADE_APP_CLIENT_ID }}
          private-key: ${{ secrets.CASCADE_APP_PRIVATE_KEY }}
```

This is contract §4.6's opm-operator block, byte for byte; the comment above the job says only
what §10.1 item 1 allows.

- **Why `publish-release`.** The cli resolves the operator with the `release` kind, which needs
  the non-draft release with `install.yaml`. `image-release` leaves a draft; `publish-docs` is
  independent of the release. The default `success()` in the `if` means notify runs only when
  `publish-release` succeeded, so a draft is never announced, and a failed `publish-docs` does
  not hold notify.
- **Why the caller owns the Environment job.** E1 showed a reusable-workflow job sees the
  caller's Environment variables but not its secrets unless the caller passes
  `secrets: inherit`, which would hand the called workflow every secret this repo has. So the
  key is read here and passed to the action as an input (contract §2.2 "Secrets", §13.1).
- **The key rule** (contract §10.1 item 9): the key is read only in the caller-owned
  `notify-downstream` or `publish` job, which declares `environment: cascade` and passes
  `secrets.CASCADE_APP_PRIVATE_KEY` only as the `private-key` input of the SHA-pinned cascade
  action; that job has no checkout or `run:` of its own, and no `env:`, `container:` or
  `services:` (the action checks out the repo but never runs it); no reusable call passes
  `secrets:` or `secrets: inherit`.
- The job-level `permissions` override the workflow-level `contents: write` down to
  `contents: read`. The App token does the work.
- The workflow-level `concurrency` already serialises notify with the rest of the run.
- The workflow-level `env` (`REGISTRY`, `IMAGE_NAME`, `CUE_VERSION`) reaches the action's steps.
  None of the three makes a shell or node run code at startup; the wiring check allows exactly
  these three (the addendum).
- Recovery: "Re-run failed jobs" re-runs a failed `publish-release` and its dependents, notify
  included, or a failed notify alone. `publish-release` succeeds without changes on a release
  that is already published, so either path is safe.
- The `release-automation` requirement "Release published once after every release job" said
  "a final job". It is no longer the final job, so the delta names `publish-release` in the body
  and in all three scenarios, keeping each scenario's name (a MODIFIED delta keeps every
  main-spec scenario). "Git tag and GitHub Release on merge" said "the final publish step"; a
  second MODIFIED changes it to "the publish job `publish-release`" and keeps its three
  scenarios.

### D2. The receiver and gates callers are the contract's files, byte for byte

`deps-cascade.yml` is contract §5 with the §5.2 opm-operator `jobs:` map (cron `47 5 * * *`,
`setup-go: true`, `labels-managed: false` on the `cascade-publish` step), plus a header comment
between `name:` and `on:`. `cascade-gates.yml` is contract §8.3, plus a header comment.

- **Two jobs.** `cascade` calls the reusable `cascade-receive.yml` (compute and gates; neither
  holds a secret). `publish` is this repo's own job in the `cascade` Environment, with one step:
  the pinned `cascade-publish` action, which verifies the plan compute uploaded, mints the token
  and pushes. It never runs the checked-out code.
- **Dry run fails closed, three times.** The `cascade` job's `dry-run` input, the `publish`
  job's `if:` and the action's required `dry-run` input each read
  `inputs.dry_run == true || vars.CASCADE_DRY_RUN != 'false'` (the `if:` the negation). The
  action publishes only on exactly `false` and fails before the mint on anything but `true` or
  `false`, so a mistyped `if:` here cannot make a dry run publish (contract §5, §9.1).
- **Why copy rather than adapt.** Five repos share these callers. A local variation makes the
  shared code's assumptions false in one repo only. Any change goes through the contract.
- **Why no `setup-cue` or `cue-version`.** The defaults are `true` and `v0.17.1`, which equal
  `test.yml:19` today, and contract §5.2 gives opm-operator no `cue-version`. R4 records the
  drift risk.
- **Why `labels-managed: false`.** opm-operator has no `labels.yml`, so the action creates the
  bot labels itself (contract §5.1, §7.4).
- **Why `setup-go: true`.** The task runs `go get`, `go mod tidy` and `go install` of the opm CLI
  (`.tasks/cascade/cascade.sh:480`, `:491-492`).

### D3. One `.github` SHA, five references, moved only by a pin PR

Every cascade reference carries `2376ffae4bfc665f327d51581350dea694c01504` and the comment
` # .github main`:

| File | Reference |
| --- | --- |
| `release.yml` | `uses: …/actions/cascade-notify@<SHA> # .github main` |
| `deps-cascade.yml` | `uses: …/workflows/cascade-receive.yml@<SHA> # .github main` |
| `deps-cascade.yml` | `uses: …/actions/cascade-publish@<SHA> # .github main` |
| `cascade-gates.yml` | `uses: …/workflows/cascade-gates.yml@<SHA> # .github main` |
| `cascade-task.yml` | `ref: <SHA> # .github main` on the `open-platform-model/.github` checkout |

- Owner decision 24 pins the two actions; the supervisor extended it to the two reusable
  workflows (compute's scripts come from the workflow's own SHA, so a mixed pin would let compute
  and publish disagree across `plan.json`) and to the resolver checkout (so CI tests the resolver
  the receiver runs) (contract §2.4).
- With every reference and every nested step pinned by SHA, `sha_pinning_required: true` stays on
  and refuses nothing (contract §10, opm-operator).
- A `.github` change reaches this repo only through a `ci(deps): pin the cascade to .github
  <sha7>` PR that moves all five at once (RELEASING.md, "Moving the cascade pin"); the canary
  rule of the addendum applies when the diff touches `cascade-publish` or `cascade-notify`.
- Dependabot ignores `open-platform-model/.github*` in its `github-actions` entry, so it never
  opens a PR that moves one reference alone.
- `cascade-task.yml` loses its "S5 is skipped until the resolver is on `main`" fallback: at a
  pinned commit that has the resolver, a missing resolver means a bad pin, and a silent skip
  would hide it (contract §10.1 item 3). The step now fails with
  `no cascade resolver at the pinned .github commit`.
- The pins name the `main` squash SHA from the first commit of this rebuild, because A had
  merged before it started (contract §2.4 "Before A merges": each B pins the squash SHA right
  away). There is no branch-SHA phase and so no separate final pin-swap commit.

### D4. The wiring check runs in the required `Lint` job

`.tasks/cascade/wiring-check.sh` is the contract §10.1 item 6 script with `RECEIVER=true` and
`PIN_COMMENT='.github main'`, plus the addendum's two additions:

- `release.yml`'s workflow-level `env` keys are an allow-list for this repo: `REGISTRY`,
  `IMAGE_NAME`, `CUE_VERSION`. Any other key fails, so `BASH_ENV`, `ENV` and `NODE_OPTIONS`
  (the contract's deny-list) fail with everything else. The `env` must be a map, so an
  expression that builds one cannot slip past the key list.
- Every key-holding job (`notify-downstream`, `publish`) has `runs-on: ubuntu-latest`, so no
  self-hosted or custom runner can hold the key.

`task cascade:wiring:check` runs it, and the `Lint` job runs the task as the step "Verify the
cascade wiring", directly after "Install Task". The runner's preinstalled mikefarah yq is used;
the script refuses any other yq. opm-operator has no aggregate `check` task, so CI is the only
caller (contract §10.1 item 6). The step adds no job and no context.

- **What it is for.** It guards against mistakes: a later edit that adds an `env:`, a second step
  or a broader permission to a key-holding job, reads the key elsewhere, or moves one pin. Review
  and the `main` ruleset (PR plus the `Lint` check) guard against a deliberate edit, since the
  same PR could change the script.
- **actionlint does not check a composite action's inputs**, so the check's exact `with` key sets
  are the only check for `cascade-notify`'s and `cascade-publish`'s inputs.
- Tested against the real tree (pass) and against mutations of a copy (each refused); results in
  "Research & Decisions".

### D5. One PR, sections by contract item

Each section leaves `main` releasable and ends green:

1. The spike (task 1.x): read `.github` `main` at the pin.
2. Notify (items 1, 2).
3. Receiver, gates caller and resolver pin (items 2 to 5).
4. Wiring check and Dependabot (items 6, 7).
5. Docs and the re-grep (items 8 to 11).

The wiring check comes after the files it checks, so each earlier section is green without it.

## Research & Decisions

### Pre-merge test results this change relies on

A's archived `design.md` (`.github`
`openspec/changes/archive/2026-10-04-add-release-cascade-workflows/design.md`) and contract
§11.4 record the tests and their runs. Their conclusions:

- **E1:** a reusable-workflow job in the caller's `cascade` Environment reads the Environment
  variable but not its secret; a caller-owned job that passes the key to a composite action
  mints the token and dispatches.
- **E1b:** the `main`-only `cascade` Environment refuses a run from another branch before any
  step runs.
- **E6:** `sha_pinning_required` refuses a composite action named by branch and runs one named by
  a full SHA; a reusable workflow is not refused either way.

**Decision**: E1's failure is why D1 and D2 have caller-owned key jobs; E6 plus owner decision 24
is why every reference is pinned by SHA. Nothing is left open for opm-operator's
`sha_pinning_required` (contract §15 item 2).

### Wiring check, tested (task 4.4, 2026-10-04)

Mikefarah yq v4.53.3, shellcheck v0.11.0 (clean). Each case copies `.github/workflows/` and the
script into a scratch directory, applies one edit, runs the script and compares its exit code;
37 of 37 behaved as expected.

- **Pass (exit 0):** the real tree (`cascade wiring: ok, .github
  2376ffae4bfc665f327d51581350dea694c01504 (.github main)`); a header comment added to
  `deps-cascade.yml`; `CUE_VERSION` removed from `release.yml`'s `env` (the allow-list does not
  require a key).
- **Refused (exit 1), the contract's 13:** a changed `publish` `if:`, an extra `publish` step,
  notify `contents: write`, `secrets: inherit` on the receive call, the key read by the `lint`
  job, a second SHA on the gates call, `cascade-notify@main`, `ref: main` on the resolver, a
  branch comment, `environment` dropped from `publish`, `environment: cascade` on the `lint` job,
  a literal `dry-run: false`, a changed concurrency group.
- **Refused, the contract's 3.1.1 additions (11):** `env: {BASH_ENV: …}` on `publish`, a
  workflow-level `env` and a `defaults` in `deps-cascade.yml`, `container` on
  `notify-downstream`, `services` on `publish`, a step `env` on the notify step, an `if` on the
  publish step, an extra `with` key on the publish step, and `BASH_ENV`, `ENV` and
  `NODE_OPTIONS` in `release.yml`'s workflow `env`.
- **Refused, the addendum (6):** `FOO` and `bash_env` in `release.yml`'s `env`, an `env` given as
  an expression string, `runs-on: self-hosted` on `notify-downstream`, `runs-on:
  [ubuntu-latest]` and `runs-on: ubuntu-24.04` on `publish`.
- **Refused, extra (4):** `notify-downstream` deleted, the gates call at `@main`, the receive
  call at a tag, a second call into `.github` added to `cascade-gates.yml`.
- **After the implementation review (6 more, all refused):** notify `needs: release-please`
  (dropping `publish-release`), notify `if: always()`, notify `tag: v9.9.9`, notify
  `timeout-minutes: 600`, publish `timeout-minutes: 600`, publish `needs: []`. The check now
  compares the notify job's `needs`, `if`, `tag` and `timeout-minutes` and the publish job's
  `needs` and `timeout-minutes` with contract §4.6 and §5.2; a head comment on the notify job
  still passes.

### Linting the workflows

**Context**: no repo workflow runs actionlint, and the `Lint` job runs golangci-lint, the
release-pin gate, the offline cascade tests and the docs pin check.

**Decision**: each section runs actionlint v1.7.12 from the scratchpad on the changed workflows,
as contract §10 says. It adds no actionlint CI job, which would be a new check and is out of
scope.

## Risks / Trade-offs

- **R1. A bad pin stops releases.** `release.yml` names a `.github` action by SHA. A SHA that
  does not exist, or an action that fails at startup, fails `notify-downstream` only after
  `publish-release` has published, so the release itself is safe; a malformed `uses:` would make
  GitHub refuse the whole run at startup.
  - Mitigation: the wiring check refuses a non-40-hex ref, a missing comment and a second SHA;
    the pre-merge `compare` check refuses a SHA that is not on `.github` `main`; a pin PR is the
    fix and the rollback.
- **R2. The `pull_request_target` trigger.** It runs with a write-capable token in the base
  context, also for fork PRs. The caller grants only `statuses: write` and `actions: write`, and
  the called job checks nothing out (contract §8.3), so no PR code runs.
- **R3. Dependabot PRs (E7).** E7 passed before A merged (a Dependabot `pull_request_target` run's
  token had `statuses: write`, contract §8.3), a proxy for this public repo (contract §15 item 3). Not a
  blocker.
- **R4. CUE version drift fails the receiver hard.** The receiver installs `cue-version`'s
  default, `v0.17.1`. A job that calls a reusable workflow cannot read `env` in `with:`, so
  following `test.yml` would need a literal. The failure chain:
  - An upstream (core or the opm catalog) declares a newer `language.version`. The task's rule
    10 warns (`cascade.sh:393-406`), because it compares against `test.yml:19`.
  - A human bumps `CUE_VERSION` in `test.yml:19`, and with it `release.yml:27` and
    `cascade-task.yml:24`. The warning goes quiet, because it reads `test.yml`, not the CUE the
    receiver installed.
  - The receiver still installs `v0.17.1` and runs `cue mod get` and `cue mod tidy`
    (`cascade.sh:528`) on modules that CLI cannot read (`release.yml:24-26` says so itself).
    Every receiver run then goes red until the contract default moves. G2 runs the same task on
    release heads, so `cascade/freshness` turns to evaluator errors too.
  - The red run is visible, so nothing ships wrong, but the cascade stops in this repo.

  The fix is contract-level and affects all four receivers (Open Question 1). This change keeps
  the contract §5.2 caller as it is.
- **R5. Shared key reach.** Any job that can run in this repo's `cascade` Environment can mint a
  token for every repo the App is installed on (contract, Facts; §11.5). The controls are the
  `main`-only Environment policy, the `main` ruleset and the wiring check, which keeps
  `environment: cascade` and the key on exactly the two caller-owned jobs.
- **R6. A skipped notify is not retried.** With `CASCADE_NOTIFY=off`, notify is skipped, not
  failed, so "Re-run failed jobs" does not re-send it. The cli's daily sweep, at `17 6 * * *`
  (contract §5.1), picks the release up within a day.
- **R7. The wiring check can be edited in the same PR it checks.** It catches mistakes, not a
  deliberate change; review and the `main` ruleset are the control for that (the addendum).

## Migration Plan

1. `CASCADE_DRY_RUN=true` is already set here (supervisor, 2026-10-04); the pre-merge check reads
   it back.
2. The supervisor runs the contract §10.1 pre-merge check on the PR's final head (the `compare`
   check prints `identical` or `ahead`; the "Verify the cascade wiring" step printed
   `cascade wiring: ok, .github 2376ffae4bfc665f327d51581350dea694c01504 (.github main)`).
3. Phase 3 gate (supervisor):
   - run `gh workflow run deps-cascade.yml -R open-platform-model/opm-operator -f dry_run=true`
     and expect mode `fresh` and action `noop`, or the local `task -x deps:cascade` diff on
     `main`; `Publish` skipped; the `Compute` log shows the pinned SHA;
   - check that at least one run **not** started with `dry_run=true` (the 05:47 UTC sweep, or a
     real dispatch) shows "DRY RUN: nothing was pushed" while `CASCADE_DRY_RUN` is `true`;
   - check that the next PR shows both contexts, and that the next `cascade-task.yml` run checks
     the resolver out at the pinned SHA.
4. Phase 4: one non-noop dry-run summary is read, then `CASCADE_DRY_RUN=false`.

**Rollback**:

- Set `CASCADE_NOTIFY=off` (notify) or `CASCADE_DRY_RUN=true` (receiver), or disable the
  workflow.
- Move the pin back with a `ci(deps)` pin PR.
- Or revert the PR. Nothing in it changes the image or a pin.

## Open Questions

1. **R4, CUE version drift** (contract level). Two ways out, for the supervisor:
   - (a) the opm-operator caller passes `cue-version: v0.17.1` explicitly, with a check that it
     equals `test.yml`'s `CUE_VERSION`; this deviates from contract §5.2;
   - (b) a contract amendment so `cascade-receive.yml` reads the CUE version from the receiving
     repo (an input such as `cue-version-file`, default `repo/.github/workflows/test.yml`, read
     with `grep -oP "CUE_VERSION: '\K[^']+"`, the same source as `cascade.sh:406`), which fixes it
     for every receiver at once.

   Recommendation: (b). Until then this change keeps the contract caller.
