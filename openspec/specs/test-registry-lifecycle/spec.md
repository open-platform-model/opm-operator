## Purpose

Define the local OCI registry lifecycle used by integration and e2e tests that
exercise the real `KernelModuleRenderer`. This keeps CUE-native module
resolution testable in developer and CI environments without depending on the
public `ghcr.io/open-platform-model` registry, while letting stub-based specs
run in minimal environments that lack a container runtime.

## ADDED Requirements

### Requirement: Local OCI registry for e2e tests
A local OCI registry MUST be available for e2e tests that exercise the real
`KernelModuleRenderer`. The registry MUST be a `registry:2` container published on
`localhost:$(REGISTRY_PORT)` (default `5000`). Lifecycle is operator-driven via
the Taskfile (`.tasks/registry.yaml`) rather than in-process auto-start,
keeping the integration suite runnable in environments without a container
runtime. Tests that require the registry MUST call `skipIfNoTestRegistry()`
which skips the spec when `CUE_REGISTRY` lacks a `testing.opmodel.dev`
mapping OR when no container tool is on `PATH`.

#### Scenario: Operator starts registry via Taskfile
- **WHEN** the operator runs `task registry:start`
- **THEN** an `opm-registry` container (image `registry:2`) is created/started on `localhost:$(REGISTRY_PORT)` (default `5000`)

#### Scenario: Operator publishes fixture module
- **WHEN** the operator runs `task registry:publish-test-module` after `task registry:start`
- **THEN** the fixture module is published to the local registry so registry-dependent tests can resolve it

#### Scenario: Test opts into registry via guard helper
- **WHEN** a registry-dependent spec calls `skipIfNoTestRegistry()` at setup
- **THEN** the spec skips if the registry is not configured/available and otherwise proceeds

### Requirement: CUE_REGISTRY configuration for tests
Tests that reach the registry MUST use a `CUE_REGISTRY` value that maps
`testing.opmodel.dev` to the local registry AND maps `opmodel.dev` to the local
registry (the workspace catalog is published to the same local registry in dev,
not to `ghcr.io/open-platform-model`). This separates test module namespace
(`testing.opmodel.dev`) from catalog namespace (`opmodel.dev`) while pointing
both at the local registry.

Example (matches the `CUE_REGISTRY` default in `Taskfile.yml`):

```
testing.opmodel.dev=localhost:5000+insecure,opmodel.dev=localhost:5000+insecure,registry.cue.works
```

#### Scenario: Both namespaces resolve locally
- **WHEN** `CUE_REGISTRY` maps both `testing.opmodel.dev` and `opmodel.dev` to the local registry
- **THEN** CUE resolves fixture modules and catalog imports from the local registry rather than from any public host

### Requirement: Test module publication
The fixture module at `test/fixtures/modules/hello/` MUST be publishable to the
local registry as `testing.opmodel.dev/modules/hello@v0` version `v0.0.1`. The
`registry:publish-test-module` task MUST use a task-local `CUE_REGISTRY` so it
cannot be broken by an unrelated shell export (shell `CUE_REGISTRY` values
that omit `testing.opmodel.dev=` would otherwise cause a 401 against a
non-local host).

#### Scenario: Publish task uses task-local CUE_REGISTRY
- **WHEN** `task registry:publish-test-module` runs in a shell whose `CUE_REGISTRY` lacks `testing.opmodel.dev=`
- **THEN** the task still publishes to the local registry because it sets its own `CUE_REGISTRY` instead of inheriting the shell value

#### Scenario: Fixture published at expected coordinates
- **WHEN** `task registry:publish-test-module` succeeds
- **THEN** `testing.opmodel.dev/modules/hello@v0` version `v0.0.1` is available in the local registry

### Requirement: Test fixture module path
The fixture at `test/fixtures/modules/hello/cue.mod/module.cue` MUST use module
path `testing.opmodel.dev/modules/hello@v0`. The fixture's catalog imports
(`opmodel.dev/core/v1alpha1@v1`, `opmodel.dev/opm/v1alpha1@v1`) MUST remain
unchanged — they resolve from the local registry via the `opmodel.dev=`
mapping.

#### Scenario: Fixture module path isolated from public catalog
- **WHEN** the fixture `module.cue` declares `module: "testing.opmodel.dev/modules/hello@v0"`
- **THEN** it does not conflict with the public `opmodel.dev` catalog namespace and publishes/resolves cleanly from the local registry

#### Scenario: Fixture catalog imports unchanged
- **WHEN** the fixture imports catalog packages
- **THEN** it continues to use `opmodel.dev/core/v1alpha1@v1` and `opmodel.dev/opm/v1alpha1@v1`, which resolve via the local registry mapping

### Requirement: End-to-end integration tests
At least one integration test MUST exercise the real renderer
(`render.KernelModuleRenderer`) against the local OCI registry, materializing a
platform from the real catalog, to validate the registry-backed render pipeline:
module acquisition → kernel `SynthesizeRelease` → `Compile` → rendered resources
with inventory entries. The test MUST resolve the catalog from the materialized
platform (the same path the `PlatformReconciler` uses) rather than copying
catalog sources into `test/fixtures/`, so it tracks production composition
automatically. Full apply → `Ready=True` on a live cluster is covered by the
Kind-backed `test/e2e` suite, not this integration-tier test.

#### Scenario: Real-renderer pipeline validated against the registry
- **WHEN** the integration test runs with the local registry available
- **THEN** it constructs `render.KernelModuleRenderer` with a kernel-materialized platform, renders a ModuleRelease, and the rendered resources carry inventory entries and the runtime-identity labels (`managed-by = opm-controller`, non-empty release uuid)

#### Scenario: Catalog resolved from the materialized platform
- **WHEN** the integration test materializes the platform
- **THEN** the catalog is resolved from the registry via the kernel rather than a copy under `test/fixtures/`, so the test automatically tracks production composition

### Requirement: Skip when registry unavailable
Registry-dependent tests MUST skip gracefully (via Ginkgo's `Skip()`) when any
of the following is true: `CUE_REGISTRY` is unset; `CUE_REGISTRY` does not
contain a `testing.opmodel.dev=` mapping; or no container runtime (`docker` or
`podman`) is on `PATH`. This keeps stub-based specs runnable in minimal CI
environments.

#### Scenario: CUE_REGISTRY missing testing mapping
- **WHEN** `CUE_REGISTRY` is unset or lacks `testing.opmodel.dev=`
- **THEN** `skipIfNoTestRegistry()` invokes `Skip()` with a clear message and stub-based specs continue to run

#### Scenario: No container runtime on PATH
- **WHEN** neither `docker` nor `podman` is on `PATH`
- **THEN** `skipIfNoTestRegistry()` invokes `Skip()` with a clear message and stub-based specs continue to run

## Scenarios

### Happy path integration

1. Operator runs `task registry:start && task registry:publish-test-module` before
   invoking `go test`
2. Test invokes `skipIfNoTestRegistry()` and proceeds (registry reachable)
3. Test materializes a platform from the real catalog via the kernel
   (`SynthesizePlatform` → `Materialize`) and seeds the platform store
4. Test constructs `render.KernelModuleRenderer` and renders
   `testing.opmodel.dev/modules/hello@v0`
5. CUE resolves both `testing.opmodel.dev` and `opmodel.dev` from the local
   registry
6. Rendered resources carry inventory entries and runtime-identity labels
   (`managed-by = opm-controller`, non-empty release uuid)
7. Full apply → `Ready=True` on a live cluster is covered by the Kind-backed
   `test/e2e` suite

### No registry available

1. `skipIfNoTestRegistry()` observes missing `testing.opmodel.dev=` mapping
   or absent container tool
2. Registry-dependent specs skip with a clear message
3. Stub-based specs continue to run
