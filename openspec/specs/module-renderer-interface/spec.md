## Purpose

Define the dependency-injection boundary for module rendering inside the
reconcile loop. The `ModuleRenderer` interface lets production code use the
kernel-backed renderer (`KernelModuleRenderer`) while tests inject a stub that
returns pre-built `*RenderResult` values, so downstream phases (apply, prune,
drift, impersonation) can be exercised without a live OCI registry.

## ADDED Requirements

### Requirement: ModuleRenderer interface
The `internal/render` package MUST export a `ModuleRenderer` interface whose
sole method renders a module given its identifying coordinates and a values
document:

```go
type ModuleRenderer interface {
    RenderModule(ctx context.Context, name, namespace, modulePath, moduleVersion string,
        values *releasesv1alpha1.RawValues) (*RenderResult, error)
}
```

Production wires `KernelModuleRenderer` (see `platform-gated-rendering`); tests
inject a stub that returns a pre-built `*RenderResult` without contacting an OCI
registry. The package exports no registry-backed renderer struct and no longer
references a catalog provider — transformers come from the materialized platform
via the kernel.

#### Scenario: Stub renderer returns pre-built result
- **WHEN** a test stub implementing `ModuleRenderer` is invoked with any coordinates
- **THEN** it returns the pre-built `*RenderResult` without contacting any OCI registry

### Requirement: Renderer in reconcile params
`ModuleReleaseParams` in `internal/reconcile` MUST include a `Renderer` field
of type `render.ModuleRenderer`, and the reconcile loop MUST render through
`params.Renderer.RenderModule(...)` rather than constructing a renderer inline.
The `Renderer` field MUST NOT be nil — all callers are required to set it.

#### Scenario: Reconcile invokes injected renderer
- **WHEN** `ReconcileModuleRelease` executes the render phase
- **THEN** it calls `params.Renderer.RenderModule(...)` with the release's module coordinates and values

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

## Scenarios

### Production reconcile

1. Controller creates `ModuleReleaseParams` with `Renderer: &KernelModuleRenderer{...}`
2. Reconcile loop calls `params.Renderer.RenderModule(...)`
3. `KernelModuleRenderer` renders through the library kernel against the materialized platform (see `platform-gated-rendering`)
4. The reconcile loop consumes the resulting `*RenderResult` for apply, prune, drift, and impersonation

### Test with stub renderer

1. Test creates params with a stub returning a fixed `*RenderResult`
2. Reconcile loop calls `params.Renderer.RenderModule(...)`
3. Stub returns the pre-built result without OCI registry access
4. Downstream phases (apply, prune, drift, impersonation) execute normally
