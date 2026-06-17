## 1. Migrate existing fixtures to public path

- [x] 1.1 Update `test/fixtures/modules/hello/cue.mod/module.cue` `module:` to `opmodel.dev/modules/test/hello@v0` (preserve dep pins); update `metadata.modulePath` in `module.cue` to `opmodel.dev/modules/test`
- [x] 1.2 Update `test/fixtures/modules/hello-web/cue.mod/module.cue` + `module.cue` `metadata.modulePath` the same way
- [x] 1.3 Update `test/fixtures/modules/hello/modulerelease.yaml` `spec.module.path` to `opmodel.dev/modules/test/hello@v0`
- [x] 1.4 Update `test/fixtures/releases/hello/release.cue` import + `test/fixtures/releases/hello/cue.mod/module.cue`, and `ocirepository.yaml`/`release.yaml` path/url fields to the new path
- [x] 1.5 Update `PUBLISH_REGISTRY`/repo vars and `MODULE_DIR`/`RELEASE_REPO` defaults in `.tasks/module.yaml` and `.tasks/release.yaml` to the `opmodel.dev/modules/test` path
- [x] 1.6 Publish migrated modules to local registry and run the registry-backed integration tests to confirm unchanged behavior (`go test ./internal/controller -run TestControllers`)

## 2. Author podinfo example module

- [x] 2.1 Create `test/fixtures/modules/podinfo/` with `cue.mod/module.cue` (`module: opmodel.dev/modules/test/podinfo@v0`, version `v0.1.0`, core/catalog dep pins)
- [x] 2.2 Author `module.cue` (`#Module`, metadata, `#config` for image/tag/replicas, `debugValues`) and `components.cue` using `StatelessWorkload` with container port 9898
- [x] 2.3 Add `livenessProbe.httpGet /healthz` and `readinessProbe.httpGet /readyz` on port 9898; ensure Service is rendered
- [x] 2.4 Add `modulerelease.yaml` (+ ServiceAccount/Role/RoleBinding) referencing `opmodel.dev/modules/test/podinfo@v0`
- [x] 2.5 Publish locally and `cue vet` / materialize to confirm Deployment + Service + probes render as specified

## 3. Author redis example module

- [x] 3.1 Create `test/fixtures/modules/redis/` with `cue.mod/module.cue` (`module: opmodel.dev/modules/test/redis@v0`, version `v0.1.0`, dep pins)
- [x] 3.2 Author `module.cue` + `components.cue` using `StatefulWorkload` with headless Service and a PVC/volumeClaimTemplate
- [x] 3.3 Add exec readiness probe (`redis-cli ping`); document and wire the persistence default (ephemeral vs PVC) as an overridable `#config` field
- [x] 3.4 Add `modulerelease.yaml` (+ RBAC) referencing `opmodel.dev/modules/test/redis@v0`
- [x] 3.5 Publish locally and materialize to confirm StatefulSet + headless Service + PVC + exec probe render

## 4. e2e validation of podinfo probes

- [x] 4.1 Add a Ginkgo spec under `test/e2e/` that applies the podinfo ModuleRelease against the Kind-backed operator
- [x] 4.2 Assert the rendered Deployment's pods reach Ready within timeout via `Eventually` (proves liveness + readiness pass)
- [x] 4.3 Inspect the deployed container and assert probe paths/port match the module (`/healthz`, `/readyz`, 9898); optional port-forward curl of `/healthz` as secondary check
- [x] 4.4 Run `task dev:e2e` locally (or document skip if no Kind) and confirm the spec passes

## 5. CI: publish example modules on release

- [x] 5.1 Add a per-module publish task (e.g. in `.tasks/module.yaml` or new `.tasks/examples.yaml`) that reads each module's declared version and publishes via `cue mod publish` with `CUE_REGISTRY='opmodel.dev=ghcr.io/open-platform-model,registry.cue.works'`
- [x] 5.2 Implement publish-if-absent: detect changed module dirs via git-diff against the previous release tag; publish declared version; treat "already exists" as success
- [x] 5.3 Extend `.github/workflows/release.yml` (release-gated, after image build) to GHCR-login and run the module-publish task for all example modules
- [x] 5.4 Verify the publish step is idempotent across two consecutive release simulations (second run skips unchanged modules without failing)

## 6. CI: attach example manifests as release artifacts

- [x] 6.1 Add a task that bundles each example module's `modulerelease.yaml` (and `releases/*` manifests) into release assets (per-file and/or `opm-examples.tar.gz`)
- [x] 6.2 Extend `release.yml` to `gh release upload` the example manifest assets to the release (mirroring the `install.yaml` step)
- [x] 6.3 Confirm attached manifests reference `opmodel.dev/modules/test/<m>@v0` versions published by the same release

## 7. Documentation & finalize

- [x] 7.1 Add a short README in `test/fixtures/modules/` (or per-module) documenting how to apply each example against a running operator
- [x] 7.2 Run `task dev:fmt dev:vet dev:test` and `task dev:lint` for any Go/e2e changes; ensure no generated files were hand-edited
- [x] 7.3 Verify the change with `/opsx:verify` and update artifacts if implementation revealed gaps
