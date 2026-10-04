## Context

Sources of truth, in order (wiring contract, "Sources"):

1. The owner decisions quoted in the supervisor's `owner-selections-verbatim.md`. Those that bind
   this change: 5 (App without a Workflows permission, key in a `main`-only `cascade`
   Environment), 7 (no auto-merge), 11 and 12 (G2 and G3 warn first), 13 (`@main` plus ruleset)
   and 22 (the supervisor applies the settings and repo variables).
2. Workspace `RELEASING.md` on `main`: "The cascade" (`:162-325`), "Gates" (`:327-358`), "Stop
   switches" (`:454-462`), "Owner settings" (`:464-527`), "Rollout and changes" (`:529-571`).
3. The Phase 2 contract (`.github`
   `openspec/changes/archive/2026-10-04-add-cascade-resolver/contract.md`) and the `.github`
   main specs.
4. The Phase 3 wiring contract, version 2,
   `/var/home/emil/.cache/claude-tmp/claude-1000/-var-home-emil-dev-open-platform-model/2ee0ca8e-268c-4b20-8bd9-b5e4f0d96717/scratchpad/p3-wiring-contract.md`,
   cited as "contract §N".

If this change finds a conflict between these sources, the implementer stops and reports to the
supervisor. It does not pick a side.

**Reconcile phase impact: none.** Source, Render, Apply, Prune and Status do not change. No CRD,
API type, controller or Go package changes. The change is three workflow files and one
`AGENTS.md` bullet.

### What is on `main` today (`14345e7`)

| Fact | Where |
| --- | --- |
| Workflow-level `permissions: contents: write, pull-requests: write` | `.github/workflows/release.yml:8-10` |
| Workflow-level `concurrency: release-${{ github.ref }}`, `cancel-in-progress: false` | `release.yml:17-19` |
| `release-please` outputs `releases_created` and `tag_name` | `release.yml:33-35` |
| `publish-docs` calls `docs-kit/.github/workflows/publish.yml@v0.4.0`, a reusable workflow at a tag, and needs only `image-release` | `release.yml:323-335` |
| `publish-release` is the last job; it needs `[release-please, image-release, publish-examples]` and publishes the draft | `release.yml:337-360` |
| The receiver's checkout layout (`repo/`, `org-github/`), which the shared receive workflow copies | `.github/workflows/cascade-task.yml:32-46` |
| The CUE version the task compares `language.version` with | `.github/workflows/test.yml:19` (`v0.17.1`), read by `.tasks/cascade/cascade.sh:47`; the check itself is `cascade.sh:398-406` |
| The other CUE literals that move with it | `release.yml:27` and `cascade-task.yml:23` (both `v0.17.1`) |
| The task runs `cue mod get` and `cue mod tidy` on the CUE modules it moves | `.tasks/cascade/cascade.sh:528` |
| The task installs the opm CLI with `go install`, so the receiver needs Go | `.tasks/cascade/cascade.sh:480` |
| Repo settings: `sha_pinning_required: true`, `default_workflow_permissions: read` | `gh api repos/open-platform-model/opm-operator/actions/permissions` (research, 2026-10-04) |
| No repo workflow runs actionlint | `.github/workflows/lint.yml:13-49` |

## Goals / Non-Goals

**Goals**

- An opm-operator release that published its `install.yaml` dispatches `upstream-released` to
  the cli, exactly once per published release.
- library, catalog_opm and cli releases, and a daily sweep, run `task -x deps:cascade` here
  through the shared receiver, dry at first.
- `cascade/freshness` and `cascade/settled` appear on every PR, so Phase 5 can require them.

**Non-Goals**

- No logic in this repo. Payload validation, mode selection, the push, the PR text, labels and
  gate evaluation all live in `.github` (contract §2.1).
- No change to `.tasks/cascade/` or to any pin value.
- No required check is added or changed.
- Going live, which is Phase 4: the supervisor sets `CASCADE_DRY_RUN=false`.
- RELEASING.md amendments (contract §14). Those ship in a workspace PR together with `.github`
  A.

## Decisions

### D1. Notify hangs off `publish-release`, not off the image or the tag

```yaml
  notify-downstream:
    name: Notify downstream
    # The cli resolves the operator with the release kind, which needs the
    # published release and its install.yaml asset, so notify waits for the
    # publish point. publish-docs never gates it.
    needs: [release-please, publish-release]
    if: needs.release-please.outputs.releases_created == 'true' && vars.CASCADE_NOTIFY != 'off'
    permissions:
      contents: read
    uses: open-platform-model/.github/.github/workflows/cascade-notify.yml@main
    with:
      tag: ${{ needs.release-please.outputs.tag_name }}
```

- The default `success()` in the `if` means notify runs only when `publish-release` succeeded. A
  draft is never announced.
- The job is appended after `publish-release`. The job-level `permissions` override the
  workflow-level `contents: write` (`release.yml:8-10`) down to `contents: read` for the call.
- The workflow-level `concurrency` (`release.yml:17-19`) already serialises notify with the rest
  of the run.
- Recovery: "Re-run failed jobs" re-runs a failed `publish-release` and its dependents, notify
  included, or a failed notify alone. `publish-release` succeeds without changes on a release
  that is already published, so either path is safe.
- The `release-automation` requirement "Release published once after every release job" said
  "a final job". It is no longer the final job, so the delta names it `publish-release` in the
  body and in all three scenarios, keeping each scenario's name (a MODIFIED delta keeps every
  main-spec scenario). "Git tag and GitHub Release on merge" said "the final publish step"; a
  second MODIFIED changes it to "the publish job `publish-release`" and keeps its three
  scenarios.

### D2. The receiver and gates callers copy the contract templates verbatim

`deps-cascade.yml` is contract §5 with the §5.1 opm-operator row (cron `47 5 * * *`,
`setup-go: true`, `labels-managed: false`):

```yaml
name: Deps cascade

on:
  repository_dispatch:
    types: [upstream-released]
  schedule:
    - cron: '47 5 * * *'
  workflow_dispatch:
    inputs:
      dry_run:
        description: Compute and show the diff in the job summary; push nothing
        type: boolean
        default: false
      gates_only:
        description: Evaluate and post the release-PR gates only (sent by Cascade gates)
        type: boolean
        default: false

permissions: {}

concurrency:
  group: ${{ github.ref != 'refs/heads/main' && format('deps-cascade-{0}', github.ref) || (inputs.gates_only && 'deps-cascade-gates' || 'deps-cascade') }}
  cancel-in-progress: false

jobs:
  cascade:
    name: Deps cascade
    permissions:
      contents: read
      pull-requests: read
      statuses: write
    uses: open-platform-model/.github/.github/workflows/cascade-receive.yml@main
    with:
      dry-run: ${{ inputs.dry_run == true || vars.CASCADE_DRY_RUN != 'false' }}
      gates-only: ${{ inputs.gates_only == true }}
      g2-mode: ${{ vars.CASCADE_G2_MODE || 'warn' }}
      g3-mode: ${{ vars.CASCADE_G3_MODE || 'warn' }}
      setup-go: true
      labels-managed: false
```

`cascade-gates.yml` is contract §8.3 unchanged.

- **Why copy rather than adapt.** Five repos share these callers. A local variation makes the
  shared workflow's assumptions false in one repo only. Any change goes through the contract.
- **Why no `setup-cue` or `cue-version`.** The defaults are `true` and `v0.17.1`, which equal
  `test.yml:19` today, and contract §5 has no per-repo `cue-version`. R4 shows the drift is a
  hard failure, not a quiet one, and Open Question 2 asks the supervisor to settle it.
- **Why `labels-managed: false`.** opm-operator has no `labels.yml`, so the receiver creates the
  five bot-relevant labels with `gh label create --force` (contract §6.4 step 4). That matches
  RELEASING.md "Labels".
- **Why `setup-go: true`.** The task runs `go get`, `go mod tidy`, `go run ./hack/crdref` and
  `go install` of the opm CLI (`.tasks/cascade/cascade.sh:480`).

### D3. One PR, three `ci` sections

Each section leaves `main` releasable: none changes a required check or the shipped image.

- The receiver comes before the gates caller, because the gates caller dispatches
  `deps-cascade.yml`.
- Notify is independent, and it comes after the spike because it is the riskiest edit: it touches
  `release.yml`.

## Research & Decisions

### Is `@main` allowed under `sha_pinning_required: true`?

**Context**: opm-operator is the only cascade repo with `sha_pinning_required: true`. If the
setting refuses a `uses: …/cascade-notify.yml@main` call, GitHub fails the whole `release.yml`
run at startup, and every operator release stops. `deps-cascade.yml` and `cascade-gates.yml`
would fail the same way, which is harmless but noisy.

**Explored**: the wiring research (2026-10-04) and GitHub's documentation.

- GitHub's repository Actions settings page says, for "Require actions to be pinned to a
  full-length commit SHA": all actions must be pinned to a full-length commit SHA, and
  "Reusable workflows can still be referenced by tag" ([Managing GitHub Actions settings for a
  repository](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/enabling-features-for-your-repository/managing-github-actions-settings-for-a-repository);
  [changelog 2025-08-15](https://github.blog/changelog/2025-08-15-github-actions-policy-now-supports-blocking-and-sha-pinning-actions/)).
  So a reusable workflow at a tag is exempt by the docs. The docs do not name a branch ref.
- `release.yml:331` calls `docs-kit/.github/workflows/publish.yml@v0.4.0`, a tag, and its
  release-time calls have run green. Run 37183553716 (2026-10-04, a push to `main`) is a `Docs`
  workflow run (`docs.yml:66`, `:77`, `:88`), not a `release.yml` run; it calls the same
  docs-kit workflow at a tag. docs-kit's `publish.yml@v0.4.0` pins all six of its actions by SHA.
- What the setting does reach is every action step **inside** a called workflow, because those
  steps run in opm-operator's run. So every step `uses:` in `cascade-notify.yml`,
  `cascade-receive.yml` and `cascade-gates.yml` must be `owner/repo@<40-hex>`. Task 1.3 checks
  that statically. On branch `feat/add-release-cascade-workflows` at `04bc25d` all fifteen are
  SHA-pinned (notify 2, receive 13, gates 0).
- Contract §11.4 E6 runs only `cascade-sandbox-down`'s caller (receive and gates). The notify
  call from `release.yml` is never run under the setting. By the docs that does not matter,
  because the open question is the ref form of the `uses:` line, which is the same `@main` for
  all three calls.

**Decision**: the only open question E6 still settles is whether a **branch** ref (`@main`) is
treated like a tag. Section 1 reads E6's result from `.github`
`add-release-cascade-workflows`'s `design.md`, and task 1.3 checks the step pins statically.

- If `@main` is accepted, the change proceeds as written.
- If it is refused, the change stops and the supervisor takes the owner's choice (contract §15
  item 2): turn the setting off, or pin opm-operator's three calls to a `.github` commit SHA.
- Under the SHA choice, a Dependabot `ignore` for `open-platform-model/.github*` would also be
  needed, so Dependabot does not move the pin on its own. The owner then decides it.
- The contract §13.1 fallback (composite actions at
  `open-platform-model/.github/.github/actions/…@main`) is an **action** reference, not a
  reusable workflow. opm-operator's setting refuses an action at `@main`, so §13.1 cannot work
  here as written. If E1 fails, opm-operator needs an owner decision as well: a SHA pin of the
  composite actions, or the setting turned off.

**Rationale**: the cost of guessing wrong is a stopped release pipeline, and the test already
exists in A's cycle.

### Does a called workflow's `environment: cascade` reach this repo's secret?

**Context**: the App key is an Environment secret here. Notify and publish declare
`environment: cascade` inside the reusable workflow, because a calling job cannot set
`environment` (RELEASING.md "Notify after publish").

**Explored**: the wiring research found no GitHub doc that confirms the called job resolves the
caller's Environment, and a search summary claimed the opposite. Contract §2.2 assumes it, and
E1 and E1b prove it before A merges.

**Decision**: the spike also reads E1 and E1b. If E1 failed, contract §13.1 moves publish and
notify into composite actions, and each caller gains its own `environment: cascade` job. That is
a contract change, so this change stops and reports. It does not redesign itself. In this repo
§13.1 at `@main` also collides with `sha_pinning_required` (previous section), so it needs an
owner decision on top of the contract change.

**Rationale**: a contract change belongs to the supervisor (contract, "Sources").

### Where notify attaches

**Context**: notify must run only when the artifact the cli resolves is public.

**Explored**:

- `image-release` (`release.yml:55-231`) leaves a draft, which the resolver does not see.
- `publish-docs` is independent of the release.
- `publish-release` (`release.yml:337-360`) is the single publish point.

**Decision**: `needs: [release-please, publish-release]`, with the `releases_created` guard
(contract §4.5, opm-operator row).

**Rationale**: the resolver's `release` kind needs a non-draft release carrying `install.yaml`
(RELEASING.md "Notify after publish").

### Linting the workflows

**Context**: no repo workflow runs actionlint, and the `Lint` job (`lint.yml:13`) runs
golangci-lint only.

**Decision**: each section runs `actionlint` from the local binary on the changed workflows, as
contract §10 says. It adds no actionlint CI job, which is out of scope and would be a new check.

**Rationale**: keeps the required checks unchanged.

## Risks / Trade-offs

- **R1. A broken call stops releases.** `release.yml` now depends on a file in another repo at
  `@main`. If `.github` `main` breaks `cascade-notify.yml`, only the notify job fails, because
  the release is already published. If the file is missing or the call is refused (E6), the run
  never starts.
  - Mitigation: merge only after A (gate) and after E6.
  - `.github`'s `main` ruleset requires PR review.
- **R2. The `pull_request_target` trigger.** It runs with a write-capable token in the base
  context, also for fork PRs. The caller grants only `statuses: write` and `actions: write`, and
  the called job checks nothing out (contract §8.3), so no PR code runs.
- **R3. Dependabot PRs (E7).** If a Dependabot-triggered `pull_request_target` run gets a
  read-only token, the per-PR caller fails on every Dependabot PR here (`dependabot.yml` has the
  `github-actions` and `gomod` entries). It is not a required check, so nothing blocks. It
  becomes a Phase 5 blocker (contract §15 item 3).
- **R4. CUE version drift fails the receiver hard.** The receiver installs `cue-version`'s
  default, `v0.17.1`. A job that calls a reusable workflow cannot read `env` in `with:`, so
  following `test.yml` would need a literal. The failure chain:
  - An upstream (core or the opm catalog) declares a newer `language.version`. The task's rule
    10 warns (`cascade.sh:398-406`), because it compares against `test.yml:19`.
  - A human bumps `CUE_VERSION` in `test.yml:19`, and with it `release.yml:27` and
    `cascade-task.yml:23`. The warning goes quiet, because it reads `test.yml`, not the CUE the
    receiver installed.
  - The receiver still installs `v0.17.1` and runs `cue mod get` and `cue mod tidy`
    (`cascade.sh:528`) on modules that CLI cannot read; `release.yml:24-26` says so itself ("an
    older CLI refuses to read them"). Every receiver run then goes red until the contract
    default moves.
  - The red run is visible, so nothing ships wrong, but the cascade stops in this repo.

  This change keeps the contract §5 caller as it is and raises Open Question 2.
- **R5. Shared key reach.** Any job that can run in this repo's `cascade` Environment can mint
  a token for all seven App installations (contract, Facts; §11.5). The controls are the
  `main`-only Environment policy and the `main` ruleset. This change adds two workflows that
  reach the Environment, both only through `@main` reusable workflows.
- **R6. A skipped notify is not retried.** With `CASCADE_NOTIFY=off`, notify is skipped, not
  failed, so "Re-run failed jobs" does not re-send it. The cli's daily sweep, at
  `17 6 * * *` (contract §5.1), picks the release up within a day.

## Migration Plan

1. The supervisor sets `CASCADE_DRY_RUN=true` here (owner decision 22) before the PR merges.
2. The PR merges after A.
3. Phase 3 gate (supervisor):
   - run `gh workflow run deps-cascade.yml -R open-platform-model/opm-operator -f dry_run=true`
     and expect mode `fresh` and action `noop`, or the local `task -x deps:cascade` diff on
     `main`;
   - check that at least one run **not** started with `dry_run=true` (the 05:47 UTC sweep, or a
     real `upstream-released` dispatch) shows "DRY RUN: nothing was pushed" in its summary while
     `CASCADE_DRY_RUN` is `true`. That proves the variable, not the input, keeps the receiver dry;
   - check that the next PR shows both contexts.
4. Phase 4: one non-noop dry-run summary is read, then `CASCADE_DRY_RUN=false`.

**Rollback**:

- Set `CASCADE_NOTIFY=off` (notify) or `CASCADE_DRY_RUN=true` (receiver), or disable the
  workflow.
- Or revert the PR. Nothing in it changes the image or a pin.

## Open Questions

1. **E6** (above). If `@main` is refused, the owner chooses (contract §15 item 2).
2. **R4.** The drift is a hard red receiver run, not a quiet one. Two ways out, for the
   supervisor:
   - (a) the opm-operator caller passes `cue-version: 'v0.17.1'` explicitly, with a comment
     "keep in step with `test.yml` `CUE_VERSION`", plus a check in the offline set of
     `.tasks/cascade/test.sh` (which runs in the required `Lint` job) that the two literals
     match; this adds a
     fourth CUE literal and deviates from contract §5's verbatim caller;
   - (b) a contract §5 amendment so the shared receive workflow reads the CUE version from the
     receiving repo (for example `cue-version-file: .github/workflows/test.yml`), which fixes it
     for every receiver at once.

   Recommendation: (b), because the drift hits every CUE-moving receiver, not only this one;
   (a) as a stopgap only if (b) cannot land before the next CUE bump. Until then this change
   keeps the contract default.
3. **E7.** Dependabot `pull_request_target` token (R3). This is a Phase 5 item; it does not
   block this change.
