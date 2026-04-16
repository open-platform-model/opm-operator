## Tasks

### ModuleRenderer Interface

- [ ] Create `internal/render/renderer.go` with `ModuleRenderer` interface and
      `RegistryRenderer` implementation
- [ ] Add `Renderer render.ModuleRenderer` to `ModuleReleaseParams` in
      `internal/reconcile/modulerelease.go`
- [ ] Replace `render.RenderModuleFromRegistry(...)` call with
      `params.Renderer.RenderModule(...)` in reconcile loop
- [ ] Add `Renderer` field to `ModuleReleaseReconciler` in
      `internal/controller/modulerelease_controller.go`, pass through to params
- [ ] Wire `Renderer: &render.RegistryRenderer{}` in `cmd/main.go`
- [ ] Verify: `go build ./...`

### Test Stub Renderer

- [ ] Add `stubRenderer` struct and `stubRenderResult` helper to
      `test/integration/reconcile/suite_test.go`
- [ ] Update `reconcileParams()` to include `Renderer` field
- [ ] Update `reconcileParamsWithConfig()` in `impersonation_test.go`
- [ ] Add same stub to `internal/controller/testhelpers_test.go`
- [ ] Add `Renderer` to all `ModuleReleaseReconciler` constructions in
      `internal/controller/modulerelease_reconcile_test.go`

### Convert PIt → It (integration tests)

- [ ] `test/integration/reconcile/reconcile_test.go`: convert 3 PIt → It,
      adjust error expectations for stub
- [ ] `test/integration/reconcile/drift_test.go`: convert 4 PIt → It
- [ ] `test/integration/reconcile/impersonation_test.go`: convert 4 PIt → It
- [ ] Verify: `go test ./test/integration/reconcile/... -count=1`

### Convert PIt → It (controller tests)

- [ ] `internal/controller/modulerelease_reconcile_test.go`: convert 17 PIt → It
- [ ] Verify: `go test ./internal/controller/ -count=1`

### Fixture and Registry Setup

- [ ] Update `test/fixtures/modules/hello/cue.mod/module.cue`: module path
      → `testing.opmodel.dev/test/hello@v0`
- [ ] Update `config/samples/releases_v1alpha1_modulerelease.yaml`: module path
      → `testing.opmodel.dev/test/hello@v0`
- [ ] Update `Makefile` CUE_REGISTRY to include
      `testing.opmodel.dev=localhost:$(REGISTRY_PORT)+insecure`
- [ ] Add registry lifecycle helpers to
      `test/integration/reconcile/suite_test.go` (start, publish, teardown)
- [ ] Add skip guard for tests requiring container tool

### End-to-End Integration Test

- [ ] Add happy-path e2e test using `RegistryRenderer` + real catalog provider
- [ ] Verify: full `make test` passes

### Validation

- [ ] `make fmt vet lint` — 0 issues
- [ ] `make test` — all pass, 0 pending
