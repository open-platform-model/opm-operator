## Why

The operator is moving to an OPM module of its own. `opm operator install` will pull that module from a registry and apply it as a CLI-owned ModuleInstance, instead of applying a manifest compiled into the cli. `add-operator-module` puts the module's source in this repository under `modules/opm_operator/`. Nothing publishes it yet.

The cli needs a published module version to pin, and that version must say exactly which operator it deploys. The owner decided on 2026-10-04 that the module has a version train of its own, independent of the operator binary's, and that the cli pins the module version and records the operator version it deploys. That forces three things on this repository. The module needs its own versions, release PR and tags. Each operator release has to be followed by a module change that names the new image. And the kubectl install manifest, which users apply from their own tooling, has to come from the module, so the two install paths cannot drift apart. Today the operator release renders that manifest from `config/default`, and the module's image reference would move only by hand.

This changes, for the operator, what archived `0006:D35` and `0021:D4` describe: the manifest the cli installs is no longer an asset of the operator release. The amendments that record this live in enhancements 0021 and 0012. This change carries its own requirements and does not implement an enhancement decision.

## What Changes

- **A second release-please package for the module.** It has its own version train starting at `0.1.0`, its own release PR, its own `CHANGELOG.md`, and tags shaped `opm_operator-vX.Y.Z`. No operator tag (`vX.Y.Z`) and no operator release branch (`release/vX.Y`) can take that shape. While the module path is `@v0`, the package overrides the repository's pre-major bump settings: a breaking change raises the minor and every other release raises the patch. The operator package stops counting commits under `modules/opm_operator/`, so a module change never cuts an operator release.
- **Existing release jobs key on the operator package alone.** Today they gate on `releases_created`, which a module-only release also sets. They move to the root package's `release_created` and `tag_name`. The example publisher's previous-tag lookup stops matching module tags.
- **Identity advance on the module's release PR.** The release workflow writes the release PR's version into the module's `identity.Version` with `opm module version set`, as catalog_opm does for its catalog. It is the only writer, so the published module is exactly the tagged source.
- **A module release gate in the `Lint` job.** On the module's release PR it refuses: a development pin; an image reference that is not a published operator release at the digest GHCR serves; an `identity.Version` that is not the proposed version; a version whose major disagrees with the `@v0` path; `1.0.0` or higher while the deployed operator is a prerelease; and a module whose CRDs or controller RBAC differ from the operator release it deploys (the release mode of the drift check `add-operator-module` provides).
- **A module publish job.** It publishes `opmodel.dev/modules/opm_operator` through `opm module publish` with the CLI from `.opm-cli-version`. A re-run of the same release succeeds without republishing. It signs the artifact with cosign keyless and attests build provenance, as the image job does. It renders the published module at its default values into `install.yaml`, attaches it to the module's draft release, then publishes the draft. No other workflow or task in this repository publishes under the module's path.
- **An image-bump PR after every operator release.** Once an operator release is public, a job opens or updates one PR that names the new image by tag and digest in the module and regenerates the module's CRDs and RBAC from that tag. Its title is `fix(deps)`, or `fix(deps)!` when the operator release declares a break or a CRD stops serving a version. A `workflow_dispatch` path recovers a missed run.
- **The module in the cascade and the docs.** `deps:cascade` moves the module's core and catalog pins as shipped pins. A module release notifies the cli once its artifact is public. A docs-kit bundle `opm-operator-module` publishes the module's `#config` documentation at the module's own version.
- **GATED, last section: the operator release stops publishing `install.yaml`.** It lands only after the cli no longer embeds or downloads the operator release's manifest and its cascade no longer resolves the operator through that asset. The operator's notify job then stops dispatching to the cli. **BREAKING** for anyone who downloads `install.yaml` from an operator release: the manifest moves to the module release.

Release class: the module's own train starts at `0.1.0`. This change's commits are `ci`, `docs` and `chore`, all hidden, so it cuts no operator release before or after GA. The last section removes an operator release asset; open question 1 in design.md asks whether that section's commit carries a `!` (after GA that would be MAJOR for the operator; on the beta line it ships as the next `-beta.N`).

Delivery: one PR per section (cli needs a published opmodel.dev/modules/opm_operator release after section 2)

## Dependencies / gates

- **GATED: after add-operator-module is merged.** That change creates `modules/opm_operator/` with its `opmodel.dev/modules/opm_operator@v0` path, its `identity/` package, the `operator/` package naming the deployed operator release and image, the generated CRD and RBAC files, and the generate and drift-check tasks this change calls. Its proposal names them; task G-dep checks the merged names and substitutes any that changed before any code is written.
- **G-sandbox.** The unknowns U1 to U9 in design.md are proven in `open-platform-model/release-flow-sandbox` before section 2. The workspace rule requires release-flow changes to be proven there.
- **G-owner.** The owner approves the tag shape, the `0.1.0` start and the 0.x bump rule before section 2 merges. After the first module release, the owner limits the GHCR package `modules/opm_operator` to this repository and makes it public.
- **G-cli (last section only).** The cli installs the operator from the module, and its `deps:cascade` and release-pin check no longer read `install.yaml` from operator releases.
- **join-release-cascade (opm-operator, open).** If it has merged, its `notify-downstream` job is rekeyed in section 2. Section 4's module notify needs `.github` `cascade-notify.yml` on `main`.

Out of scope: transferring ownership of the operator's own instance (another change owns it; the instance stays CLI-owned and the operator never reconciles it), the cli's install, pins and release-pin check, workspace `RELEASING.md` rows, and opmodel.dev reading the module's bundle.

## Capabilities

### New Capabilities

- `operator-module-release`: the module as its own release unit. It covers the package, tag shape and bump rule, identity advance, release gate, publish, signing, install manifest, image-bump PR and cascade notify.

### Modified Capabilities

- `release-automation`: the root package's changelog leaves out module-only commits, and the final publish job requires the assets each package attaches. After the last section the operator release attaches no `install.yaml`.
- `container-image-publish`: the image job gates on the operator package's own `release_created`. The release install manifest requirement is removed in the last section.
- `deps-cascade`: the task also moves the module's core and catalog pins, and the pin report lists them as shipped.
- `docs-bundle`: the repository declares a second bundle, `opm-operator-module`, published at module releases.

## Impact

- `release-please-config.json`, `.release-please-manifest.json` (written by release-please only).
- `.github/workflows/release.yml`: the existing jobs' `if`, the new identity-advance step, `module-publish`, `module-publish-docs`, `module-publish-release`, `module-image-pr` and `module-notify`; also `docs.yml`, `lint.yml` and a new `module-image.yml`.
- `.github/scripts/release-guard.sh` and `image-tag-guard.sh`: read-only modes and required assets per tag shape. New `hack/module-release-check.sh`, `hack/module-image.sh` and its offline test, plus their `.tasks` entries.
- `.tasks/cascade/` (`cascade.sh`, `pins.sh`, `classes`, `test.sh`) and `docs-kit.cue`.
- Last section: `docs/site/start/install-the-operator.md`, `README.md`, `AGENTS.md`.
- No API type, CRD, controller or reconcile phase changes.
- Cross-repo, not edited here: the cli's install and release-pin check, `.github`'s resolver and notify payload, workspace `RELEASING.md`, opmodel.dev.
