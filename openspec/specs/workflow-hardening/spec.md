## Purpose
Limit what each GitHub Actions workflow in this repo can hold. Workflows declare least-privilege permissions explicitly. Only jobs in the main-only `release` Environment read the release App key. Publishing jobs never use the Actions cache. Code from the cascade, release-please, operator module bot and Dependabot heads never runs with a write token or OIDC. Code owners guard the release machinery.

## Requirements

### Requirement: Release App key read only in the release Environment
Every job that reads `secrets.RELEASE_APP_PRIVATE_KEY` SHALL declare `environment: release`, and no other job SHALL declare that Environment. Today those jobs are `release.yml`'s `release-please` and `module-identity-advance` (on a push to `main`), and the `publish` jobs of `module-image.yml` and `module-deps.yml` (which run only on `refs/heads/main`). A reusable-workflow call SHALL NOT pass the key through `secrets:` (nor `secrets: inherit`): a calling job cannot declare an Environment, so the called job that declares `environment: release` reads the Environment secret itself, and the called workflow declares no `workflow_call` secret for it.

#### Scenario: Every key reader runs in the release Environment
- **WHEN** `.github/workflows/` is searched for `RELEASE_APP_PRIVATE_KEY`
- **THEN** every job that names it declares `environment: release`, and no other job declares `release`

#### Scenario: Module image PR call passes no key
- **WHEN** `release.yml`'s `module-image-pr` job calls `module-image.yml`
- **THEN** it has no `secrets:` key, `module-image.yml` declares no `workflow_call` secret, and its `publish` job declares `environment: release`

#### Scenario: Key not yet moved
- **WHEN** Environment `release` has no `RELEASE_APP_PRIVATE_KEY` and the org secret still exists
- **THEN** the jobs of a top-level run (`release-please`, `module-identity-advance`, and `module-image.yml` or `module-deps.yml` run by dispatch) read the org secret and run as before, while `module-image.yml`'s `publish` job called from `module-image-pr` gets no key until the owner moves it into the Environment (a called workflow sees no org secret that is not passed), and a dispatch of `module-image.yml` recovers that run

### Requirement: Explicit least-privilege permissions in every workflow
Every workflow file under `.github/workflows/` SHALL declare a top-level `permissions:` block granting at most `contents: read`, and every job that needs more SHALL declare its own grant naming exactly the scopes its steps use. No workflow SHALL rely on the repository's default token permissions. A job whose steps act only with an App token SHALL grant nothing (`permissions: {}`).

#### Scenario: Read-only default token
- **WHEN** the repository's default workflow token is set to read-only
- **THEN** every workflow runs as before, because each declares its own permissions

#### Scenario: Workflow-level grant
- **WHEN** a workflow file's top-level `permissions:` is inspected
- **THEN** it is `{}` or `contents: read`

#### Scenario: Release-please holds no job token grant
- **WHEN** `release.yml`'s `release-please` job is inspected
- **THEN** it declares `permissions: {}` and acts only with the release App token it mints

### Requirement: No Actions cache in a publishing job
A job that publishes, signs or attests anything (a container image, a CUE module, a release asset) SHALL NOT restore or save the GitHub Actions cache: no `type=gha` cache in a Docker build, `cache: false` on `actions/setup-go`, and no `actions/cache`. Jobs that publish nothing MAY keep their caches, except in a workflow that also has a publishing job: `test-e2e.yml` and `image-pr.yml` use no cache at all, so the cascade wiring check's per-workflow no-cache rule can cover them.

#### Scenario: Release image built without the Actions cache
- **WHEN** `release.yml`'s `image-release` job builds the release image
- **THEN** its build step has no `cache-from` or `cache-to`

#### Scenario: E2E workflow holds no cache
- **WHEN** either job of `test-e2e.yml` sets up Go
- **THEN** `actions/setup-go` runs with `cache: false`

#### Scenario: Module publishing jobs skip the Go cache
- **WHEN** `release.yml`'s `publish-examples`, `publish-fixtures.yml` or `test-e2e.yml`'s `publish-fixtures` job sets up Go
- **THEN** `actions/setup-go` runs with `cache: false`

#### Scenario: Operator module release jobs skip the Go cache
- **WHEN** `release.yml`'s `module-identity-advance` (which later holds the release App key), `module-publish` or `module-manifest` job sets up Go
- **THEN** `actions/setup-go` runs with `cache: false`, and `module-image.yml` and `module-deps.yml` use no cache in any job

### Requirement: Bot-head code never holds a write token or OIDC
A job triggered by `pull_request` that holds any write scope or `id-token: write` SHALL NOT run for a head named `deps/cascade` or starting with `release-please--`, `module/` or `dependabot/`. The code on those heads comes from the release cascade, release-please, the operator module's bot (`module/deps`, `module/operator-image`, pushed with the release App's token) or Dependabot, and no human has reviewed it yet; a Dependabot `github-actions` bump runs a new upstream action release in the job itself. A job that runs such code SHALL hold only read scopes.

#### Scenario: Cascade PR image build skipped
- **WHEN** a pull request from `deps/cascade` opens or updates
- **THEN** `image-pr.yml`'s job is skipped

#### Scenario: Cascade PR e2e runs read-only
- **WHEN** a pull request from `deps/cascade` or `release-please--*` opens or updates
- **THEN** `test-e2e.yml`'s `publish-fixtures` job is skipped, and the `test-e2e` job runs the suite with `contents: read` and `packages: read`, without the pre-release pins or a GHCR credential

#### Scenario: E2E without a publish still runs the podinfo spec
- **WHEN** `test-e2e.yml`'s `publish-fixtures` job is skipped (a fork, or a `deps/cascade`, `release-please--*`, `module/*` or `dependabot/*` head)
- **THEN** the `test-e2e` job seeds a job-local registry from the tree, connects it to the kind network, sets `LOCAL_REGISTRY` so the controller resolves the fixtures from it, and the podinfo and redis specs run instead of skipping

#### Scenario: Dependabot and operator module bot PRs hold no write token
- **WHEN** a pull request from a `dependabot/*` or `module/*` head opens or updates
- **THEN** `image-pr.yml`'s job and `test-e2e.yml`'s `publish-fixtures` job are skipped

#### Scenario: Human PR e2e publishes in its own job
- **WHEN** a same-repo pull request from any other head opens or updates
- **THEN** `publish-fixtures` publishes the pre-release fixtures holding `packages: write`, and the `test-e2e` job pins them and runs the suite holding only read scopes, with a docker config built from its own read-only token

### Requirement: Tools installed at an exact version
Every workflow step that installs a tool SHALL name an exact version, never a floating range such as `3.x` or `latest`. A tool downloaded by a `run:` step SHALL be checked against a sha256 digest kept in this repo before it runs, and no downloaded script SHALL be piped into a shell. A tool action SHALL NOT receive the job token unless it needs it.

#### Scenario: Task installed at an exact version
- **WHEN** a workflow uses `go-task/setup-task`
- **THEN** its `version:` is an exact release such as `3.54.0`, and the step passes no `repo-token`

#### Scenario: kind and flux checked before they run
- **WHEN** `test-e2e.yml` installs `kind` and the `flux` CLI
- **THEN** it downloads the pinned release assets (`KIND_VERSION`, and `FLUX_VERSION` from `.tasks/flux.yaml`), checks each against its in-tree sha256 (`KIND_SHA256`, `FLUX_CLI_SHA256_LINUX_AMD64`), and pipes no install script into `bash`

### Requirement: Code owners on the release machinery
`.github/CODEOWNERS` SHALL name code owners for `/.github/`, `/.tasks/`, `/Taskfile*.yml`, `/release-please-config.json`, `/.release-please-manifest.json`, `/.cascade-frozen`, `/hack/` (scripts that run in jobs holding write tokens or the release App key, `hack/operator-module/` included), `/.opm-cli-version` (the opm binary the publishing jobs install), and `/.opm-docs-version` and `/docs-kit.cue` (the signed docs publish).

#### Scenario: Workflow edit needs a code owner
- **WHEN** a pull request edits a file under `.github/workflows/`
- **THEN** GitHub requests review from the code owners listed for `/.github/`
