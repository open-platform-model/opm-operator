## Why

The release cascade security pass (2026-10-04) found that this repo's workflows hand more
than they need to code that no human has reviewed yet:

- **The release App key is an org secret with no Environment.** `release.yml`'s
  `release-please` job reads `secrets.RELEASE_APP_PRIVATE_KEY`, and any workflow on any pushed
  branch could read it too. The App bypasses the tag rulesets, so the key can create a
  permanent `v*` tag on an unreviewed commit. Owner decision 29 moves the key, unrotated, into
  a `main`-only Environment `release`, which the supervisor has already created in this repo.
  Until the owner stores the key there, a job in an Environment without the secret still
  reads the org secret, so the job can declare the Environment now.
- **Write tokens reach branch code from bot heads.** `image-pr.yml` holds `packages: write`
  and `id-token: write` on every pull request, and `test-e2e.yml` holds `packages: write` for
  its whole run and writes that token into a docker config. On a `deps/cascade` or
  `release-please--*` head that code comes from the cascade's compute job, or from a writer
  who pushed to those heads, before any human has looked at it. `actions/checkout` persists the
  job token in `.git/config`, and the code also runs as a user with passwordless sudo, so
  hiding the token from a single step does not keep it from that code. Only a job that does not
  hold the grant keeps it away.
- **Release builds restore poisonable Actions caches.** A `main`-ref run of the cascade's
  compute job executes release-head and dependency code with main's cache scope. The release
  image build restores `type=gha` layers, then signs and attests the result. The example and
  fixture publishing jobs restore setup-go's default cache.
- **Implicit permissions.** `lint.yml`, `test.yml` and `test-e2e.yml` declare no
  `permissions:`, and `release.yml` grants `contents: write` and `pull-requests: write` to
  every job at workflow level. Owner decision 30 flips the repo's default token to read-only
  and requires SHA pinning once this lands, so every workflow must say what it needs.
- **No code owners.** Owner decision 28 makes code-owner review required on `main`, and that
  needs a `CODEOWNERS` file.

## What Changes

- `release.yml`: workflow-level `permissions: {}`. The `release-please` job, the only reader
  of `RELEASE_APP_PRIVATE_KEY`, declares `environment: release` and `permissions: {}`. It acts
  only as the App, so it needs no grant of the job token. The other jobs keep their own
  explicit grants.
- `lint.yml`, `test.yml`: `permissions: contents: read`.
- `test-e2e.yml` is split. A short `publish-fixtures` job holds `packages: write` and only
  publishes the per-commit pre-release fixtures. It runs on a push to `main` and on a
  same-repo pull request whose head is none of `deps/cascade`, `release-please--*` and
  `dependabot/*`. The
  `test-e2e` job runs the kind suite on every event with `contents: read` and `packages: read`.
  It pins the fixtures and passes the controller a read-only GHCR credential only when
  `publish-fixtures` succeeded. Otherwise (a bot head or a fork) it seeds a job-local registry
  from the tree and points the controller at it, so the podinfo spec still runs.
- `image-pr.yml` skips the `deps/cascade`, `release-please--*` and `dependabot/*` heads.
- `.github/dependabot.yml`: a 7-day `cooldown` on the `github-actions` ecosystem.
- No Actions cache in a publishing job. Drop `type=gha` from the release and PR image builds,
  and set `cache: false` on setup-go in `release.yml`'s `publish-examples`,
  `publish-fixtures.yml` and the e2e `publish-fixtures` job.
- `.github/CODEOWNERS` assigns `/.github/`, `/.tasks/`, `/Taskfile*.yml`, the release-please
  config and manifest, `/.cascade-frozen`, `/hack/` (`hack/fixtures.sh` and
  `hack/render-config.sh` run in jobs that hold write tokens), `/.opm-cli-version`,
  `/.opm-docs-version` and `/docs-kit.cue` to both admins.
- Every `go-task/setup-task` step pins an exact Task version instead of `3.x`, and
  `publish-fixtures.yml` stops passing the job token to it as `repo-token`.
- `test-e2e.yml` installs `kind` and the `flux` CLI from pinned release assets checked against
  in-tree sha256 digests, instead of `dl/latest` and `curl | sudo bash`.
- `AGENTS.md` records these rules.

Not changed: the cascade key-holding jobs (`notify-downstream`, `publish`), the `.github` pin,
`.tasks/cascade/wiring-check.sh` and `deps-cascade.yml`. A later change moves those. The
Dependabot `github-actions` entry already exists with the `.github` ignore; review added its cooldown.

## Capabilities

### New Capabilities

- `workflow-hardening`: what each workflow may hold. Explicit least-privilege permissions,
  the release App key only in the `release` Environment, no Actions cache in a publishing job,
  no write token or OIDC for code from a bot head, and code owners on the release machinery.

### Modified Capabilities

- `container-image-publish`: the PR image workflow skips `deps/cascade`, `release-please--*`
  and `dependabot/*` heads and builds without the Actions cache. Review also corrected two
  statements that predate this change: the workflow has no `paths` filter, and the PR image is
  built for `linux/amd64` and `linux/arm64`, not `linux/amd64` only.

## Impact

- Workflows: `release.yml`, `lint.yml`, `test.yml`, `test-e2e.yml`, `image-pr.yml`, `cascade-task.yml`,
  `publish-fixtures.yml`, `.github/dependabot.yml`. New file `.github/CODEOWNERS`. `.tasks/flux.yaml`. `AGENTS.md`.
- No Go, API, CRD or reconcile change, and no release: every commit is `ci` or `docs` typed.
- Cascade, release and Dependabot PRs lose the PR image. They run the full e2e suite against
  a job-local registry seeded from the tree. Release image builds get slower without the layer cache.
- Owner follow-up (not in this repo): store `RELEASE_APP_PRIVATE_KEY` in Environment `release`
  and delete the org secret (decision 29).
