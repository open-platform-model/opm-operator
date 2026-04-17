## 1. Scaffolding and shared pieces

- [x] 1.1 Determine the current pinned SHA for each third-party action: `docker/setup-qemu-action`, `docker/setup-buildx-action`, `docker/login-action`, `docker/metadata-action`, `docker/build-push-action`, `sigstore/cosign-installer`, `anchore/sbom-action`, `actions/attest-build-provenance`. Record each SHA + human-readable version tag in the workflow files as a comment, per the repo convention already used in `release.yml`.
- [x] 1.2 Confirm the `open-platform-model` GitHub org allows GHCR packages to be published from this repo (package visibility default: public). Document in the PR description if any org-level setting change is required.

## 2. PR image workflow

- [x] 2.1 Create `.github/workflows/image-pr.yml` with trigger `on: pull_request: types: [opened, synchronize, reopened]`.
- [x] 2.2 Declare job-level permissions: `contents: read`, `packages: write`, `id-token: write`.
- [x] 2.3 Add steps: `actions/checkout`, `docker/setup-buildx-action`, `docker/login-action` (GHCR, using `GITHUB_TOKEN`), `docker/metadata-action` producing tags `sha-<short7>` and `pr-<PR_ID>`, `docker/build-push-action` with `platforms: linux/amd64`, `push: true`, GHA cache.
- [x] 2.4 Add cosign install + sign step using the manifest digest from the build-push step output.
- [x] 2.5 Verify locally by hand (or in a throwaway branch) that `metadata-action` produces the expected tag set; iterate until both `:sha-<short7>` and `:pr-<N>` appear.

## 3. Release image job

- [x] 3.1 Edit `.github/workflows/release.yml`: expose `release-please` job outputs `releases_created` and `tag_name`.
- [x] 3.2 Add new job `image-release` with `needs: release-please` and `if: needs.release-please.outputs.releases_created == 'true'`.
- [x] 3.3 Declare job-level permissions: `contents: write`, `packages: write`, `id-token: write`, `attestations: write`.
- [x] 3.4 Add steps: checkout at the release tag, setup QEMU + buildx, login to GHCR, metadata-action producing tags `sha-<short7>`, `v<version>` (from `tag_name`), and `latest`.
- [x] 3.5 `docker/build-push-action` with `platforms: linux/amd64,linux/arm64`, GHA cache, `push: true`; capture `steps.build.outputs.digest`.
- [x] 3.6 Cosign install + keyless sign step for the manifest-list digest.
- [x] 3.7 `anchore/sbom-action` generating SPDX-JSON and attaching as a cosign attestation on the manifest-list digest.
- [x] 3.8 `actions/attest-build-provenance` for the manifest-list digest.

## 4. Release install manifest asset

- [x] 4.1 Add a step to the `image-release` job that runs `task operator:installer IMG="ghcr.io/open-platform-model/opm-operator:${{ needs.release-please.outputs.tag_name }}@${{ steps.build.outputs.digest }}"`. Confirm no changes to `Taskfile.yml` or `.tasks/` are required (target already accepts `IMG` override).
- [x] 4.2 Add a step using `gh release upload "${{ needs.release-please.outputs.tag_name }}" dist/install.yaml --clobber` with `env: GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}`.
- [ ] 4.3 Manually inspect (via a dry run or scratch tag) that the uploaded `install.yaml` contains the pinned `image: ghcr.io/open-platform-model/opm-operator:vX.Y.Z@sha256:...` reference in the controller Deployment.

## 5. Documentation and verification

- [x] 5.1 Add a short "Installing" section to `README.md` showing `kubectl apply -f https://github.com/open-platform-model/poc-controller/releases/latest/download/install.yaml` (or the equivalent stable URL) and the `cosign verify` one-liner with the correct `--certificate-identity-regexp`.
- [x] 5.2 Add a brief note in the README about tag semantics: `:v<version>` and `:<digest>` are immutable; `:latest` tracks the newest release; `:pr-<N>` is mutable and for PR preview only.
- [x] 5.3 Update `adr/` with a new ADR (next zero-padded number) capturing the decision to use cosign keyless + asymmetric multi-arch strategy. Status: `Proposed` → `Accepted` on merge.

## 6. Validation gates

- [x] 6.1 Run `task dev:fmt dev:vet dev:lint` to ensure no incidental Go / tooling changes were introduced.
- [x] 6.2 Run `actionlint` (or `task dev:lint:config` if covered) against the new and modified workflow YAML; fix any warnings.
- [x] 6.3 Open a draft PR; confirm `image-pr.yml` triggers and pushes `:sha-<short7>` + `:pr-<N>`; pull the image and `docker run --rm <image> --help` to confirm it starts.
- [x] 6.4 Confirm the signed image verifies with `cosign verify` using the documented identity regex (pointed at `image-pr.yml` for PR builds).
- [ ] 6.5 After merge, wait for the first release-please cut, confirm `image-release` runs end-to-end, and validate: release assets include `install.yaml`; `install.yaml` references the image by digest; signature + SBOM + provenance are discoverable via `cosign download attestation`.

## 7. Follow-up (out of scope, capture only)

- [ ] 7.2 Open a follow-up issue for image vulnerability scanning (e.g., Trivy) and CVE-gating policy.
