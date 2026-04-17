# ADR-014: Container Image CI Publishing — Cosign Keyless, Asymmetric Multi-Arch

## Status

Proposed

## Context

The controller repo ships a `Dockerfile` and Taskfile targets (`docker:build`, `docker:push`, `docker:buildx`) but no CI pipeline builds, signs, or publishes the controller image. Release-please cuts tags and a CHANGELOG but consumers have no pullable artifact. PR changes never exercise the Dockerfile, so container-build regressions surface only after merge.

Downstream installation (`kubectl apply -f`) needs a published, signed, digest-pinned image and a release-attached install manifest. The supply-chain bar for a controller that downloads and applies user payloads across clusters justifies signature + SBOM + provenance on release builds. PR builds need the cheaper "Dockerfile still compiles and pushes" signal.

The controller image is platform-agnostic. K8s node fleets in the wild include amd64 (dominant), arm64 (cost-optimized, Graviton), s390x, and ppc64le (enterprise mainframe / Power). The Taskfile already supports the full four-arch matrix via `docker buildx`. QEMU emulation for s390x + ppc64le on GitHub-hosted runners regularly pushes multi-arch builds past 10 minutes — acceptable per release, prohibitive per PR.

## Decision

Build and publish the controller image from CI with two separate flows sharing the GHCR registry `ghcr.io/open-platform-model/opm-operator`:

- **PR flow** (`.github/workflows/image-pr.yml`, `pull_request`): single-arch `linux/amd64`, tags `:sha-<short7>` and `:pr-<N>`, cosign keyless signature only.
- **Release flow** (`image-release` job inside `.github/workflows/release.yml`, gated on `needs.release-please.outputs.releases_created == 'true'`): multi-arch `linux/amd64,linux/arm64,linux/s390x,linux/ppc64le`, tags `:sha-<short7>`, `:v<version>`, `:latest`, cosign keyless signature + SPDX-JSON SBOM attestation (`anchore/sbom-action` + `cosign attest`) + SLSA build provenance (`actions/attest-build-provenance`).

Sign images with **cosign keyless** (Sigstore OIDC via GitHub Actions token, Fulcio certificate, Rekor transparency log). No long-lived signing key is stored or rotated. The signer identity is the workflow path + ref, verifiable via `--certificate-identity-regexp` and `--certificate-oidc-issuer=https://token.actions.githubusercontent.com`.

Pin the install manifest image reference by digest. The release job invokes the existing `task operator:installer IMG="ghcr.io/open-platform-model/opm-operator:v<VERSION>@sha256:<DIGEST>"` (no new task needed — `kustomize edit set image` natively supports `<tag>@<digest>`), then uploads `dist/install.yaml` as an asset on the release via `gh release upload`. Default dev invocation (`task operator:installer` without `IMG`) is unchanged and still produces `controller:latest`.

Pin all third-party GitHub Actions by full commit SHA, matching the existing repo convention in `release.yml`.

Alternatives considered and rejected:

- **One workflow with matrix strategy** for PR + release — rejected for readability; large `if:` ladders obscure intent and attestation boundaries.
- **Key-based cosign signing** — rejected for POC scope; keyless trades off trust in Sigstore's public-good instances for zero key management. Migration to key-based later does not touch the workflow's core shape.
- **Full four-arch matrix on every PR** — rejected on cost and latency; PRs need Dockerfile-build signal, not production coverage.
- **Trigger image-release on `on: release` event** — rejected; querying `release-please.outputs.releases_created` in the same run is more reliable than depending on the external `release` event firing in time.
- **Parallel `operator:installer:release` task** — rejected as duplication; `kustomize edit set image <name>:<tag>@<digest>` is a first-class kustomize primitive.

## Consequences

**Positive:** Consumers get a pullable, signed, attested controller image per release and a release-attached `install.yaml` with digest-pinned references. PR authors get Dockerfile-build signal without waiting on QEMU emulation. Supply-chain posture aligns with CNCF norms (cosign keyless, Rekor, SPDX SBOM, SLSA provenance).

**Positive:** No new Taskfile surface, no new secrets in the repo. Keyless signing means zero key rotation cost.

**Negative:** Verifiers must trust Sigstore's public-good Fulcio + Rekor. An airgapped or regulation-constrained consumer cannot verify signatures offline without mirror infrastructure.

**Negative:** `:pr-<N>` is mutable by design — two consumers pulling the tag across a force-push see different bytes. The immutable alternative `:sha-<short7>` is always available; the trade-off is chosen for tag-name ergonomics, not supply-chain strength.

**Negative:** QEMU emulation of s390x and ppc64le on GitHub-hosted runners occasionally crashes the builder. Rerun the failed release workflow; the resulting digest changes and that is expected.

**Trade-off:** Image name `opm-operator` diverges from in-cluster identifiers (`poc-controller-system` namespace, `app.kubernetes.io/name: poc-controller` labels, kustomize resource names). Reconciling those is a separate, non-blocking change — captured as follow-up work, not scope creep here.

**Trade-off:** The release workflow now carries significantly more moving parts (buildx, QEMU, cosign, anchore, attest-build-provenance). Re-pinning SHAs on upgrade is a recurring maintenance task; the pinning itself is the correct hygiene for a workflow that publishes signed artifacts.
