## Why

The operator is moving to an OPM module of its own. `opm operator install` will pull that module from a registry and apply it as a CLI-owned ModuleInstance, instead of applying a manifest compiled into the cli. `add-operator-module` puts the module's source in this repository under `modules/opm_operator/`. Nothing publishes it yet.

The cli needs a published module version to pin, and that version must say exactly which operator it deploys. The owner decided on 2026-10-04 that the module has a version train of its own, independent of the operator binary's, and that the cli pins the module version and records the operator version it deploys. That forces three things on this repository. The module needs its own versions, release PR and tags. Each operator release has to be followed by a module change that names the new image. And the module's release must publish the kubectl install manifest rendered from the module, so that both install paths can later come from one source. Today the operator release renders that manifest from `config/default`, and the module's image reference would move only by hand.

This changes, for the operator, what archived `0006:D35` and `0021:D4` describe: the manifest the cli installs becomes the module's render, published by the module's release. The amendments that record this live in enhancement 0021 (an amended `0021:D4` and a new decision that the install artifact is a registry module on its own train). This change implements the release half of them. Task G-enh writes `enhancement.yaml` once the amendment's decision numbers are merged.

## What Changes

- **A second release-please package for the module.** It has its own version train starting at `0.1.0`, its own release PR, its own `CHANGELOG.md`, and tags shaped `opm_operator-vX.Y.Z`. No operator tag (`vX.Y.Z`) and no operator release branch (`release/vX.Y`) can take that shape. While the module path is `@v0`, the package overrides the repository's pre-major bump settings: a breaking change raises the minor and every other release raises the patch. The operator package stops counting commits under `modules/opm_operator/`, so a module change never cuts an operator release.
- **Existing release jobs key on the operator package alone.** Today they gate on `releases_created`, which a module-only release also sets. They move to the root package's `release_created` and `tag_name`. The example publisher's previous-tag lookup stops matching module tags.
- **Identity advance on the module's release PR.** The release workflow writes the release PR's version into the module's `identity.Version` with `opm module version set`, as catalog_opm does for its catalog. It is the only writer, so the published module is exactly the tagged source.
- **A module release gate in the `Lint` job.** On the module's release PR it refuses: a development pin; an image reference that is not a published operator release at the digest GHCR serves; an `identity.Version` that is not the proposed version; a version whose major disagrees with the `@v0` path; `1.0.0` or higher while the deployed operator is a prerelease; a deployed operator older than the module's minimum operator version (the first operator release carrying `refuse-own-instance`, opm-operator PR #211); and a module whose CRDs or controller RBAC differ from the operator release it deploys (the release mode of the drift check `add-operator-module` provides). An offline test covers its non-network checks.
- **A module publish job.** It publishes `opmodel.dev/modules/opm_operator` through `opm module publish` with the CLI from `.opm-cli-version`. A re-run of the same release succeeds without republishing. It renders the published module at its default values into `install.yaml`, attaches it to the module's draft release, then publishes the draft. No other workflow or task in this repository publishes under the module's path.
- **A visible carrier commit opens the first module release.** Every commit of the module so far is a hidden type, which opens no release PR. One `feat(module)` PR that touches only `modules/opm_operator/` opens the `0.1.0` release PR.
- **An image-bump PR after every operator release.** Once an operator release is public, a job opens or updates one PR that names the new image by tag and digest in the module. Its title is `fix(deps)`, or `fix(deps)!` when the operator release declares a break or a CRD stops serving a version. A `workflow_dispatch` path recovers a missed run.
- **The module's own cascade PR.** `task deps:cascade` stops at the module's directory. A new `task deps:cascade:module` moves only the module's core and catalog pins, and a workflow opens that move as its own `fix(deps)` PR, so a module pin move never rides a PR that also releases the operator. A module release notifies the cli once its artifact is public.

Not in this change, each its own follow-up: removing `install.yaml` from operator releases (change `stop-operator-install-manifest`, gated on the cli); a docs-kit bundle for the module's `#config` (the module's README documents it, and no site reads a module bundle yet); a cosign signature and provenance attestation on the module artifact (no CUE artifact in the workspace is signed today, and the cli pins content digests).

Release class: the module's own train starts at `0.1.0`. This change's commits are `ci`, `docs` and `chore`, all hidden, except the carrier commit `feat(module)`, which changes only `modules/opm_operator/` and so releases only the module. It cuts no operator release before or after GA.

Delivery: one PR per section (cli needs a published opmodel.dev/modules/opm_operator release after section 3)

## Dependencies / gates

- **GATED: after add-operator-module is merged.** That change creates `modules/opm_operator/` with its `opmodel.dev/modules/opm_operator@v0` path, its `identity/` package, the `operator/` package naming the deployed operator release and image, the generated CRD and RBAC files, `hack/operator-module/min-operator-version`, and the generate and drift-check tasks this change calls. Its proposal names them; task G-dep checks the merged names and substitutes any that changed before any code is written.
- **GATED: the module's minimum operator version carries `refuse-own-instance`.** `hack/operator-module/min-operator-version` on `main` names a published operator release that contains opm-operator PR #211, so no module release can deploy an operator that reconciles its own instance. add-operator-module is itself gated on that release; G-dep re-checks it on the merged tree.
- **G-sandbox.** The unknowns U1 to U8 in design.md are proven in `open-platform-model/release-flow-sandbox` before section 2. The workspace rule requires release-flow changes to be proven there.
- **G-owner.** The owner approves the tag shape, the `0.1.0` start and the 0.x bump rule before section 2 merges. After the first module release, the owner limits the GHCR package `modules/opm_operator` to this repository and makes it public; the cli's install waits for that, nothing in this change does.
- **G-enh.** The 0021 amendment PR has merged, so `enhancement.yaml` can name its decision numbers. Blocks only the archive.
- **join-release-cascade (opm-operator, open).** If it has merged, its `notify-downstream` job is rekeyed in section 2. Section 5's module notify needs `.github` `cascade-notify.yml` on `main`.

Out of scope: transferring ownership of the operator's own instance (another change owns it; the instance stays CLI-owned and the operator never reconciles it), the cli's install, pins and release-pin check, workspace `RELEASING.md` rows, and the three follow-ups named above.

## Capabilities

### New Capabilities

- `operator-module-release`: the module as its own release unit. It covers the package, tag shape and bump rule, identity advance, release gate, publish, install manifest, image-bump PR and cascade notify.

### Modified Capabilities

- `release-automation`: tags and drafts per package, the root package's changelog leaves out module-only commits, the final operator publish job runs only for an operator release, and the draft-only upload rule covers the module's asset.
- `container-image-publish`: the image job gates on the operator package's own `release_created`.
- `deps-cascade`: `deps:cascade` never touches the module; `deps:cascade:module` and its PR move the module's core and catalog pins.
- `docs-bundle`: the operator's bundle publishes only for an operator release, never for a module release.

## Impact

- `release-please-config.json`, `.release-please-manifest.json` (written by release-please only).
- `.github/workflows/release.yml`: the existing jobs' `if`, the new identity-advance step, `module-publish`, `module-publish-release`, `module-image-pr` and `module-notify`; also `lint.yml` and new `module-image.yml` and `module-deps.yml`.
- `.github/scripts/release-guard.sh` and `image-tag-guard.sh`: read-only modes and required assets per tag shape. New `hack/operator-module/release-check.sh` and `hack/operator-module/image.sh` with their offline tests, plus their `.tasks` entries.
- `.tasks/cascade/` (`cascade.sh`, a module pins script and classes file, `test.sh`).
- `modules/opm_operator/README.md` (the carrier commit).
- No API type, CRD, controller or reconcile phase changes.
- Cross-repo, not edited here: the cli's install and release-pin check, the cli's `hack/operator-legacy` (must read only `v*` releases, see design.md), `.github`'s resolver and notify payload, workspace `RELEASING.md`.
