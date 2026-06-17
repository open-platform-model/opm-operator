## MODIFIED Requirements

### Requirement: Local OCI registry for e2e tests
A local OCI registry MUST be available for e2e tests that exercise the real
`KernelModuleRenderer`. The registry MUST be a `registry:2` container published on
`localhost:$(REGISTRY_PORT)` (default `5000`). Lifecycle is operator-driven via
the Taskfile (`.tasks/registry.yaml`) rather than in-process auto-start,
keeping the integration suite runnable in environments without a container
runtime. Tests that require the registry MUST call `skipIfNoTestRegistry()`
which skips the spec when `CUE_REGISTRY` lacks an `opmodel.dev=` mapping to the
local registry OR when no container tool is on `PATH`.

#### Scenario: Operator starts registry via Taskfile
- **WHEN** the operator runs `task registry:start`
- **THEN** an `opm-registry` container (image `registry:2`) is created/started on `localhost:$(REGISTRY_PORT)` (default `5000`)

#### Scenario: Operator publishes fixture module
- **WHEN** the operator runs `task module:publish` after `task registry:start`
- **THEN** the fixture module is published to the local registry so registry-dependent tests can resolve it

#### Scenario: Test opts into registry via guard helper
- **WHEN** a registry-dependent spec calls `skipIfNoTestRegistry()` at setup
- **THEN** the spec skips if the registry is not configured/available and otherwise proceeds

### Requirement: CUE_REGISTRY configuration for tests
Tests that reach the registry MUST use a `CUE_REGISTRY` value that maps
`opmodel.dev` to the local registry. The example test modules now share the
public `opmodel.dev` namespace (under the `modules/test/` prefix) with the
catalog and core, so a single `opmodel.dev=` mapping resolves the fixture, the
catalog, and core from the local registry. The local registry MUST hold the
kernel-era modules the fixture and the kernel resolve through that mapping:
`opmodel.dev/core@v0` and `opmodel.dev/catalogs/opm@v0` (at the versions pinned
by the fixture's `cue.mod/module.cue`). The catalog is published to the local
registry from the adjacent `catalog_opm` repo (`task publish VERSION=…`); the
operator repo documents this prerequisite alongside the registry lifecycle
tasks.

Example (matches the `CUE_REGISTRY` default in `Taskfile.yml`; the legacy
`testing.opmodel.dev=` mapping is retained for backward compatibility but is no
longer required by any fixture):

```
opmodel.dev=localhost:5000+insecure,testing.opmodel.dev=localhost:5000+insecure,registry.cue.works
```

#### Scenario: Namespace resolves locally
- **WHEN** `CUE_REGISTRY` maps `opmodel.dev` to the local registry
- **THEN** CUE resolves the fixture module (`opmodel.dev/modules/test/hello@v0`), `opmodel.dev/core@v0`, and `opmodel.dev/catalogs/opm@v0` from the local registry rather than from any public host

#### Scenario: Catalog present enables kernel materialization
- **WHEN** `opmodel.dev/catalogs/opm@v0` is published to the local registry at the fixture-pinned version
- **THEN** the kernel-renderer integration spec materializes a platform subscribed to `opmodel.dev/catalogs/opm` and proceeds instead of skipping

### Requirement: Test module publication
The fixture module at `test/fixtures/modules/hello/` MUST be publishable to the
local registry as `opmodel.dev/modules/test/hello@v0` version `v0.0.2`. The
publish task MUST use a task-local `CUE_REGISTRY` so it cannot be broken by an
unrelated shell export (shell `CUE_REGISTRY` values that omit `opmodel.dev=`
would otherwise cause a 401 against a non-local host). Version `v0.0.1` (the
retired old-era artifact) MUST NOT be republished or referenced by tests; the
bump keeps stale local registries and CUE module caches inert.

#### Scenario: Publish task uses task-local CUE_REGISTRY
- **WHEN** `task module:publish` runs in a shell whose `CUE_REGISTRY` lacks `opmodel.dev=`
- **THEN** the task still publishes to the local registry because it sets its own `CUE_REGISTRY` instead of inheriting the shell value

#### Scenario: Fixture published at expected coordinates
- **WHEN** `task module:publish` succeeds
- **THEN** `opmodel.dev/modules/test/hello@v0` version `v0.0.2` is available in the local registry

#### Scenario: Stale old-era artifact is inert
- **WHEN** a local registry or CUE module cache still holds the old-era `v0.0.1` artifact
- **THEN** no test resolves it, because all registry-backed specs reference `v0.0.2`

### Requirement: Test fixture module path
The fixture at `test/fixtures/modules/hello/cue.mod/module.cue` MUST use module
path `opmodel.dev/modules/test/hello@v0`, and its `#Module.metadata.modulePath`
MUST be `opmodel.dev/modules/test`. The fixture MUST be authored against the
kernel-era schema: it imports `opmodel.dev/core@v0` (embedding `#Module`) and
catalog packages under `opmodel.dev/catalogs/opm/…`, with `cue.mod` dependencies
on `opmodel.dev/core@v0` and `opmodel.dev/catalogs/opm@v0` pinned by `cue mod
tidy`. The module MUST stay minimal: one component attaching the catalog's
ConfigMaps resource so the catalog's `configmap-transformer` matches it without
any workload-type labels, rendering exactly one ConfigMap.

#### Scenario: Fixture published under the public test namespace
- **WHEN** the fixture `module.cue` declares `module: "opmodel.dev/modules/test/hello@v0"`
- **THEN** it resolves under the public `opmodel.dev` namespace via the standard registry mapping (the `modules/test/` prefix scopes it to test fixtures) and publishes/resolves cleanly from the local registry

#### Scenario: Fixture is concrete-valid standalone
- **WHEN** `cue eval . --concrete` runs in `test/fixtures/modules/hello/` with the local registry mappings
- **THEN** the module evaluates with no errors, resolving full `metadata` (including `fqn` and `uuid`) and `debugValues`, confirming the kernel-era authoring is correct

### Requirement: Skip when registry unavailable
Registry-dependent tests MUST skip gracefully (via Ginkgo's `Skip()`) when any
of the following is true: `CUE_REGISTRY` is unset; `CUE_REGISTRY` does not
contain an `opmodel.dev=` mapping to the local registry; or no container runtime
(`docker` or `podman`) is on `PATH`. This keeps stub-based specs runnable in
minimal CI environments.

#### Scenario: CUE_REGISTRY missing local mapping
- **WHEN** `CUE_REGISTRY` is unset or lacks `opmodel.dev=localhost`
- **THEN** `skipIfNoTestRegistry()` invokes `Skip()` with a clear message and stub-based specs continue to run

#### Scenario: No container runtime on PATH
- **WHEN** neither `docker` nor `podman` is on `PATH`
- **THEN** `skipIfNoTestRegistry()` invokes `Skip()` with a clear message and stub-based specs continue to run
