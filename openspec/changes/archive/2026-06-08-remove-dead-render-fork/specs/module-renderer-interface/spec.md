## MODIFIED Requirements

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

Production wires `KernelModuleRenderer` (see `platform-gated-rendering`); tests
inject a stub that returns a pre-built `*RenderResult` without contacting an OCI
registry. The package no longer exports a registry-backed renderer struct — the
fork implementation (`RegistryRenderer` delegating to `RenderModuleFromRegistry`)
is deleted.

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
- **THEN** it calls `params.Renderer.RenderModule(...)` with the release's module coordinates, values, and provider

#### Scenario: Stub renderer drives downstream phases
- **WHEN** a test supplies a stub `Renderer` returning a fixed `*RenderResult`
- **THEN** the reconcile loop consumes that result and executes apply, prune, drift, and impersonation phases normally without OCI access

## REMOVED Requirements

### Requirement: Production wiring

**Reason**: This requirement mandated `cmd/main.go` set `Renderer: &render.RegistryRenderer{}`, which is no longer true (the manager wires `KernelModuleRenderer`) and whose type is deleted in this slice. The production-wiring contract is superseded by the `platform-gated-rendering` requirement that the manager wires the kernel-backed renderer.
**Migration**: See `platform-gated-rendering` ("ModuleRelease renders through the kernel against the materialized platform"), which specifies the production renderer wiring. The `ModuleRenderer`/`ReleaseRenderer` interfaces and the reconcile-params/controller-struct requirements of this capability are preserved.
