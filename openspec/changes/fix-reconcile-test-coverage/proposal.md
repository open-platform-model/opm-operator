## Why

The ModuleRelease integration test suite has zero real coverage of the reconcile
loop's core phases (render, apply, prune, drift detection, impersonation). All 28
phase-specific tests are marked `PIt` (pending) because `ReconcileModuleRelease`
calls `render.RenderModuleFromRegistry()` which requires a live OCI registry.
Without one, module resolution fails before any downstream phase can execute. Two
of the pending impersonation tests would fail for the wrong reason even if
un-pended — resolution error fires before the impersonation check.

## What Changes

- Introduce a `ModuleRenderer` interface in `internal/render/` so tests can inject
  a stub renderer that returns pre-built `*RenderResult` without needing a registry.
  Production uses `RegistryRenderer` (wraps existing `RenderModuleFromRegistry`).
- Add `Renderer` field to `ModuleReleaseParams` and `ModuleReleaseReconciler`.
- Update 28 pending tests across 4 test files to use the stub and convert
  `PIt` → `It`.
- Update the test fixture module path from `opmodel.dev/test/hello@v0` to
  `testing.opmodel.dev/test/hello@v0` so it resolves from a local registry
  without conflicting with the public `opmodel.dev` catalog.
- Add local OCI registry lifecycle to the integration test suite (`BeforeSuite`
  / `AfterSuite`) for end-to-end tests that validate the full synthesis →
  resolution → render pipeline.
- Add 1-2 end-to-end integration tests using the real registry + real catalog
  provider.

## Capabilities

### New Capabilities

- `module-renderer-interface`: Dependency injection boundary for module rendering
  in the reconcile loop, enabling stub-based testing of post-render phases.
- `test-registry-lifecycle`: Local OCI registry setup in integration test suites
  for end-to-end validation of CUE-native module resolution.

### Modified Capabilities

_(none — no spec-level behavior changes, only testability improvements)_

## Impact

- **Go code**: New interface + production impl in `internal/render/`, new field
  in `internal/reconcile/` params and `internal/controller/` reconciler struct,
  updated wiring in `cmd/main.go`.
- **Tests**: 28 tests un-pended across `test/integration/reconcile/` (3 files)
  and `internal/controller/` (1 file). New test stubs and helpers.
- **Test fixture**: `test/fixtures/modules/hello/cue.mod/module.cue` path updated.
- **Makefile**: `CUE_REGISTRY` updated with `testing.opmodel.dev` mapping.
- **Sample CR**: Module path updated to `testing.opmodel.dev/test/hello@v0`.
- **Dependencies**: No new Go dependencies. Container runtime (docker/podman)
  required for registry-based e2e tests (skipped when unavailable).
- **SemVer**: PATCH — no API changes, internal testability improvement only.

## Scope Boundary

**In scope:**

- `ModuleRenderer` interface and `RegistryRenderer` implementation
- Stub renderer for tests
- Wiring through params → controller → cmd/main.go
- Converting all 28 `PIt` → `It` with stub renderer
- Fixture module path migration to `testing.opmodel.dev`
- Local registry lifecycle in test suites
- 1-2 e2e integration tests with real registry

**Out of scope:**

- Changing the render pipeline itself (no CUE evaluation changes)
- BundleRelease test coverage (separate concern)
- Controller envtest tests for phases that need real K8s resources beyond
  ConfigMaps (e.g., Deployments, Services)
- CI/CD pipeline changes for registry availability
