## Why

The controller has a `Dockerfile` and `task docker:build` / `task docker:buildx` targets, but nothing in CI actually builds, signs, or publishes the controller image. Release-please cuts tags and CHANGELOG entries, but consumers have no image to pull. PRs never exercise the Dockerfile, so container-build breaks only surface after merge. This change closes that gap by adding a supply-chain-aware container image pipeline gated on PRs and release-please tag cuts.

## What Changes

- Add `.github/workflows/image-pr.yml`: on `pull_request`, build single-arch image (`linux/amd64`) and push to GHCR with `:sha-<short>` and `:pr-<PR_ID>` tags.
- Extend `.github/workflows/release.yml`: add an `image-release` job gated on `release-please.outputs.releases_created == 'true'`, build multi-arch (`linux/amd64,arm64,s390x,ppc64le`), push `:sha-<short>`, `:v<version>`, `:latest`.
- Publish to `ghcr.io/open-platform-model/opm-operator`.
- Sign all pushed images with **cosign keyless** (Sigstore OIDC + Rekor transparency log).
- Generate SBOM (SPDX JSON) and attach as registry attestation for every push.
- Generate SLSA build provenance attestation (`actions/attest-build-provenance`) for release builds.
- On release: invoke `task build:installer IMG=ghcr.io/open-platform-model/opm-operator:v<version>@sha256:<digest>` to render `dist/install.yaml` with the image pinned by tag + digest; upload as release asset via `gh release upload`.
- Reuse the existing `task build:installer` target (accepts `IMG` override) — no new task, no changes to its default behavior.
- No rename of the image-agnostic identifiers (`poc-controller` labels, namespace, kustomize resources remain unchanged).

## Capabilities

### New Capabilities

- `container-image-publish`: Building, tagging, signing, and publishing the controller container image from CI (PR and release flows), including supply-chain attestations and digest-pinned install manifest for releases.

### Modified Capabilities

_None._ The new capability consumes outputs from `release-automation` (release-please) but does not change its requirements.

## Impact

- **Affected files**:
  - New: `.github/workflows/image-pr.yml`.
  - Modified: `.github/workflows/release.yml` (adds `image-release` job, adjusts permissions + outputs).
  - No changes to `Dockerfile`, `Taskfile.yml`, `.tasks/`, `config/manager/kustomization.yaml` defaults, or any Go/CUE source.
- **External dependencies** (pinned by SHA per repo convention):
  - `docker/setup-qemu-action`, `docker/setup-buildx-action`, `docker/login-action`, `docker/metadata-action`, `docker/build-push-action`.
  - `sigstore/cosign-installer`, `anchore/sbom-action`, `actions/attest-build-provenance`.
- **GitHub settings / permissions**: Requires `packages: write`, `id-token: write`, `attestations: write` on the relevant jobs. The `open-platform-model` org must allow GHCR publishing from this repo.
- **Consumer impact**: Users gain a pullable, signed controller image and a release-attached `install.yaml` with digest-pinned image reference. Nothing breaks for existing local-dev workflows (Taskfile defaults unchanged).
- **SemVer classification**: MINOR — additive CI/CD capability, no user-facing API or behavioral change to the controller itself. Pre-1.0 so this bumps `0.x.y → 0.(x+1).0` on first release after merge.
