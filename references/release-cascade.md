# Release cascade, pins and workflow hardening

Covers how upstream pins move (`task -x deps:cascade`, `.opm-cli-version`), the three workflows that join the release cascade, the wiring check, and the token and cache rules every workflow follows. The release units and commit types are not covered here.

## Contents

- Pins and `task -x deps:cascade`
- The three cascade workflows
- The key and the pin
- The wiring check
- Workflow token rules
- Pitfalls
- Check before you finish

## Pins and `task -x deps:cascade`

- **The opm CLI pin lives only in `.opm-cli-version`.** Every workflow that installs the opm CLI reads and checks the tag from that one-line file. No workflow carries a version literal, so the cascade can move it without editing `.github/workflows/`. Moving it is a `ci(deps)` commit and releases nothing (workspace `RELEASING.md`: "Cascade files", "Pin classes").
- **`task -x deps:cascade` moves the upstream pins** (workspace `RELEASING.md`): library (`go get` and `go mod tidy`); the opm catalog and core as a consistent set across `config/samples`, `test/fixtures/catalog.go`, the fixture modules and the provider catalog fixture, each changed fixture's version once per PR with its consumers; `.opm-cli-version` last.
- It edits the working tree only. Exit 0: it changed something. Exit 3: nothing to do. Other: error. Always run it with `task -x`, or go-task turns 3 into 201.
- Every version comes from the shared resolver in `open-platform-model/.github` (`CASCADE_RESOLVER`, or the checkout beside this repo).
- `task -x deps:cascade:title` and `deps:cascade:body` print the cascade PR title and body. `task -x deps:cascade:test` tests the task against a stub resolver (`CASCADE_TEST_SET=offline` runs in the `Lint` job, the full set in `cascade-task.yml`). The scripts live in `.tasks/cascade/`.
- `task deps:cascade` never touches the operator module. The module has its own `task -x deps:cascade:module`.

## The three cascade workflows

Contract: workspace `RELEASING.md` (the cascade, pinning, gates and stop switches parts) and the Phase 3 wiring contract (version 3.1) in the `.github` repo.

| Workflow | What it does |
| --- | --- |
| `release.yml`, job `notify-downstream` | Dispatches `upstream-released` to the cli once `publish-release` has published the draft. `publish-docs` never gates it. The repo variable `CASCADE_NOTIFY=off` stops it. |
| `deps-cascade.yml` | Runs on an upstream dispatch, daily at 05:47 UTC and by hand (`gh workflow run deps-cascade.yml -f dry_run=true`). Job `cascade` calls the reusable `cascade-receive.yml` (compute, which runs `task -x deps:cascade`, and the gates; it holds no secret). Job `publish` pushes `deps/cascade` and opens the PR. |
| `cascade-gates.yml` | Calls the reusable `cascade-gates.yml` to post `cascade/freshness` (G2) and `cascade/settled` (G3) on every PR, in `warn` mode until `CASCADE_G2_MODE` and `CASCADE_G3_MODE` say `enforce`. Neither is a required check yet. |

The `publish` job publishes only when `CASCADE_DRY_RUN` is exactly `false` and the run is not gates-only. Any other value or an unset variable is a dry run. The `cascade-publish` action itself refuses to publish otherwise, or when its `gates-only` input is not exactly `false`.

## The key and the pin

- **The key** is read only in the caller-owned `notify-downstream` or `publish` job. That job declares `environment: cascade` and passes `secrets.CASCADE_APP_PRIVATE_KEY` only as the `private-key` input of the SHA-pinned cascade action. It has no checkout or `run:` of its own, and no `env:`, `container:` or `services:` (the action checks out the repo but never runs it). No reusable call passes `secrets:` or `secrets: inherit`.
- **The pin:** all six cascade references name one full `.github` `main` commit SHA with the comment `# .github main`. They are the `cascade-notify` and `cascade-publish` steps, the `cascade-receive.yml` and `cascade-gates.yml` calls, and the `ref:` of the resolver checkouts in `cascade-task.yml` and `module-deps.yml`.
- A `.github` change reaches this repo only through a `ci(deps): pin the cascade to .github <sha7>` PR that moves all six (workspace `RELEASING.md`). Dependabot ignores them. `cascade-task.yml` fails when the pinned commit has no resolver. A bad pin is fixed by a pin PR.

## The wiring check

The wiring check script in `.tasks/cascade/` is a byte-identical copy of the `.github` file at the pin. This repo's values are in the config beside it (described in the `.github` README).

- Offline: `task cascade:wiring:check` (needs mikefarah `yq` v4). Run it after any edit under `.github/workflows/`.
- CI form: the `Lint` job runs the wiring check script with `--pin-on-main` with `GH_TOKEN`. Only SHA-pinned actions from other repos may come before it; every `run:` step goes after it.
- It refuses:
  - a change to the key-holding jobs' shape: keys, step, inputs, permissions, Environment, a `runs-on` other than `ubuntu-latest`;
  - a change to the notify job's `needs`, `if`, `tag` and timeout, or the publish job's `needs`, `if` (with the gates-only clause), `gates-only` input and timeout;
  - a key read or `cascade` Environment elsewhere;
  - a `RELEASE_APP_PRIVATE_KEY` reader without `environment: release`, or a `release` job that reads no key;
  - `secrets` on a call into `.github`;
  - a `release.yml` workflow `env` key other than `REGISTRY`, `IMAGE_NAME` and `CUE_VERSION`;
  - any Actions cache in a workflow the config lists as publishing (`release.yml`, `publish-fixtures.yml`, `docs.yml`, `image-pr.yml`, `test-e2e.yml`, `module-image.yml`, `module-deps.yml`);
  - a `.github` checkout other than `actions/checkout@<SHA>` with `repository`, `ref`, `path` and `persist-credentials: false`;
  - a `Lint` workflow or job `env` name outside `CUE_*`, `OPM_*`, `REGISTRY` and `IMAGE_NAME`;
  - a second SHA or a missing pin comment.
- With `--pin-on-main` it also refuses a SHA not on `.github` `main` and a copy that differs from the file at the pin.
- A new workflow that publishes joins `publish-workflows` in the config.
- It guards against mistakes, not a deliberate edit. Code-owner review guards against that, and the `main` ruleset does not require review while OPM is in beta (no approvals, workspace PR 29).

## Workflow token rules

Spec: `openspec/specs/workflow-hardening`.

- Every workflow declares a top-level `permissions:` of `{}` or `contents: read`. Each job grants exactly what its steps use.
- The release App key (`RELEASE_APP_PRIVATE_KEY`) is read only by jobs that declare the `main`-only Environment `release`: `release.yml`'s `release-please` (`permissions: {}`) and `module-identity-advance`, and the `publish` jobs of `module-image.yml` and `module-deps.yml`. No other job declares `release`.
- No reusable call passes the key through `secrets:`. `module-image-pr` passes none; `module-image.yml`'s `publish` job reads the Environment secret itself (only deleting the org secret is pending).
- A job that publishes, signs or attests never uses the Actions cache (no `type=gha`, `cache: false` on setup-go, the module's `module-*` release jobs included). Reason: a `main`-ref cascade run executes unreviewed code with main's cache scope. `test-e2e.yml`, `image-pr.yml`, `module-image.yml` and `module-deps.yml` use no cache in any job, so the wiring check lists them whole.
- A `pull_request` job holding a write scope or `id-token: write` never runs for a `deps/cascade`, `release-please--*`, `module/*` or `dependabot/*` head. Reason: a Dependabot `github-actions` bump runs a new upstream action release in the job itself (`dependabot.yml` waits 7 days before proposing one). `image-pr.yml` skips those heads. `test-e2e.yml` publishes its pre-release fixtures in a separate `publish-fixtures` job, the only one with `packages: write`, while the kind suite runs with read grants only.
- On those heads, and on a fork, the suite seeds a job-local registry from the tree (`task registry:start`, `task examples:seed`, `task registry:connect`) and sets `LOCAL_REGISTRY`, so the podinfo and redis specs still run with no GHCR credential in the cluster.
- Every `go-task/setup-task` step installs an exact Task version (`3.54.0`), never `3.x`, and passes no `repo-token`.
- `test-e2e.yml` installs `kind` and the `flux` CLI from pinned release assets checked against an in-tree sha256 (`KIND_SHA256` in the workflow, `FLUX_CLI_SHA256_LINUX_AMD64` beside `FLUX_VERSION` in `.tasks/flux.yaml`). Bump each digest with its version.
- `.github/CODEOWNERS` covers `.github/`, `.tasks/`, `Taskfile*.yml`, the release-please files, `.cascade-frozen`, `hack/`, `.opm-cli-version`, `.opm-docs-version` and `docs-kit.cue`.

## Pitfalls

- Running `task deps:cascade` without `-x` turns exit 3 (nothing to do) into 201.
- Moving fewer than all six cascade references leaves two SHAs; the wiring check refuses it.
- A cache in a publishing workflow fails the wiring check.
- A `.github` checkout without `persist-credentials: false` fails the wiring check.
- A pin PR typed `fix` cuts an operator release; type it `ci(deps)`.

## Check before you finish

- [ ] `task cascade:wiring:check` passes after any edit under `.github/workflows/`.
- [ ] A new workflow has `permissions:` at the top and, if it publishes, an entry in `publish-workflows`.
- [ ] No workflow carries an opm CLI version literal.
- [ ] A new `setup-task` step names an exact version and no `repo-token`.
