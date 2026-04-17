## Test Registry Lifecycle

### ADDED: Local OCI registry for e2e tests

A local OCI registry MUST be available for e2e tests that exercise the real
`RegistryRenderer`. The registry MUST be a `registry:2` container published on
`localhost:$(REGISTRY_PORT)` (default `5000`).

Lifecycle is operator-driven via the `Makefile` rather than in-process auto-
start, keeping the integration suite runnable in environments without a
container runtime:

- `make start-registry` — creates/starts the `opm-registry` container
- `make publish-test-module` — publishes the fixture module to the registry
- Tests that require the registry MUST call `skipIfNoTestRegistry()` which
  skips the spec when `CUE_REGISTRY` lacks a `testing.opmodel.dev` mapping OR
  when no container tool is on `PATH`

### ADDED: CUE_REGISTRY configuration for tests

Tests that reach the registry MUST use a `CUE_REGISTRY` value that maps:

- `testing.opmodel.dev` → the local registry
- `opmodel.dev` → the local registry (the workspace catalog is published to
  the same local registry in dev; not `ghcr.io/open-platform-model`)

Example (matches `Makefile:211`):

```
testing.opmodel.dev=localhost:5000+insecure,opmodel.dev=localhost:5000+insecure,registry.cue.works
```

This separates test module namespace (`testing.opmodel.dev`) from catalog
namespace (`opmodel.dev`) while pointing both at the local registry.

### ADDED: Test module publication

The fixture module at `test/fixtures/modules/hello/` MUST be publishable to
the local registry as `testing.opmodel.dev/test/hello@v0` version `v0.0.1`.

The `publish-test-module` target MUST use a target-local `CUE_REGISTRY` so it
cannot be broken by an unrelated shell export (shell `CUE_REGISTRY` values
that omit `testing.opmodel.dev=` would otherwise cause a 401 against a
non-local host).

### MODIFIED: Test fixture module path

The fixture at `test/fixtures/modules/hello/cue.mod/module.cue` MUST use module
path `testing.opmodel.dev/test/hello@v0`.

The fixture's catalog imports (`opmodel.dev/core/v1alpha1@v1`,
`opmodel.dev/opm/v1alpha1@v1`) MUST remain unchanged — they resolve from the
local registry via the `opmodel.dev=` mapping.

### ADDED: End-to-end integration tests

At least one integration test MUST use `render.RegistryRenderer` with the real
catalog provider loaded from `catalog/` (the workspace composition module) to
validate the full pipeline: synthesis → OCI resolution → render → SSA apply.

The e2e test MUST reference `catalog/` by relative path rather than copying
it into `test/fixtures/`, so the test tracks the production composition
automatically.

### ADDED: Skip when registry unavailable

Registry-dependent tests MUST skip gracefully (via Ginkgo's `Skip()`) when any
of the following is true:

- `CUE_REGISTRY` is unset
- `CUE_REGISTRY` does not contain a `testing.opmodel.dev=` mapping
- No container runtime (`docker` or `podman`) is on `PATH`

This keeps stub-based specs runnable in minimal CI environments.

### Scenarios

#### Happy path e2e

1. Operator runs `make start-registry && make publish-test-module` before
   invoking `go test`
2. Test invokes `skipIfNoTestRegistry()` and proceeds (registry reachable)
3. Test loads the real `kubernetes` provider via
   `catalog.LoadProvider("../../../catalog", "kubernetes")`
4. Test creates ModuleRelease CR with `testing.opmodel.dev/test/hello@v0`
5. Reconcile uses `RegistryRenderer` + real catalog provider
6. CUE resolves both `testing.opmodel.dev` and `opmodel.dev` from the local
   registry
7. Resources rendered and applied via SSA into the envtest API server
8. `Ready=True`, inventory populated

#### No registry available

1. `skipIfNoTestRegistry()` observes missing `testing.opmodel.dev=` mapping
   or absent container tool
2. Registry-dependent specs skip with a clear message
3. Stub-based specs continue to run
