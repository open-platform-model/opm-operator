## Why

Enhancement 0028 makes the operator an OPM module that `opm operator install` pulls from a registry. `add-operator-module` puts the module's source in this repository. Nothing publishes it yet. 0028:D1 makes the module a release unit of its own here, with its own versions, release PR and tags. 0028:D2:R9 makes each module release publish the kubectl install manifest. 0028:D7 places the module release between the operator release and the cli. Until this change lands, the cli has no module version to pin, and the module's image reference moves only by hand.

## What Changes

- **A second release-please package for the module.** It has its own version train starting at `0.1.0`, its own release PR and tags shaped `opm_operator-vX.Y.Z`. No operator tag (`vX.Y.Z`) and no operator release branch (`release/vX.Y`) can take that shape (0028:D1:R9). The package overrides the top-level 0.x bump settings: a breaking change raises the minor and every other release raises the patch (0028:D1:R16). The operator package stops counting commits under the module directory, so a module change never cuts an operator release.
- **Existing release jobs key on the operator package alone.** Today they gate on `releases_created`, which a module-only release also sets. They move to the root package's `release_created` and `tag_name`. The example publisher's previous-tag lookup stops matching module tags.
- **Identity advance on the module's release PR.** The release workflow writes the release PR's version into the module's `identity.Version` with `opm module version set`, as catalog_opm does for its catalog. This is the only writer, so the published module is exactly the tagged source (0028:D1:R10).
- **A module release gate in the `Lint` job (G1).** On the module's release PR it refuses: a development pin, an image reference that is not a published operator release at the digest GHCR serves, an `identity.Version` that is not the proposed version, a version whose major disagrees with the `v0` path, `1.0.0` before operator GA (0028:D1:R13), and a module whose CRDs or controller RBAC differ from the operator release it deploys (the drift check `add-operator-module` provides).
- **A module publish job.** It publishes `opmodel.dev/modules/opm_operator` through `opm module publish` with the CLI from `.opm-cli-version`, and a re-run of the same release succeeds without republishing. It signs the artifact with cosign keyless and attests build provenance, like the image (0028:D1:R4). It renders the published module at its default values into `install.yaml` and attaches it to the module's draft release (0028:D2:R9). Then it publishes the draft. No other workflow or task in this repository publishes under the module's path (0028:D1:R5).
- **An image-bump PR after every operator release (0028:D1:R10, 0028:D7).** Once an operator release is public, a job opens or updates one PR. The PR names the new image by tag and digest in the module and regenerates the module's CRDs and RBAC from that tag. Its title is `fix(deps)`, or `fix(deps)!` when the operator release declares a break or a CRD stops serving a version. A `workflow_dispatch` path recovers a missed run.
- **The module in the cascade and the docs (0028:D7).** `deps:cascade` moves the module's core and catalog pins as shipped pins. A module release notifies the cli once its artifact is public. A docs-kit bundle `opm-operator-module` publishes the module's `#config` documentation at the module's own version.
- **GATED, last section: the operator release stops publishing `install.yaml` (0028:D2:R13).** It lands only after the cli no longer embeds or downloads the operator release's manifest and its cascade no longer resolves the operator through that asset. The operator's notify job then stops dispatching to the cli. **BREAKING** for anyone who downloads `install.yaml` from an operator release: the manifest moves to the module release.

Release class: the module's own train starts at `0.1.0`. This change's commits are `ci`, `docs` and `chore`, all hidden, so it cuts no operator release before or after GA. The last section removes an operator release asset. Open question 1 in design.md asks whether that section must carry a `!`.

Delivery: one PR per section (cli needs a published opmodel.dev/modules/opm_operator release after section 2)

## Dependencies / gates

- **GATED: after add-operator-module is merged.** That change creates the module directory, its `opmodel.dev/modules/opm_operator@v0` path and identity package, the file that names the operator image, and the generate and drift-check tasks this change calls. Task G-dep checks this and records the real names before section 1. This proposal assumes the directory `module/`. If the merged change differs, the gate step substitutes the real names before any code is written.
- **G-sandbox.** The unknowns U1 to U9 in design.md are proven in `open-platform-model/release-flow-sandbox` before section 2. The workspace rule requires release-flow changes to be proven there.
- **G-owner.** The owner approves the tag shape and the first module release. After that release, the owner limits the GHCR package `modules/opm_operator` to this repository and makes it public (0028:D1:R5).
- **G-cli (last section only).** The cli installs the operator from the module, and its `deps:cascade` and G1 no longer read `install.yaml` from operator releases.
- **join-release-cascade (opm-operator, open).** If it has merged, its `notify-downstream` job is rekeyed in section 2. Section 4's module notify needs `.github` `cascade-notify.yml` on `main`.

Out of scope: ownership transfer (another change owns it), the cli's install, pins and G1 (0028:D1:R17 to R19, D3), workspace `RELEASING.md` rows, and opmodel.dev reading the module's bundle.

## Capabilities

### New Capabilities

- `operator-module-release`: the module as its own release unit. It covers the package, tag shape and bump rule, identity advance, release gate, publish, signing, install manifest, image-bump PR, cascade notify and docs bundle.

### Modified Capabilities

- `release-automation`: the root package's changelog leaves out module-only commits, and the final publish job requires the assets each package attaches. After the last section the operator release attaches no `install.yaml`.
- `container-image-publish`: the image job gates on the operator package's own `release_created`. The release install manifest requirement is removed in the last section.
- `deps-cascade`: the task also moves the module's core and catalog pins, and the pin report lists them as shipped.
- `docs-bundle`: the repository declares a second bundle, `opm-operator-module`, published at module releases.

## Impact

- `release-please-config.json`, `.release-please-manifest.json`.
- `.github/workflows/release.yml`: the existing jobs' `if`, the new identity-advance step, `module-gate`, `module-publish`, `module-publish-docs`, `module-publish-release`, `module-image-pr` and `module-notify`; also `docs.yml` and `lint.yml`.
- `.github/scripts/release-guard.sh`: required assets per tag shape. New `hack/module-release-check.sh` and `hack/module-image.sh`, plus their `.tasks` entries.
- `.tasks/cascade/` (`cascade.sh`, `pins.sh`) and `docs-kit.cue`.
- Last section: `docs/site/start/install-the-operator.md`, `README.md`, `AGENTS.md`.
- No API type, CRD, controller or reconcile phase changes.
- Cross-repo, not edited here: the cli's install and G1, `.github`'s resolver and notify payload, workspace `RELEASING.md`, opmodel.dev.
