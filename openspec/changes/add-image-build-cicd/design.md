## Context

The `poc-controller` repo produces a Kubebuilder-based controller manager. Today:

- `Dockerfile` (distroless/static:nonroot) builds a multi-arch-capable static binary and bundles the `catalog/` CUE module.
- `Taskfile.yml` + `.tasks/` expose `docker:build`, `docker:push`, `docker:buildx` (platforms: `linux/amd64,linux/arm64,linux/s390x,linux/ppc64le`), and `build:installer` (renders `dist/install.yaml` via `kustomize edit set image controller=${IMG}`).
- `.github/workflows/` has `lint.yml`, `test.yml`, `test-e2e.yml`, and `release.yml` (release-please only).
- **Gap**: nothing in CI executes `docker build`. No image lands in any registry. Release-please cuts tags with no published artifact. PRs never verify that the Dockerfile still builds.

Downstream installation (via `kubectl apply -f`) requires a published, signed, digest-pinned image and a release-attached install manifest. This design adds that pipeline while preserving existing local-dev ergonomics.

## Goals / Non-Goals

**Goals:**

- Publish a single, signed, multi-arch image per release to `ghcr.io/open-platform-model/opm-operator`.
- Validate the Dockerfile on every PR by building + pushing a per-PR tag.
- Gate release image publication on release-please actually cutting a release (not every push to `main`).
- Attach a `dist/install.yaml` release asset pinned to `<image>:<version>@sha256:<digest>` so `kubectl apply -f <release-url>` pulls an immutable, verifiable image.
- Sign with cosign keyless (Sigstore OIDC + Rekor) and attach SBOM + SLSA provenance attestations.
- Reuse the existing `task build:installer` target — avoid inventing a parallel "release-installer" task.

**Non-Goals:**

- Renaming in-cluster identifiers (`app.kubernetes.io/name: poc-controller`, `poc-controller-system` namespace, kustomize resource names). The image name diverges from those labels deliberately; reconciling that is a separate future change.
- Supporting PRs from forks (current policy: internal-only contributions). Fork PRs will not be able to push to GHCR; this is acceptable and will be revisited if/when external contributors arrive.
- Changing the default of `IMG` in `Taskfile.yml` (`controller:latest` stays; dev workflows untouched).
- Signing or scanning images produced by local `task docker:build`. CI-only scope.
- Image scanning / CVE gating (e.g., Trivy). Deferred.
- Helm chart publishing. Deferred.

## Decisions

### D1. Two workflows, one registry

**Decision**: Split into `image-pr.yml` (PR trigger) and extend `release.yml` (release trigger, gated by release-please output). Both push to `ghcr.io/open-platform-model/opm-operator`.

**Rationale**: PR and release builds have different matrix sizes, different tags, different attestation requirements. Merging them into one workflow with large `if:` ladders hurts readability. Keeping image-release inside `release.yml` preserves the natural dependency on `release-please.outputs.releases_created`.

**Alternatives considered**:

- One workflow, matrix strategy — rejected for the same readability reason.
- Separate `release.yml` and `image-release.yml` triggered by `on: release`/`on: push tags: 'v*'` — rejected because release-please creates tags _and_ GitHub releases in the same action run; querying `outputs.releases_created` from the same job is more reliable than depending on the external `release` event to fire in time.

### D2. Cosign keyless (OIDC) over key-based

**Decision**: Use `cosign sign` with Sigstore's OIDC flow. Identity = GitHub Actions OIDC token bound to the workflow run.

**Rationale**:

- No long-lived signing key to rotate, store, or compromise.
- Identity is auditable via Rekor transparency log and certificate claims (`repository`, `ref`, `workflow`, `sha`).
- Aligns with how upstream K8s, Flux, cert-manager, and most CNCF projects sign.

**Trade-off**: Verifiers must trust Sigstore's public-good Fulcio + Rekor instances. For a POC, acceptable; if airgap or regulatory constraints change that later, migrate to key-based without touching the workflow's core shape.

**Verification recipe** (documented for consumers):

```bash
cosign verify ghcr.io/open-platform-model/opm-operator:v1.2.3 \
  --certificate-identity-regexp='^https://github.com/open-platform-model/poc-controller/\.github/workflows/release\.yml@refs/tags/v.*$' \
  --certificate-oidc-issuer=https://token.actions.githubusercontent.com
```

### D3. Multi-arch strategy — asymmetric PR vs release

**Decision**:

- PR: `linux/amd64` only.
- Release: `linux/amd64,linux/arm64,linux/s390x,linux/ppc64le` (full Taskfile matrix).

**Rationale**: s390x and ppc64le require QEMU emulation in GHA runners — a full 4-arch build regularly exceeds 10 minutes. Running that on every PR push is cost + latency waste; PRs primarily need "does the Dockerfile still build?" signal. Releases are infrequent and warrant full coverage for broad K8s node support.

### D4. Tagging scheme

**Decision**:

| Trigger | Tags pushed |
| --- | --- |
| PR (any update) | `:sha-<short7>`, `:pr-<PR_ID>` |
| Release (gated) | `:sha-<short7>`, `:v<version>`, `:latest` |

- `<short7>` = 7-char short commit SHA (`${{ github.sha }}` truncated).
- `:pr-<PR_ID>` is mutable (overwrites on force-push); acceptable per user's explicit choice for simplicity. The immutable identifier for any PR build is `:sha-<short7>`.
- `:latest` updates only on release, not on main-push (avoids pointing at an unsigned, un-SBOMed, untagged commit).

### D5. Install manifest digest pinning reuses `task build:installer`

**Decision**: The release job calls the existing `task build:installer` target with an `IMG` override containing both tag and digest:

```bash
task build:installer IMG="ghcr.io/open-platform-model/opm-operator:v${VERSION}@sha256:${DIGEST}"
```

**Rationale**: `kustomize edit set image controller=<name>:<tag>@<digest>` is natively supported. Adding a parallel `build:installer:release` task would duplicate logic. Default dev invocation (`task build:installer` with no `IMG`) still produces `controller:latest` as before.

**Digest source**: `docker/build-push-action` returns `steps.<id>.outputs.digest` (format: `sha256:...`). The release job consumes that output, strips the `sha256:` prefix if needed for templating, and constructs the `IMG` argument.

**Trade-off**: The render requires `kustomize` to be available in the runner (already true — `task build:installer` depends on `:tool:kustomize` via `.tasks/tools.yaml` auto-download).

### D6. Attestations — SBOM + SLSA provenance, on release only

**Decision**:

- PR: cosign signature only (supply-chain signal for "did this PR produce a valid image?").
- Release: cosign signature + SPDX JSON SBOM (via `anchore/sbom-action` attached as a cosign attestation) + SLSA build provenance (via `actions/attest-build-provenance`).

**Rationale**: Attestations grow the `manifest-list` transitive size and cost runner time. For PR builds they add no consumer value (nobody deploys `:pr-123`). Concentrating them on release keeps PR latency low.

### D7. No rename

**Decision**: Image is `opm-operator`. In-cluster `app.kubernetes.io/name: poc-controller` labels, `poc-controller-system` namespace, kustomize resource names all stay as-is. Taskfile `IMG` default (`controller:latest`) unchanged. Kubebuilder `PROJECT` unchanged.

**Rationale**: Flagged as an inconsistency during exploration; user chose not to expand scope. Can be revisited as an orthogonal change without blocking CI/CD delivery.

### D8. Action pinning

**Decision**: All third-party actions pinned by full commit SHA, not by floating tag (`@v5`). Matches existing convention in `release.yml` (`googleapis/release-please-action@5c625bfb...`).

**Rationale**: Supply-chain hygiene for a workflow that itself publishes signed artifacts. Losing this would be self-undermining.

## Risks / Trade-offs

- **Risk**: QEMU emulation of s390x/ppc64le occasionally fails on GHA runners (builder crashes, flaky qemu syscalls).
  **Mitigation**: Release workflow can be re-run; digest changes on retry are expected and acceptable. Document the re-run playbook briefly in the workflow.

- **Risk**: Dropping a PR's image tag `:pr-<N>` leaks a private PR's code to GHCR if the repo ever goes public with private PRs pending.
  **Mitigation**: `open-platform-model` org repo is public; PRs are public by nature. Non-issue under current policy. Revisit if repo visibility changes.

- **Risk**: release-please may open a Release PR rather than cutting a release; the image-release job will skip (correct behavior) but produces no image until the Release PR is merged.
  **Mitigation**: Expected and documented. The `image-release` job's `if:` guard makes the skip explicit in the Actions UI.

- **Risk**: Cosign keyless identity regex drift — if `release.yml` is later renamed/split, the `--certificate-identity-regexp` in verification instructions becomes stale.
  **Mitigation**: Document the verification recipe in the repo README alongside the workflow path so they stay in sync. Add a `ci(workflow):` note requirement if renaming.

- **Trade-off**: `:pr-<N>` is mutable by design. Anyone consuming `:pr-<N>` between a force-push has a stale reference. Immutable builds are available under `:sha-<short7>`. Accepted per user preference.

- **Trade-off**: GHA cache (`type=gha,mode=max`) improves multi-arch build time but is scoped per-branch by default. First release build on a fresh branch pays full cost. Acceptable.

## Migration Plan

No runtime migration. Deployment of this change is CI-only:

1. Land workflow files on `main` (via a conventional-commits merge with `ci:` scope).
2. Verify PR workflow triggers on the merge PR itself — the workflow's own PR exercises the new workflow once the workflow file is in the PR's branch.
3. On first release-please release after merge: confirm `image-release` job runs, image is pushed, signature + SBOM + provenance attach, and `dist/install.yaml` is uploaded to the GitHub release.
4. Rollback: revert the commit. No data/state side-effects. Any already-pushed images stay in GHCR and can be deleted manually if desired.

## Open Questions

None blocking implementation. Resolved during exploration:

- Registry: `ghcr.io/open-platform-model/opm-operator` ✓
- Signing: cosign keyless ✓
- Arches: asymmetric (amd64 PR, all 4 release) ✓
- Digest pinning: reuse `task build:installer` with `IMG` override ✓
- Gating: `release-please.outputs.releases_created == 'true'` ✓
- Rename: deferred ✓
