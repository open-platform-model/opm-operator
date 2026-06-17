## Context

The existing fixtures (`hello`, `hello-web`) are kernel-probe minimal: a bare ConfigMap and a bare Deployment with no Service and no probes, pinned to specific `core`/`catalog` versions as regression guards. There is no published, consumable example a newcomer can apply to a running operator.

Confirmed facts that shape this design:
- `core` and `catalog` publish to GHCR with `CUE_REGISTRY='opmodel.dev=ghcr.io/open-platform-model,registry.cue.works'`. So `opmodel.dev/*` already resolves to `ghcr.io/open-platform-model`. The example modules' public path and their registry are therefore the *same* decision: authoring under `opmodel.dev/modules/test/<m>` and publishing to GHCR is consistent with `core`/`catalog`, and consumers need no extra config.
- The catalog container schema supports `livenessProbe`/`readinessProbe`/`startupProbe` and a `HealthCheckTrait` wired into both the deployment and statefulset transformers, so podinfo probes and redis exec probes are modellable today.
- The release job already renders and `gh release upload`s `install.yaml`. Attaching example manifests is the same pattern.
- The local e2e path maps both `testing.opmodel.dev` and `opmodel.dev` to `localhost:5000`, so migrating fixture paths does not break local registry tests.

## Goals / Non-Goals

**Goals:**
- Two real-world example modules (podinfo stateless web, redis stateful) covering the stateless-web and stateful-storage axes.
- All fixtures on one public, consistently-resolvable path `opmodel.dev/modules/test/<m>@v0`.
- e2e proof that podinfo's modelled liveness/readiness probes actually work.
- CI publishes example modules to GHCR and attaches ready-to-apply ModuleRelease manifests to GitHub Releases, on release only.

**Non-Goals:**
- No combined podinfo+redis multi-component module in this change (podinfo `--cache-server` integration is a deliberate follow-up; the two modules stay standalone here).
- No Go API/CRD changes; `api/v1alpha1` is untouched.
- No new public-docs site content (`opmodel.dev/`) — that is a separate repo and follow-up.
- No change to how the operator resolves modules at runtime.

## Decisions

### D1: Migrate all fixtures to `opmodel.dev/modules/test/<m>` (not just the new ones)
Per the explore decision, `hello`/`hello-web` move too, so every fixture shares one public path and the registry mapping is uniform. Regression-guard behavior is path-independent, and the pinned `core`/`catalog` dep versions are preserved verbatim — only path strings change. *Alternative considered:* keep `hello*` on `testing.opmodel.dev` and only put new modules on the public path. Rejected: two path conventions is more confusing for a "getting started" surface, and the migration is mechanical.

### D2: Independent per-module versioning via `module.cue`, publish-if-absent
Each module declares its own `@v0` major and semver; the author bumps the version when changing a module. CI reads the declared version and publishes only if that tag is absent in the registry, treating "already exists" as success. *Alternatives considered:* (a) tag modules with the operator's release version — rejected, conflates two version streams and forces a republish every operator release; (b) a separate release-please component for modules — rejected for now as more machinery than warranted (Principle VII), but left as a future option if module churn grows.

### D3: Changed-module detection — git-diff with tolerate-already-exists fallback
The publish step determines which modules changed by diffing each module dir against the previous release tag; for any changed module it publishes the declared version. The "publish only if version absent" check (D2) is the safety net that makes the step idempotent even if diff detection is imprecise. *Alternative considered:* always attempt publish and rely solely on tolerate-already-exists — simpler but noisier and masks a real failure mode (forgetting to bump a changed module's version). Git-diff makes "changed but version not bumped" visible.

### D4: Publish on release only, reusing the existing release job
Module publish + manifest upload are added as steps to the `image-release` job (or a sibling job gated on `releases_created == 'true'`), after image build. This reuses the existing GHCR login and release-tag checkout. *Alternative considered:* a standalone workflow on tag push — rejected to keep one release surface and avoid duplicate auth/checkout.

### D5: podinfo modelled with HTTP probes; redis with exec probe + PVC
podinfo: `StatelessWorkload` → Deployment + Service, `livenessProbe.httpGet /healthz`, `readinessProbe.httpGet /readyz`, port 9898. redis: `StatefulWorkload` → StatefulSet + headless Service + PVC, exec readiness `redis-cli ping`. These exercise the two transformer paths and both probe styles.

### D6: e2e validates probes via pod readiness
The e2e spec applies the podinfo ModuleRelease and uses `Eventually` to assert the Deployment's pods reach Ready — which is only possible if both probes pass — then inspects the container to confirm the rendered probe paths/port match the module. This proves "works as intended and modelled" without brittle in-cluster HTTP curling as the primary signal (curl via port-forward is an optional secondary assertion).

## Risks / Trade-offs

- **Migrating `hello*` disturbs regression guards** → Mitigation: change only path strings; preserve all dep pins and guard comments; run the existing registry-backed integration tests after migration to confirm behavior is unchanged.
- **`cue mod publish` immutability causes CI failure on re-run** → Mitigation: D2/D3 publish-if-absent + tolerate-already-exists.
- **Changed module but forgotten version bump silently skips publish** → Mitigation: D3 git-diff detection surfaces a changed-but-unbumped module so CI can warn/fail rather than silently no-op.
- **e2e flakiness pulling real podinfo/redis images on Kind** → Mitigation: pin image digests/tags; generous `Eventually` timeouts; gate behind the existing e2e job which already tolerates image pulls.
- **GHCR auth/visibility for published modules** → Mitigation: reuse the release job's `GITHUB_TOKEN` GHCR login; ensure the `ghcr.io/open-platform-model` packages are public so anonymous `cue` resolution works for getting-started users.

## Migration Plan

1. Author new modules and migrate fixture paths locally; verify local publish + integration tests against `localhost:5000`.
2. Add CI publish + manifest-upload steps gated on `releases_created`.
3. First release after merge publishes module `v0.1.0`s and attaches manifests.
4. Rollback: CI steps are additive and release-gated; reverting the workflow change disables publishing without affecting the operator. Already-published immutable module versions remain (harmless).

## Open Questions

- Exact changed-module detection mechanism in CI (git-diff range vs `oras`/`crane` registry probe for the tag). Leaning git-diff-since-previous-tag with the absent-version check as the authority.
- Whether redis should default to ephemeral `emptyDir` (simplest demo) or a real PVC (more realistic) — to be settled when authoring the module; spec requires the default be explicit and overridable either way.
- Whether to also publish to `registry.cue.works` later for reach (out of scope now; GHCR only).
