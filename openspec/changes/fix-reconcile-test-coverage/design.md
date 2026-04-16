## Context

The reconcile loop calls `render.RenderModuleFromRegistry()` directly — a hard
dependency on OCI registry availability. This blocks all integration tests that
need successful rendering to exercise downstream phases (apply, prune, drift,
impersonation). The test fixture module uses `opmodel.dev/test/hello@v0` which
conflicts with the public catalog namespace.

## Goals / Non-Goals

**Goals:**

- Decouple the reconcile loop from the concrete render implementation via
  interface injection.
- Enable stub-based testing for all post-render phases.
- Provide real end-to-end test coverage via a local OCI registry.
- Separate test module namespace (`testing.opmodel.dev`) from production
  (`opmodel.dev`).

**Non-Goals:**

- Changing the render pipeline or CUE evaluation.
- Mocking the Kubernetes API server (envtest handles this).
- BundleRelease test coverage.

## Design

### ModuleRenderer interface

**File:** `internal/render/renderer.go` (new)

```go
type ModuleRenderer interface {
    RenderModule(ctx context.Context, name, namespace, modulePath, moduleVersion string,
        values *releasesv1alpha1.RawValues, prov *provider.Provider) (*RenderResult, error)
}

type RegistryRenderer struct{}

func (r *RegistryRenderer) RenderModule(ctx context.Context,
    name, namespace, modulePath, moduleVersion string,
    values *releasesv1alpha1.RawValues, prov *provider.Provider,
) (*RenderResult, error) {
    return RenderModuleFromRegistry(ctx, name, namespace, modulePath, moduleVersion, values, prov)
}
```

### Params wiring

`ModuleReleaseParams` gains a `Renderer render.ModuleRenderer` field. The
reconcile loop calls `params.Renderer.RenderModule(...)` instead of the
package-level function. No nil fallback — all callers must set it.

`ModuleReleaseReconciler` gains the same field, passed through to params.
`cmd/main.go` wires `&render.RegistryRenderer{}`.

### Test stub

Tests inject a `stubRenderer` that returns a pre-built `*render.RenderResult`
(or an error). The stub constructs a single ConfigMap resource via
`cuecontext.New().CompileString(...)`, matching what the existing test provider
would produce.

### Registry configuration

CUE_REGISTRY for tests:
```
opmodel.dev=ghcr.io/open-platform-model,testing.opmodel.dev=localhost:<port>+insecure,registry.cue.works
```

- `opmodel.dev` → public GHCR (catalog core, OPM types, provider transformers)
- `testing.opmodel.dev` → local registry (test fixture modules only)
- `registry.cue.works` → CUE standard library fallback

### Fixture module path

`test/fixtures/modules/hello/cue.mod/module.cue`:
```
module: "testing.opmodel.dev/test/hello@v0"
```

Imports stay as `opmodel.dev/core/...` and `opmodel.dev/opm/...` — they resolve
from the public GHCR registry.

### Test classification

| Category | Renderer | Count |
|----------|----------|-------|
| Phase-specific (apply, prune, drift, impersonation, counters, events, no-op) | Stub | ~26 |
| Error path (resolution failed, render failed) | Stub returning error | ~2 |
| End-to-end (full pipeline) | RegistryRenderer | 1-2 |

### Registry lifecycle in test suites

`BeforeSuite`:
1. Start `registry:2` container on a dynamic port
2. Set `CUE_REGISTRY` env var
3. Run `cue mod tidy && cue mod publish v0.0.1` in the fixture directory

`AfterSuite`:
1. Remove the registry container

E2e tests skip when no container tool is available.
