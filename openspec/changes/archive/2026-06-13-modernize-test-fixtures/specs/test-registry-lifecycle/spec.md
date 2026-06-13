# Delta: test-registry-lifecycle

## MODIFIED Requirements

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
> `configmap-transformer` match) is **not** asserted here. The acquisition shim
> (`internal/moduleacquire/shim.go`) cannot yet load a core@v0 `#Module` — its
> `import mod; mod` embedding re-evaluates the self-referential `#Module`
> metadata and is rejected as `field not allowed`. That kernel-integration fix
> (and the corresponding `releases/hello` rewrite) is owned by the
> `fix-moduleacquire-core-v0` change, which carries the acquire/render and
> release-load acceptance scenarios.
