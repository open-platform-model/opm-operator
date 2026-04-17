## Module Renderer Interface

### ADDED: ModuleRenderer interface

The `internal/render` package MUST export a `ModuleRenderer` interface:

```go
type ModuleRenderer interface {
    RenderModule(ctx context.Context, name, namespace, modulePath, moduleVersion string,
        values *releasesv1alpha1.RawValues, prov *provider.Provider) (*RenderResult, error)
}
```

The package MUST export a `RegistryRenderer` struct that implements
`ModuleRenderer` by delegating to `RenderModuleFromRegistry`.

### ADDED: Renderer in reconcile params

`ModuleReleaseParams` in `internal/reconcile` MUST include a `Renderer` field
of type `render.ModuleRenderer`.

The reconcile loop MUST call `params.Renderer.RenderModule(...)` instead of
`render.RenderModuleFromRegistry(...)` directly.

The `Renderer` field MUST NOT be nil — all callers are required to set it.

### ADDED: Renderer in controller struct

`ModuleReleaseReconciler` in `internal/controller` MUST include a `Renderer`
field of type `render.ModuleRenderer` and pass it through to
`ModuleReleaseParams`.

### MODIFIED: Production wiring

`cmd/main.go` MUST set `Renderer: &render.RegistryRenderer{}` when constructing
the `ModuleReleaseReconciler`.

### Scenarios

#### Production reconcile

1. Controller creates `ModuleReleaseParams` with `Renderer: &RegistryRenderer{}`
2. Reconcile loop calls `params.Renderer.RenderModule(...)`
3. `RegistryRenderer` delegates to `RenderModuleFromRegistry`
4. Behavior is identical to the current direct call

#### Test with stub renderer

1. Test creates params with a stub returning a fixed `*RenderResult`
2. Reconcile loop calls `params.Renderer.RenderModule(...)`
3. Stub returns the pre-built result without OCI registry access
4. Downstream phases (apply, prune, drift, impersonation) execute normally
