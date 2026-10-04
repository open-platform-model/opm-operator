## Context

Sources, highest first: the owner's security-pass decisions 28 to 31 (2026-10-04), the
supervisor's plan for the `harden-release-workflows` changes, and the findings of the security
audit (CAS-R1, CAS-R3, GOV-1 to GOV-4, and the reviewer's missed items on the release key and
implicit write tokens). Decision 30's settings come after this change merges: default token
read-only, SHA pinning required, Actions may not approve PRs. This repo already runs with a
read-only default and with SHA pinning required, so this change mostly makes the existing
grants explicit and removes the over-grants.

**Reconcile phase impact: none.** No Go, API, CRD or controller change.

Threats this change addresses:

- (b) code from a bot head (`deps/cascade`, `release-please--*`) that no human has reviewed;
- (c) code that the cascade's compute job runs in a `main`-ref run, which holds main's Actions
  cache scope;
- (d) a branch push whose workflow reads an org secret.

## Decisions

### D1. `environment: release` only on `release-please`

`release.yml`'s `release-please` job is the only reader of `RELEASE_APP_PRIVATE_KEY` in this
repo (`grep -rn RELEASE_APP .github`). It runs only on a push to `main`, which the Environment's
branch policy admits. Before the owner stores the key in the Environment, the job reads the org
secret. GitHub resolves an Environment secret first, then a repo secret, then an org secret, so
the job works before, during and after the move. No other job declares `release`, so a push of
the key into the Environment exposes it to that one job.

### D2. Permissions: `{}` at the top, grants per job

`release.yml` drops its workflow-level `contents: write, pull-requests: write`. The
`release-please` job gets `permissions: {}` because both of its steps act only with the App
token they mint. Every other job already declares its own block, and those stay as they are:
`image-release`, `publish-examples`, `publish-docs`, `publish-release`, `notify-downstream`.

`lint.yml` and `test.yml` read the repo and pull public packages, so they get
`contents: read`. `test-e2e.yml` is covered by D4. Every other workflow already declares its
permissions.

### D3. No Actions cache where a job publishes

A cache entry written by a `main`-ref run is visible to every later run on `main`. The compute
job of `deps-cascade.yml` runs release-head and dependency code in such a run (CAS-R3). So a
job that publishes or signs must not restore the cache:

- `release.yml` `image-release`: drop `cache-from: type=gha` and `cache-to: type=gha,mode=max`.
  The image is built from scratch, then signed and attested.
- `release.yml` `publish-examples`: `cache: false` on setup-go. It installs the opm CLI that
  publishes the fixtures.
- `publish-fixtures.yml`: `cache: false` on setup-go.
- `image-pr.yml`: drop `type=gha`. A PR run reads main's cache scope, and the job signs the
  image with the repo's OIDC identity.
- `test-e2e.yml` `publish-fixtures` (D4): `cache: false`.

Jobs that publish nothing keep their caches: Lint, Tests, the e2e test job and Cascade task.
`docs.yml` calls docs-kit's reusable `publish.yml`, and caching there is docs-kit's to decide.

### D4. Split `test-e2e.yml` so the suite never holds a write token

Today the single e2e job holds `packages: write` on every same-repo PR. It writes the job token
into `$RUNNER_TEMP/ghcr/config.json`, mounts that file into the kind cluster, and runs
`go mod tidy` and `task dev:e2e` from the head. Gating the credential step on the head ref is
not enough. `actions/checkout` persists the token in `.git/config`, and the runner user has
passwordless sudo, so any code in the job can reach the job token. Permissions are static per
job and do not take expressions. So the write grant has to leave the job that runs the suite.

- `publish-fixtures` (`packages: write`, `contents: read`) runs only on a push or on a
  same-repo PR whose head is neither `deps/cascade` nor `release-please--*`. It checks out,
  installs opm from `.opm-cli-version`, and runs `task examples:publish PRERELEASE=e2e.g<sha7>`.
  Its output is `prerelease`. Its run still includes a human's own PR code, which is acceptable:
  a human wrote it.
- `test-e2e` (`contents: read`, `packages: read`) needs `publish-fixtures` and runs whenever
  `publish-fixtures` did not fail or get cancelled (`!cancelled() && result != 'failure'`), so a
  skipped publish still runs the suite. When the publish succeeded, it writes a docker config
  holding its own job token (`packages: read`; every fixture package is public) and pins the
  fixtures to the pre-release. Otherwise it does neither, and the podinfo spec skips, as it
  already does on a fork.

The job name `Run on Ubuntu` stays on the test job. Only `Lint` is a required check on `main`.

Rejected alternatives:

- Dropping `packages: write` from every PR. This loses the podinfo spec on human PRs, a
  coverage regression nobody asked for.
- Two copies of the full e2e job, with a write and a read variant. That means about 90
  duplicated lines and two check names.
- A local reusable workflow with an input. The called workflow cannot declare permissions above
  the caller's grant, so it ends up with no `permissions:` of its own, which the "every
  workflow declares permissions" rule would then have to except.

The controller in the kind cluster used to mount a write token. It now mounts a read-only one,
and only on trusted events.

### D5. `image-pr.yml` skips bot heads

The job's `if:` excludes `github.head_ref == 'deps/cascade'` and
`startsWith(github.head_ref, 'release-please--')`. The job must hold `packages: write` and
`id-token: write` to push and sign, so the only way to keep them from a bot head is not to run
there. Nothing consumes a cascade or release PR's `pr-<N>` image.

### D6. CODEOWNERS

The supervisor's spec, keeping only paths that exist here: `/.github/`, `/.tasks/`,
`/Taskfile*.yml`, `/release-please-config.json`, `/.release-please-manifest.json`,
`/.cascade-frozen`. Add `/hack/`, because `hack/fixtures.sh` runs in `publish-fixtures.yml`
(`packages: write`) and the e2e publish job, and `hack/render-config.sh` runs in
`image-release` (`contents`, `packages`, `id-token`, `attestations: write`). The file has no
effect until the supervisor turns on `require_code_owner_review` (decision 28).

## Risks / Trade-offs

- Release image builds take longer without layer caching (multi-arch with QEMU). This is
  accepted: a release happens a few times a week at most.
- Cascade and release PRs no longer run the podinfo e2e spec or build a PR image. The suite's
  other specs still run, and the push to `main` after merge runs everything.
- `test-e2e.yml` still installs `kind` from `latest` and pipes `fluxcd.io/install.sh` into
  bash. The job that runs them now holds only read grants, but the downloads are still
  unpinned. That is follow-up work, not part of this change.
- The wiring check (`.tasks/cascade/wiring-check.sh`) is unchanged and passes. It asserts
  nothing about `release-please` or the cache lines.
