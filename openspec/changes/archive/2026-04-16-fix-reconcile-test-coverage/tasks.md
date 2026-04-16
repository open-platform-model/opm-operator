## Tasks

### ModuleRenderer Interface

- [x] Create `internal/render/renderer.go` with `ModuleRenderer` interface and
      `RegistryRenderer` implementation
- [x] Add `Renderer render.ModuleRenderer` to `ModuleReleaseParams` in
      `internal/reconcile/modulerelease.go`
- [x] Replace `render.RenderModuleFromRegistry(...)` call with
      `params.Renderer.RenderModule(...)` in reconcile loop
- [x] Add `Renderer` field to `ModuleReleaseReconciler` in
      `internal/controller/modulerelease_controller.go`, pass through to params
- [x] Wire `Renderer: &render.RegistryRenderer{}` in `cmd/main.go`
- [x] Verify: `go build ./...`

### Test Stub Renderer

- [x] Add `stubRenderer` struct and `stubRenderResult` helper to
      `test/integration/reconcile/suite_test.go`
- [x] Update `reconcileParams()` to include `Renderer` field
- [x] Update `reconcileParamsWithConfig()` in `impersonation_test.go`
- [x] Add same stub to `internal/controller/testhelpers_test.go`
- [x] Add `Renderer` to all `ModuleReleaseReconciler` constructions in
      `internal/controller/modulerelease_reconcile_test.go`

### Convert PIt → It (integration tests)

- [x] `test/integration/reconcile/reconcile_test.go`: convert 3 PIt → It,
      adjust error expectations for stub
- [x] `test/integration/reconcile/drift_test.go`: convert 4 PIt → It
- [x] `test/integration/reconcile/impersonation_test.go`: convert 4 PIt → It
- [x] Verify: `go test ./test/integration/reconcile/... -count=1`

### Convert PIt → It (controller tests)

- [x] `internal/controller/modulerelease_reconcile_test.go`: convert 17 PIt → It
- [x] Verify: `go test ./internal/controller/ -count=1`

### Fixture and Registry Setup

- [x] Update `test/fixtures/modules/hello/cue.mod/module.cue`: module path
      → `testing.opmodel.dev/test/hello@v0`
- [x] Update `config/samples/releases_v1alpha1_modulerelease.yaml`: module path
      → `testing.opmodel.dev/test/hello@v0`
- [x] Update `Makefile` CUE_REGISTRY to include
      `testing.opmodel.dev=localhost:$(REGISTRY_PORT)+insecure`
      (already present from prior registry lifecycle commits)
- [x] Add registry lifecycle helpers to
      `test/integration/reconcile/suite_test.go` (start, publish, teardown)
      → delegated to Makefile targets `make start-registry` /
      `make publish-test-module`; integration suite uses skip-guard pattern
      rather than auto-start to keep suite runnable w/o container runtime
- [x] Add skip guard for tests requiring container tool
      (`skipIfNoTestRegistry` in `registry_helpers_test.go`)

### End-to-End Integration Test

- [x] Add happy-path e2e test using `RegistryRenderer` + real catalog provider
      → `test/integration/reconcile/e2e_registry_test.go` has three specs:
      CUE-only resolution, registry-error surfacing, and full reconcile using
      `catalog.LoadProvider("../../../catalog", "kubernetes")` + SSA apply into
      envtest. Catalog is referenced directly (no copy) so tests track the
      production composition automatically.
- [x] Verify: full `make test` passes

### Validation

- [x] `make fmt vet lint` — 0 issues
- [x] `make test` — all pass, 0 pending
