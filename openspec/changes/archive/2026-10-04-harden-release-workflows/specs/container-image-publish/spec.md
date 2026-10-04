## MODIFIED Requirements

### Requirement: Pull-request image build trigger and tags

The PR image workflow SHALL run on `pull_request` events (types: `opened`, `synchronize`, `reopened`) whatever paths they touch (the workflow has no `paths` filter), except for a head named `deps/cascade` or starting with `release-please--`, `module/` (the operator module's bot heads) or `dependabot/`, where the job SHALL be skipped so that unreviewed bot-head code never builds under a `packages: write` and `id-token: write` token. On every run it SHALL build and push exactly two tags: `:sha-<short>` where `<short>` is the 7-character short commit SHA of the PR head, and `:pr-<PR_ID>` where `<PR_ID>` is the GitHub pull request number. The `:pr-<PR_ID>` tag MAY be overwritten by subsequent pushes to the same PR; `:sha-<short>` is effectively immutable because the SHA changes when the commit changes. The build SHALL NOT use the GitHub Actions cache.

#### Scenario: New PR opened
- **WHEN** a contributor opens a pull request whose head commit is `abcd123...`
- **THEN** the workflow SHALL push `ghcr.io/open-platform-model/opm-operator:sha-abcd123` and `ghcr.io/open-platform-model/opm-operator:pr-<PR_ID>`

#### Scenario: Force push updates an open PR
- **WHEN** a contributor force-pushes a new head commit `ef01234...` to an existing PR with `PR_ID=42`
- **THEN** the workflow SHALL push `:sha-ef01234` as a new tag AND overwrite `:pr-42` to point at the new image

#### Scenario: Non-applicable event
- **WHEN** a comment-only or label-only event fires on a pull request (without code changes)
- **THEN** the workflow SHALL NOT run an image build

#### Scenario: Bot head skipped
- **WHEN** a pull request from `deps/cascade`, a `release-please--*` head, a `module/*` head or a `dependabot/*` head opens or updates
- **THEN** the image job SHALL be skipped and SHALL push nothing

### Requirement: Multi-architecture support

PR image builds SHALL produce a multi-architecture manifest list covering `linux/amd64` and `linux/arm64`, the same set as a release. Release image builds SHALL produce a multi-architecture manifest list covering `linux/amd64` and `linux/arm64`.

#### Scenario: PR build architecture set
- **WHEN** the PR image workflow runs
- **THEN** the resulting pushed manifest list SHALL expose exactly two platform descriptors: `linux/amd64` and `linux/arm64`

#### Scenario: Release build architecture set
- **WHEN** the release image-release job runs successfully
- **THEN** the resulting pushed manifest list SHALL expose exactly two platform descriptors: `linux/amd64` and `linux/arm64`
