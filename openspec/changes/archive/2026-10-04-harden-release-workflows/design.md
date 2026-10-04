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

- (b) code from a bot head (`deps/cascade`, `release-please--*`, `module/*`, `dependabot/*`) that no human has reviewed;
- (c) code that the cascade's compute job runs in a `main`-ref run, which holds main's Actions
  cache scope;
- (d) a branch push whose workflow reads an org secret.

## Decisions

### D1. `environment: release` only on the key readers

When this change was proposed, `release.yml`'s `release-please` job was the only reader of
`RELEASE_APP_PRIVATE_KEY` in this repo (`grep -rn RELEASE_APP .github`); D9 adds the operator
module's three. It runs only on a push to `main`, which the Environment's
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

Jobs that publish nothing keep their caches: Lint, Tests and Cascade task. The e2e test job
gave up its Go cache in review: the canonical wiring check's no-cache rule is per workflow, and
`test-e2e.yml` and `image-pr.yml` go on its `publish-workflows` list in the wave-2 pin bump,
which a cached suite job would fail.
`docs.yml` and `release.yml`'s `publish-docs` call docs-kit's reusable `publish.yml` (v0.7.0), which already sets up Go with `cache: false` and uses no other cache.

### D4. Split `test-e2e.yml` so the suite never holds a write token

Today the single e2e job holds `packages: write` on every same-repo PR. It writes the job token
into `$RUNNER_TEMP/ghcr/config.json`, mounts that file into the kind cluster, and runs
`go mod tidy` and `task dev:e2e` from the head. Gating the credential step on the head ref is
not enough. `actions/checkout` persists the token in `.git/config`, and the runner user has
passwordless sudo, so any code in the job can reach the job token. Permissions are static per
job and do not take expressions. So the write grant has to leave the job that runs the suite.

- `publish-fixtures` (`packages: write`, `contents: read`) runs only on a push or on a
  same-repo PR whose head is none of `deps/cascade`, `release-please--*` and `dependabot/*`. It checks out,
  installs opm from `.opm-cli-version`, and runs `task examples:publish PRERELEASE=e2e.g<sha7>`.
  Its output is `prerelease`. Its run still includes a human's own PR code, which is acceptable:
  a human wrote it.
- `test-e2e` (`contents: read`, `packages: read`) needs `publish-fixtures` and runs whenever
  `publish-fixtures` did not fail or get cancelled (`!cancelled() && result != 'failure'`), so a
  skipped publish still runs the suite. When the publish succeeded, it writes a docker config
  holding its own job token (`packages: read`; every fixture package is public) and pins the
  fixtures to the pre-release. Otherwise it seeds a job-local registry from the tree
  (`task registry:start`, `task examples:seed`), creates the kind cluster, connects the
  registry to the kind network and sets `LOCAL_REGISTRY`, as `task dev:e2e:local` does. The
  controller then resolves the fixtures from `opm-registry:5000` and core and the catalogs
  from GHCR, so the podinfo and redis specs run on every head, including a fork.

  Review added the seeded path. The first cut let the podinfo spec skip on bot heads, which
  dropped the only live check of the `pkg/ssa` apply path and kstatus readiness from every
  cascade PR, the automated library, core and catalog bumps that most need it.

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
`startsWith(github.head_ref, 'release-please--')` and `startsWith(github.head_ref, 'dependabot/')`. The job must hold `packages: write` and
`id-token: write` to push and sign, so the only way to keep them from a bot head is not to run
there. Nothing consumes a cascade, release or Dependabot PR's `pr-<N>` image.

`dependabot/*` heads were added in review. A Dependabot branch is a same-repo head, and a
Dependabot run honors the permissions a job declares, so a `github-actions` bump ran a new
upstream action release under `packages: write` and `id-token: write` before anyone looked
(run 37219447843 on PR 218). `dependabot.yml` also sets a 7-day `cooldown` on the
`github-actions` ecosystem, so a release that is pulled within a week never reaches a PR.

### D6. CODEOWNERS

The supervisor's spec, keeping only paths that exist here: `/.github/`, `/.tasks/`,
`/Taskfile*.yml`, `/release-please-config.json`, `/.release-please-manifest.json`,
`/.cascade-frozen`. Add `/hack/`, because `hack/fixtures.sh` runs in `publish-fixtures.yml`
(`packages: write`) and the e2e publish job, and `hack/render-config.sh` runs in
`image-release` (`contents`, `packages`, `id-token`, `attestations: write`). Review added
`/.opm-cli-version`, which picks the opm binary that `publish-examples`, `publish-fixtures.yml`
and the e2e publish job install under `packages: write`, and `/.opm-docs-version` and
`/docs-kit.cue`, which drive the docs-kit publish whose signature (`id-token: write`) the site
trusts. The file has no
effect until the supervisor turns on `require_code_owner_review` (decision 28).

### D7. Exact tool versions

Added in review. `go-task/setup-task` installed `version: 3.x`, whatever Task release came out
last, in jobs that hold `contents: write`, `packages: write`, `id-token: write` and
`attestations: write`. Every setup-task step now names `3.54.0`, the release the PR's green CI
resolved `3.x` to. Bumping it is an ordinary reviewed edit. `publish-fixtures.yml` also passed
`repo-token: secrets.GITHUB_TOKEN` (a `packages: write` token) to the action, which needs it
only to list releases when resolving a range; that line is gone.

### D8. kind and flux from checked release assets

Added in review. The e2e suite job installed `kind` from `dl/latest` and piped
`fluxcd.io/install.sh` into `sudo bash`. After D4 that job holds only read grants, but a
tampered download still ran PR CI with root and saw the read-only token mounted in the cluster.
`kind` now comes from the `v0.33.0` release asset, checked against `KIND_SHA256` in the
workflow. The flux CLI comes from the `flux_<FLUX_VERSION>_linux_amd64.tar.gz` release asset,
checked against `FLUX_CLI_SHA256_LINUX_AMD64`, which sits beside `FLUX_VERSION` in
`.tasks/flux.yaml` so the single version pin and its digest move together.

### D9. The operator module's release train (merged from `main`)

`main` gained the operator module's own release train (PRs 221, 224, 226 to 229) while this
change was in review. It added three more readers of the release key, all on `main` only:
`release.yml`'s `module-identity-advance` (pushes the identity commit to the module's release
PR branch), and the `publish` jobs of `module-image.yml` and `module-deps.yml` (push
`module/operator-image` and `module/deps` through `hack/operator-module/bot-pr.sh`). Each now
declares `environment: release`.

`module-image-pr` calls `module-image.yml` as a reusable workflow and passed the key through
`secrets:`. A calling job cannot declare an Environment, so that call read the key outside
`release`. GitHub's rule for reusable workflows: "If you include `environment` in the reusable
workflow at the job level, the environment secret will be used, and not the secret passed from
the caller workflow." So the call passes no `secrets:` and `module-image.yml` declares no
`workflow_call` secret: the called `publish` job reads the Environment secret itself. The cost
is ordering: a called workflow sees no org or repo secret that is not passed, so until the
owner stores the key in Environment `release`, the called `publish` job gets an empty key and
fails to mint (after `publish-release`, so the operator release itself is unaffected). A
dispatch of `module-image.yml` is a top-level run, which falls back to the org secret and
opens the PR.

`module-publish` and `module-manifest` publish, and `module-identity-advance` holds the key
after it builds, so all three set up Go with `cache: false`; `module-publish` and
`module-deps.yml` install Task `3.54.0` (D7). `module-image.yml` and `module-deps.yml` use no
cache, so both go on the canonical wiring check's `publish-workflows` list.

`module/deps` and `module/operator-image` are bot heads like `deps/cascade`: the release App
pushes them and no human has reviewed their content. `image-pr.yml` and the e2e
`publish-fixtures` job skip `module/*`.

Merge order: this change merges only after the owner has stored the key in opm-operator's
Environment `release`, or with a manual dispatch of `module-image.yml` accepted as a release
step until then. The org secret is deleted only after every repo that reads it (nine, owner
decision 29) holds the key in its own `release` Environment; deleting it earlier breaks the
release in every repo that does not yet.

The module publish path writes plain files only. `bot-pr.sh` and the two `publish` jobs
checked member names alone, so a symlink or hard link under `modules/opm_operator/` (to the
checkout's `.git/config`, which holds the App token) would have been copied into a commit
pushed with the token, and a mode change would have been committed. The `publish` jobs now
refuse any tarball member that is not a plain file or directory, and `bot-pr.sh` refuses a
symlink, a hard link or any staged mode but `100644`.

Not redesigned here (supervisor follow-up): `module-deps.yml` is a second publisher that
mints the release App token and opens PRs whose title and body come from repo code
(`task deps:cascade:module:title`/`:body`), outside the `.github` publish boundary of
`bound-cascade-publish`. Its resolver checkout is also a `.github` reference that the
canonical wiring check's fixed reference list does not know.

## Risks / Trade-offs

- Release image builds take longer without layer caching (multi-arch with QEMU). This is
  accepted: a release happens a few times a week at most.
- Cascade, release and Dependabot PRs no longer build a PR image. They run the podinfo spec
  against the tree's fixtures from a job-local registry instead of the GHCR pre-release, so
  they do not exercise the controller's authenticated GHCR pull; human PRs and the push to
  `main` still do.
- The pinned `kind` and `flux` digests are copied from each release's own checksum asset, so
  they lock the content seen on 2026-10-04 rather than prove its origin. A bump has to update
  the digest with the version, by hand.
- The wiring check (`.tasks/cascade/wiring-check.sh`) is unchanged and passes. It asserts
  nothing about `release-please` or the cache lines. The canonical check at `.github`
  `7b9ad1b` passes its release-key and cache rules on this branch; its remaining mismatches
  belong to the wave-2 pin bump, except the `module-deps.yml` resolver reference (D9).
- Until the owner moves the key into Environment `release`, `module-image-pr`'s called
  `publish` job fails to mint (D9); merge after the key move, or dispatch `module-image.yml`
  by hand until then. Delete the org secret only after all nine repos' Environments hold the key.
