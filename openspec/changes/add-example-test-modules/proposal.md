## Why

The repo's only end-to-end fixtures (`hello`, `hello-web`) are deliberately minimal kernel-probe modules — they render a bare ConfigMap and a bare Deployment with no Service and no probes. They are not usable as "getting started" examples, and nothing exercises real-world workload behavior (HTTP liveness/readiness, persistent storage). Newcomers have no published, consumable OPM module to apply against a running operator to see OPM work.

This change adds two real-world example modules (podinfo, redis), migrates all fixtures onto a public, consistently-resolvable module path, validates podinfo's liveness/readiness in e2e, and publishes the modules + ready-to-apply ModuleRelease manifests as release artifacts so users can `kubectl apply` an example in one step.

## What Changes

- Add a **podinfo** example module: a `StatelessWorkload` rendering a Deployment + Service with HTTP `livenessProbe` (`/healthz`) and `readinessProbe` (`/readyz`) on port 9898, plus a `ModuleRelease` manifest.
- Add a **redis** example module: a `StatefulWorkload` rendering a StatefulSet + headless Service + PVC with an exec (`redis-cli ping`) readiness probe, plus a `ModuleRelease` manifest. Persistence defaults documented (ephemeral vs PVC).
- **BREAKING** (fixture identity only, not a published API): migrate `hello`, `hello-web`, and the release fixture from `testing.opmodel.dev/modules/<m>` to `opmodel.dev/modules/test/<m>`. Updates each `module.cue` (`module:` + `metadata.modulePath`), the `release.cue` import, the `modulerelease.yaml`/`release.yaml`/`ocirepository.yaml` path fields, and the `PUBLISH_REGISTRY`/repo vars in `.tasks/module.yaml` + `.tasks/release.yaml`. Catalog/core version pins on `hello*` are preserved verbatim (regression-guard behavior is path-independent).
- Add an **e2e Ginkgo spec** that applies the podinfo `ModuleRelease`, waits for the Deployment's pods to become Ready (proving both probes pass), and asserts the rendered probe contract matches the running container.
- Extend the existing **release-please release job** to, on each release: (1) `cue mod publish` each example module to `ghcr.io/open-platform-model` (via `CUE_REGISTRY='opmodel.dev=ghcr.io/open-platform-model,registry.cue.works'`, matching `core`/`catalog`), publishing only modules whose `version:` tag is not already present; and (2) bundle the example `ModuleRelease`/`Release` manifests and upload them to the GitHub Release as assets (mirroring the existing `install.yaml` upload).

## Capabilities

### New Capabilities
- `example-test-modules`: The podinfo and redis example modules, the migration of all fixtures onto `opmodel.dev/modules/test/<m>`, the modelled workload/probe/storage behavior they exercise, and the e2e validation of podinfo liveness/readiness.
- `example-module-publishing`: CI publication of the example modules as CUE modules to GHCR on release (independent per-module versioning, idempotent re-publish) and the upload of example ModuleRelease manifests as GitHub Release artifacts.

### Modified Capabilities
<!-- No existing spec-level requirements change. The release job is extended additively; existing release-automation / container-image-publish requirements are unaffected. -->

## Impact

- **Fixtures**: `test/fixtures/modules/{podinfo,redis}/` (new), `test/fixtures/modules/{hello,hello-web}/` + `test/fixtures/releases/hello/` (path migration).
- **CUE module identity**: new public path `opmodel.dev/modules/test/<m>@v0` resolving to `ghcr.io/open-platform-model` — no consumer config change beyond the standard `opmodel.dev` mapping already used for `core`/`catalog`.
- **CI**: `.github/workflows/release.yml` gains module-publish + manifest-upload steps; new Taskfile targets in `.tasks/module.yaml` (and possibly a new `.tasks/examples.yaml`) for per-module publish + version detection.
- **Tests**: new e2e spec under `test/e2e/`; depends on Kind + the local registry + published `core`/`catalog`/example modules.
- **No Go API/CRD changes**: `api/v1alpha1` untouched; no `dev:manifests`/`dev:generate` needed. SemVer: MINOR for the operator (new CI capability, additive).
