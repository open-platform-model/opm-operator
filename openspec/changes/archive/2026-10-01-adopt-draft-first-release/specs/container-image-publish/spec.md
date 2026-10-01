## MODIFIED Requirements

### Requirement: Release image tags

On a gated release run, the image-release job SHALL push, for a release version `v<MAJOR>.<MINOR>.<PATCH>`, `:sha-<short>` (7-character short SHA of the release commit), `:v<MAJOR>.<MINOR>.<PATCH>` (exact release version including leading `v`) and `:latest`. The release commit is the commit the release tag names, read from the checked-out tag, not the pushed commit; the image's `org.opencontainers.image.revision` label SHALL carry it and its `org.opencontainers.image.version` label SHALL be the release tag. The version tag SHALL always resolve to the manifest list first pushed under it (0021:D10:R3): before building, the job SHALL probe the version tag. When it is absent the job builds and pushes, then SHALL confirm the version tag resolves to the pushed digest. When it exists with a revision label equal to the release commit the job SHALL push nothing under the version tag, SHALL reuse the existing digest for every later step, and MAY re-point the mutable tags to it. When it exists with any other revision, or the probe fails for any reason other than the tag being absent, the job SHALL fail without pushing. `:latest` and `:sha-<short>` are mutable by design.

#### Scenario: First release v0.1.0
- **WHEN** release-please cuts `v0.1.0` at commit `abcd123...`
- **THEN** the job SHALL push `:sha-abcd123`, `:v0.1.0`, and `:latest` all pointing at the same manifest list

#### Scenario: Subsequent release v0.2.0
- **WHEN** release-please cuts `v0.2.0` at commit `ef01234...`
- **THEN** the job SHALL push `:sha-ef01234`, `:v0.2.0`, and update `:latest` to point at the new manifest list

#### Scenario: Re-run after the version tag was pushed
- **WHEN** the image-release job is re-run for `v1.0.0-beta.3` after a previous attempt pushed `:v1.0.0-beta.3` built from the release commit
- **THEN** the job SHALL NOT build or push `:v1.0.0-beta.3`, its digest stays unchanged, and the job SHALL sign, attest and render `install.yaml` against that digest

#### Scenario: Version tag held by a different image
- **WHEN** `:v1.0.0-beta.3` already exists and its revision label names a commit other than the release commit
- **THEN** the job SHALL fail without pushing any tag, and the release stays a draft

#### Scenario: Release cut on a later push
- **WHEN** release-please creates the release for a Release PR merged at `abcd123...` during a workflow run triggered by a later push at `ef01234...`
- **THEN** the image SHALL be built from the tag, tagged `:sha-abcd123`, and labelled with revision `abcd123...`

### Requirement: Release install manifest with digest-pinned image

On a gated release run, the image-release job SHALL invoke `task operator:installer IMG="ghcr.io/open-platform-model/opm-operator:v<VERSION>@sha256:<DIGEST>"` where `<DIGEST>` is the manifest-list digest returned by the push step, or the existing digest when the version tag was reused, render `dist/install.yaml`, and upload that file as an asset on the draft GitHub release created by release-please. The upload SHALL follow "Release assets upload only to a draft release" in `release-automation`. The existing `task operator:installer` target SHALL remain unchanged in behavior and its default invocation (no `IMG` override) SHALL still produce a manifest using `controller:latest`.

#### Scenario: Release asset upload
- **WHEN** the image-release job finishes building and signing `v1.2.3` at digest `sha256:abc...`
- **THEN** `dist/install.yaml` SHALL be rendered with image references pinned to `ghcr.io/open-platform-model/opm-operator:v1.2.3@sha256:abc...` AND the file SHALL be uploaded as an asset on the draft GitHub release tagged `v1.2.3`

#### Scenario: Default build-installer unchanged
- **WHEN** a developer runs `task operator:installer` locally without an `IMG` override
- **THEN** the rendered `dist/install.yaml` SHALL continue to reference `controller:latest` as before this change

#### Scenario: Install manifest image is immutable
- **WHEN** a consumer downloads the release-attached `dist/install.yaml` and runs `kubectl apply -f install.yaml`
- **THEN** the controller Deployment SHALL pull the image by digest, guaranteeing the exact bytes from the release regardless of future tag movement
