## Test Registry Lifecycle

### ADDED: Local OCI registry in integration test suites

The integration test suite (`test/integration/reconcile/`) MUST start a local
OCI registry container in `BeforeSuite` and remove it in `AfterSuite`.

The registry MUST be a `registry:2` container on a non-conflicting port.

### ADDED: CUE_REGISTRY configuration for tests

The test suite MUST set `CUE_REGISTRY` to:
```
opmodel.dev=ghcr.io/open-platform-model,testing.opmodel.dev=localhost:<port>+insecure,registry.cue.works
```

This separates test modules (`testing.opmodel.dev`) from the public catalog
(`opmodel.dev`).

### ADDED: Test module publication

The test suite MUST publish the fixture module at
`test/fixtures/modules/hello/` to the local registry as
`testing.opmodel.dev/test/hello@v0` version `v0.0.1`.

Publication uses `cue mod tidy && cue mod publish v0.0.1` with the test
`CUE_REGISTRY`.

### MODIFIED: Test fixture module path

The fixture at `test/fixtures/modules/hello/cue.mod/module.cue` MUST use module
path `testing.opmodel.dev/test/hello@v0`.

The fixture's catalog imports (`opmodel.dev/core/v1alpha1@v1`,
`opmodel.dev/opm/v1alpha1@v1`) MUST remain unchanged — they resolve from the
public GHCR registry.

### ADDED: End-to-end integration tests

At least one integration test MUST use `render.RegistryRenderer` with the real
catalog provider to validate the full pipeline: synthesis → OCI resolution →
render → SSA apply.

### ADDED: Skip when container tool unavailable

Registry-dependent tests MUST skip gracefully when no container runtime
(docker/podman) is available, using Ginkgo's `Skip()`.

### Scenarios

#### Happy path e2e

1. BeforeSuite starts local registry and publishes test module
2. Test creates ModuleRelease CR with `testing.opmodel.dev/test/hello@v0`
3. Reconcile uses `RegistryRenderer` + real catalog provider
4. CUE resolves module from local registry, catalog from GHCR
5. Resources rendered and applied via SSA
6. `Ready=True`, inventory populated

#### No container tool

1. BeforeSuite detects no docker/podman available
2. Registry-dependent tests skip with clear message
3. Stub-based tests still run normally
