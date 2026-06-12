## MODIFIED Requirements

### Requirement: ModuleRenderer interface

The `internal/render` package MUST export a `ModuleRenderer` interface whose sole method renders a module given its identifying coordinates and a values document:

```go
type ModuleRenderer interface {
    RenderModule(ctx context.Context, name, namespace, modulePath, moduleVersion string,
        values *releasesv1alpha1.RawValues) (*RenderResult, error)
}
```

Production wires `KernelModuleRenderer` (see `platform-gated-rendering`); tests inject a stub that returns a pre-built `*RenderResult` without contacting an OCI registry. The package exports no registry-backed renderer struct and no longer references a catalog provider — transformers come from the materialized platform via the kernel.

#### Scenario: Stub renderer returns pre-built result

- **WHEN** a test stub implementing `ModuleRenderer` is invoked with any coordinates
- **THEN** it returns the pre-built `*RenderResult` without contacting any OCI registry

### Requirement: Renderer in reconcile params

`ModuleReleaseParams` in `internal/reconcile` MUST include a `Renderer` field of type `render.ModuleRenderer`, and the reconcile loop MUST render through `params.Renderer.RenderModule(...)` rather than constructing a renderer inline. The `Renderer` field MUST NOT be nil — all callers are required to set it.

#### Scenario: Reconcile invokes injected renderer

- **WHEN** `ReconcileModuleRelease` executes the render phase
- **THEN** it calls `params.Renderer.RenderModule(...)` with the release's module coordinates and values

#### Scenario: Stub renderer drives downstream phases

- **WHEN** a test supplies a stub `Renderer` returning a fixed `*RenderResult`
- **THEN** the reconcile loop consumes that result and executes apply, prune, drift, and impersonation phases normally without OCI access
