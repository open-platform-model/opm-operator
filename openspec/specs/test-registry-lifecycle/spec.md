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
registry. This separates the test module namespace (`testing.opmodel.dev`) from
the catalog/core namespace (`opmodel.dev`) while pointing both at the local
registry. The local registry MUST hold the kernel-era modules the fixture and
the kernel resolve through the `opmodel.dev=` mapping: `opmodel.dev/core@v0`
and `opmodel.dev/catalogs/opm@v0` (at the versions pinned by the fixture's
`cue.mod/module.cue`). The catalog is published to the local registry from the
adjacent `catalog_opm` repo (`task publish VERSION=…`); the operator repo
documents this prerequisite alongside the registry lifecycle tasks.

Example (matches the `CUE_REGISTRY` default in `Taskfile.yml`):

```
testing.opmodel.dev=localhost:5000+insecure,opmodel.dev=localhost:5000+insecure,registry.cue.works
```

#### Scenario: Both namespaces resolve locally
- **WHEN** `CUE_REGISTRY` maps both `testing.opmodel.dev` and `opmodel.dev` to the local registry
- **THEN** CUE resolves the fixture module, `opmodel.dev/core@v0`, and `opmodel.dev/catalogs/opm@v0` from the local registry rather than from any public host

#### Scenario: Catalog present enables kernel materialization
- **WHEN** `opmodel.dev/catalogs/opm@v0` is published to the local registry at the fixture-pinned version
- **THEN** the kernel-renderer integration spec materializes a platform subscribed to `opmodel.dev/catalogs/opm` and proceeds instead of skipping

### Requirement: Test module publication
The fixture module at `test/fixtures/modules/hello/` MUST be publishable to the
local registry as `testing.opmodel.dev/modules/hello@v0` version `v0.0.2`. The
`registry:publish-test-module` task MUST use a task-local `CUE_REGISTRY` so it
cannot be broken by an unrelated shell export (shell `CUE_REGISTRY` values
that omit `testing.opmodel.dev=` would otherwise cause a 401 against a
non-local host). Version `v0.0.1` (the retired old-era artifact) MUST NOT be
republished or referenced by tests; the bump keeps stale local registries and
CUE module caches inert.

#### Scenario: Publish task uses task-local CUE_REGISTRY
- **WHEN** `task registry:publish-test-module` runs in a shell whose `CUE_REGISTRY` lacks `testing.opmodel.dev=`
- **THEN** the task still publishes to the local registry because it sets its own `CUE_REGISTRY` instead of inheriting the shell value

#### Scenario: Fixture published at expected coordinates
- **WHEN** `task registry:publish-test-module` succeeds
- **THEN** `testing.opmodel.dev/modules/hello@v0` version `v0.0.2` is available in the local registry

#### Scenario: Stale old-era artifact is inert
- **WHEN** a local registry or CUE module cache still holds the old-era `v0.0.1` artifact
- **THEN** no test resolves it, because all registry-backed specs reference `v0.0.2`

### Requirement: Test fixture module path
The fixture at `test/fixtures/modules/hello/cue.mod/module.cue` MUST use module
path `testing.opmodel.dev/modules/hello@v0`. The fixture MUST be authored
against the kernel-era schema: it imports `opmodel.dev/core@v0` (embedding
`#Module`) and catalog packages under `opmodel.dev/catalogs/opm/…`, with
`cue.mod` dependencies on `opmodel.dev/core@v0` and
`opmodel.dev/catalogs/opm@v0` pinned by `cue mod tidy`. The module MUST stay
minimal: one component attaching the catalog's ConfigMaps resource so the
catalog's `configmap-transformer` matches it without any workload-type labels,
rendering exactly one ConfigMap.

#### Scenario: Fixture module path isolated from public catalog
- **WHEN** the fixture `module.cue` declares `module: "testing.opmodel.dev/modules/hello@v0"`
- **THEN** it does not conflict with the public `opmodel.dev` namespace and publishes/resolves cleanly from the local registry

#### Scenario: Fixture is concrete-valid standalone
- **WHEN** `cue eval . --concrete` runs in `test/fixtures/modules/hello/` with the local registry mappings
- **THEN** the module evaluates with no errors, resolving full `metadata` (including `fqn` and `uuid`) and `debugValues`, confirming the kernel-era authoring is correct

> Note: end-to-end loading of the fixture through the kernel
> (`moduleacquire.Acquire` → `Kernel.Compile`, including the
> `configmap-transformer` match) is asserted by the `fix-moduleacquire-core-v0`
> change, not here. Acquisition now delegates to
> `Kernel.LoadModuleFromRegistry` (the self-referential `#Module` metadata that
> the old embedding shim collapsed is preserved by loading the module as the
> main module); this spec asserts only that the fixture is concrete-valid
> standalone.

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
