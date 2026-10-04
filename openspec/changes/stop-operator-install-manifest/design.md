## Context

See proposal.md, "Why". This change touches only release automation and docs. No reconcile phase changes: Source, Render, Apply, Prune and Status behave the same. No API type or CRD changes.

It was split out of `release-operator-module`, whose last section it was, so that change could be archived without waiting for the cli. What it builds on, from that change: module releases tagged `opm_operator-vX.Y.Z` attach `install.yaml` rendered from the module at its defaults, with the Namespace and the CRDs first; module releases are not prereleases, so the newest one is GitHub's Latest release (that change's sandbox unknown U8); an operator release opens the image-bump PR that moves the module to it, and a module release notifies the cli.

## Goals / Non-Goals

**Goals:**

- One install manifest: the module's render.
- No window in which a released cli or the cascade looks for an asset that is gone.

**Non-Goals:**

- The cli's install, its `task operator:sync`, its cascade and release-pin check, and the resolver's published check. Those are their own changes and this change's gates.
- Retiring `config/default`. It still installs the operator for development and e2e.

## Decisions

### Order: the cli and the resolver move first

The cli downloads `releases/download/<operator tag>/install.yaml` for `opm operator install --version` (cli `internal/operator/fetch.go:13` holds the base URL, line 23 builds the asset URL) and in `task operator:sync` (cli `Taskfile.yml:497`). The resolver counts an operator release as published only when that asset downloads (`.github` `lib/query.sh:88-90`). Removing the asset first would break `--version` for every released cli, and would stop the cli's cascade from ever seeing a new operator release. Hence gates G-cli and G-resolver.

### What the operator release still does

`image-release` keeps building, signing and attesting the image. It drops "Render digest-pinned install manifest" and "Upload install.yaml to the draft release". `release-guard.sh publish` requires `opm-examples.tar.gz` for `v*` tags, and `install.yaml` for `opm_operator-v*` tags as before. `task operator:installer` stays, for a local `dist/install.yaml`.

### The operator release stops notifying the cli

After this change an operator release reaches the cli only through the module: `module-image-pr` opens the module's image PR, its merge releases the module, and `module-notify` dispatches to the cli. The operator's own `notify-downstream` stops dispatching to the cli. If the shared notify workflow needs a target, the job stays with no cli target; otherwise it is removed. Task 2.2 records which.

### The kustomize comparison stays

add-operator-module requires a test that the module's controller pod spec and metrics Service equal a kustomize build of `config/default`, "while `config/manager` and `config/default` still produce the operator's install manifest". After this change they no longer produce a release manifest, but they still install the operator in development (`task operator:installer`, `task operator:controller:install`) and in e2e. A drift between the two would make e2e test a pod the module does not ship. So the test stays, and the requirement's opening condition is reworded to that reason. The requirement's text in this change's delta is the one add-operator-module proposed on 2026-10-04; task G-dep re-reads the archived text and carries the same rewording onto it.

### Install docs

The kubectl path becomes `releases/latest/download/install.yaml`, which is the newest module release once module releases are Latest (U8 in `release-operator-module`). The warning about `releases/latest` serving the retired v0.7.5 manifest is removed. The page names no module version, so it does not go stale. It states the one-time migration: the module's Deployment selector differs from the earlier manifest's, so the first apply over an operator installed from an operator release's `install.yaml` must go through `opm operator install`, which recreates the Deployment once and deletes the three old `*-rolebinding` objects. A plain `kubectl apply` of the module's manifest over the old install fails on the immutable selector.

## Risks / Trade-offs

- [A user's tooling downloads `install.yaml` from an operator release] → It gets a 404 from the first release after this change. The `!` puts the removal in the CHANGELOG, and the PR body carries the migration note: download from the newest module release, or use `opm operator install`.
- [After operator GA both units could be Latest] → Open question in `release-operator-module`. Until GA every operator release is a prerelease.
- [An operator release is published while the image-bump PR is not yet merged] → The cli sees it only after the module release. That is the intended order: the cli never installs an operator that no module release deploys.

## Migration Plan

One PR after G-cli and G-resolver. Rollback: revert it, and the next operator release attaches `install.yaml` again.

## Research & Decisions

### Whether the removal carries a `!`
**Context**: The removal changes what an operator release publishes.
**Explored**: the "Beta prerelease line" requirement (a break during beta ships only as a `!` in the PR title); a hidden `ci` title.
**Decision**: `feat(release)!:`.
**Rationale**: A hidden type cuts no release and leaves no CHANGELOG entry, so users would learn of the missing asset from a 404.
