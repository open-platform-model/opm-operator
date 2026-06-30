## 1. ModulePackage fixtures (hello-web, podinfo, redis)

- [x] 1.1 Create `test/fixtures/modulepackages/hello-web/` (5 files) mirroring `hello/`: release module `opmodel.dev/releases/test/hello-web@v0`, imports module `opmodel.dev/modules/test/hello-web@v0` `v0.1.2`, `values: {replicas: 2}`, OCIRepository `hello-web-release` tag `v0.0.1`, ModulePackage `hello-web` / SA `hello-web-deploy`. (Import needs explicit `:hello_web` package qualifier — the module's CUE package is `hello_web` but the path ends in the non-identifier `hello-web`.)
- [x] 1.2 Create `test/fixtures/modulepackages/podinfo/` (5 files): release `opmodel.dev/releases/test/podinfo@v0`, module `opmodel.dev/modules/test/podinfo@v0` `v0.1.2`, `values: {replicas: 2}`, OCIRepository `podinfo-release`, ModulePackage `podinfo` / SA `podinfo-deploy`
- [x] 1.3 Create `test/fixtures/modulepackages/redis/` (5 files): release `opmodel.dev/releases/test/redis@v0`, module `opmodel.dev/modules/test/redis@v0` `v0.1.6`, `values: {persistence: {size: "1Gi"}}`, OCIRepository `redis-release`, ModulePackage `redis` / SA `redis-deploy`

## 2. ModuleInstance parity

- [x] 2.1 Create `test/fixtures/modules/hello-web/moduleinstance.yaml` mirroring `podinfo`: SA/Role/RoleBinding `hello-web-applier` (grants `apps/deployments`), ModuleInstance `hello-web` → module `opmodel.dev/modules/test/hello-web@v0` `v0.1.2`, `values: {replicas: 2}`, `prune: true`

## 3. Test wiring

- [x] 3.1 Convert the "materialized platform" context in `test/integration/reconcile/kernel_package_renderer_test.go` to a table-driven loop over `hello`, `hello-web`, `podinfo`, `redis`; keep generic assertions (≥1 resource, provenance fields, `LabelManagedBy`, `LabelModuleInstanceUUID`, inventory length); keep the empty-store negative case hello-only

## 4. Tooling + docs

- [x] 4.1 Parameterize `.tasks/release.yaml`: `PKG` var (default `hello`), derive `RELEASE_DIR`/`RELEASE_REPO`/GHCR from it, fix `apply`/`delete` to use `modulepackage.yaml`, add `publish:all`
- [x] 4.2 Fix `test/fixtures/modules/README.md`: `modulerelease.yaml` → `moduleinstance.yaml` throughout; add `hello-web` to the apply-an-example section

## 5. Validation gates

- [x] 5.1 `cue vet` each new modulepackage dir (registry-resolved) — all four pass. (`kubectl --dry-run=client` requires a reachable cluster; not run in this env — YAML/GVK structure verified instead, matches the `hello` reference.)
- [x] 5.2 `go vet` + golangci-lint (`task dev:lint`) clean; gofmt clean. (Full `task dev:test` render path is local-registry-gated and auto-skips without it; `cue vet` already proves the fixtures resolve against the registry.)
- [x] 5.3 `openspec validate add-test-fixture-modulepackage-parity` — valid
