## Purpose

Define the dependency-injection boundary for module rendering inside the
reconcile loop. The `ModuleRenderer` interface lets production code use a
registry-backed renderer while tests inject a stub that returns pre-built
`*RenderResult` values, so downstream phases (apply, prune, drift,
impersonation) can be exercised without a live OCI registry.

## ADDED Requirements

### Requirement: ModuleRenderer interface
The `internal/render` package MUST export a `ModuleRenderer` interface whose
sole method renders a module given its identifying coordinates, a values
document, and a catalog provider:

```go
type ModuleRenderer interface {
    RenderModule(ctx context.Context, name, namespace, modulePath, moduleVersion string,
        values *releasesv1alpha1.RawValues, prov *provider.Provider) (*RenderResult, error)
}
```

The package MUST also export a `RegistryRenderer` struct that implements
`ModuleRenderer` by delegating to `RenderModuleFromRegistry`.

#### Scenario: Production renderer delegates to registry
- **WHEN** code calls `(&RegistryRenderer{}).RenderModule(ctx, name, ns, path, version, values, prov)`
- **THEN** the call is delegated to `render.RenderModuleFromRegistry` with the same arguments and the result/error is returned unchanged

#### Scenario: Stub renderer returns pre-built result
- **WHEN** a test stub implementing `ModuleRenderer` is invoked with any coordinates
- **THEN** it returns the pre-built `*RenderResult` without contacting any OCI registry

### Requirement: Renderer in reconcile params
`ModuleReleaseParams` in `internal/reconcile` MUST include a `Renderer` field
of type `render.ModuleRenderer`. The reconcile loop MUST call
`params.Renderer.RenderModule(...)` instead of calling
`render.RenderModuleFromRegistry(...)` directly. The `Renderer` field MUST NOT
be nil — all callers are required to set it.

#### Scenario: Reconcile invokes injected renderer
- **WHEN** `ReconcileModuleRelease` executes the render phase
- **THEN** it calls `params.Renderer.RenderModule(...)` with the release's module coordinates, values, and provider

#### Scenario: Stub renderer drives downstream phases
- **WHEN** a test supplies a stub `Renderer` returning a fixed `*RenderResult`
- **THEN** the reconcile loop consumes that result and executes apply, prune, drift, and impersonation phases normally without OCI access

### Requirement: Renderer in controller struct
`ModuleReleaseReconciler` in `internal/controller` MUST include a `Renderer`
field of type `render.ModuleRenderer` and pass it through to
`ModuleReleaseParams` when constructing the params for each reconcile.

#### Scenario: Controller threads renderer into params
- **WHEN** the reconciler builds `ModuleReleaseParams` for a reconcile call
- **THEN** `params.Renderer` is set to `r.Renderer` rather than constructed inline

### Requirement: Production wiring
`cmd/main.go` MUST set `Renderer: &render.RegistryRenderer{}` when constructing
the `ModuleReleaseReconciler`, preserving current production behavior.

#### Scenario: Manager wires registry renderer
- **WHEN** the manager constructs `&ModuleReleaseReconciler{...}` in `cmd/main.go`
- **THEN** the struct literal includes `Renderer: &render.RegistryRenderer{}`

## Scenarios

### Production reconcile

1. Controller creates `ModuleReleaseParams` with `Renderer: &RegistryRenderer{}`
2. Reconcile loop calls `params.Renderer.RenderModule(...)`
3. `RegistryRenderer` delegates to `RenderModuleFromRegistry`
4. Behavior is identical to the current direct call

### Test with stub renderer

1. Test creates params with a stub returning a fixed `*RenderResult`
2. Reconcile loop calls `params.Renderer.RenderModule(...)`
3. Stub returns the pre-built result without OCI registry access
4. Downstream phases (apply, prune, drift, impersonation) execute normally
