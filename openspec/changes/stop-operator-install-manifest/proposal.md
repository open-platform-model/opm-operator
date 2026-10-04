## Why

After `release-operator-module`, every module release attaches `install.yaml`, the operator module rendered at its defaults, and every operator release still attaches its own `install.yaml`, rendered from `config/default`. Two manifests for one install drift apart, and only the module's is what `opm operator install` applies once the cli installs from the module. The operator release should stop publishing its manifest, so the module's render is the only install manifest, as the owner decided on 2026-10-04 (install pulls the module from a registry; no embedded manifest).

The removal cannot come first. The released cli downloads the operator release's `install.yaml` for `opm operator install --version` (cli `internal/operator/fetch.go:13,23`) and in `task operator:sync`, and the cascade resolver counts an operator release as published only when that asset downloads (`.github` `lib/query.sh:88-90`). Removing it before the cli moves would break `--version` for every released cli and hide every new operator release from the cli's cascade. So this change is gated on the cli.

This completes, for the operator, the amendment to `0021:D4` recorded in enhancement 0021: the install manifest is the module's render, not an operator release asset.

## What Changes

- **The operator release stops attaching `install.yaml`.** `image-release` no longer renders `config/default` into a release asset or uploads it. `release-guard.sh publish` requires only `opm-examples.tar.gz` for `v*` tags. `task operator:installer` stays a development task. **BREAKING** for anyone who downloads `install.yaml` from an operator release: the manifest moves to the module release.
- **The operator release stops notifying the cli.** The cli now learns of an operator release through the module: the image-bump PR, the module release and its notify.
- **The kubectl install path reads the module release.** The install page and `README.md` point at `install.yaml` of the newest module release, which is GitHub's Latest release, and say that the first apply over an operator installed from an earlier manifest needs `opm operator install`.
- **The module's pod-shape test keeps its reason.** The add-operator-module test comparing the module's controller pod and Service with a kustomize build of `config/default` stays, because `config/default` still installs the operator for development and e2e; its requirement's opening condition is reworded to say so.

Release class: MAJOR after GA (an operator release asset is removed); on the beta line it ships as the next `-beta.N`, through a `!` in the PR title, as the "Beta prerelease line" requirement says a break ships during beta. The commit is `feat(release)!:`, a visible type, so the CHANGELOG announces the removal and links the PR's migration note.

## Dependencies / gates

- **GATED: after release-operator-module is archived**, so its spec changes are in `openspec/specs/` and this change's MODIFIED requirements match them, and after add-operator-module is archived, for the `operator-module` capability.
- **G-cli.** A published cli installs the operator from the module; the cli on `main` no longer embeds or downloads an operator release's `install.yaml`; the cli's `deps:cascade` no longer resolves the operator with `--asset install.yaml`; the cli's release-pin check keys on the module pin.
- **G-resolver.** The `.github` resolver no longer counts an operator release as published by its `install.yaml` (`lib/query.sh:88-90`), or every operator release after this change looks unpublished to it. That is a `.github` change, not made here.

Out of scope: the cli's and the resolver's own changes, workspace `RELEASING.md`, and opmodel.dev's install pages.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `container-image-publish`: the release install manifest requirement is removed.
- `release-automation`: the operator's final publish job requires only `opm-examples.tar.gz`; the beta and draft-upload scenarios stop naming the operator's `install.yaml`.
- `operator-module`: the pod-shape comparison with `config/default` is kept for development and e2e installs, not for a release manifest.

## Impact

- `.github/workflows/release.yml` (`image-release`, `notify-downstream`), `.github/scripts/release-guard.sh`.
- `docs/site/start/install-the-operator.md`, `README.md`, `AGENTS.md`.
- No API type, CRD, controller or reconcile phase changes.
