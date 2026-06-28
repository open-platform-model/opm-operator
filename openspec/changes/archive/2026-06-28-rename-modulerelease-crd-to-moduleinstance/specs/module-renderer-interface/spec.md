## MODIFIED Requirements

### Requirement: Renderer in reconcile params
`ModuleInstanceParams` in `internal/reconcile` MUST include a `Renderer` field
of type `render.ModuleRenderer`, and the reconcile loop MUST render through
`params.Renderer.RenderModule(...)` rather than constructing a renderer inline.
The `Renderer` field MUST NOT be nil — all callers are required to set it.

#### Scenario: Reconcile invokes injected renderer
- **WHEN** `ReconcileModuleInstance` executes the render phase
- **THEN** it calls `params.Renderer.RenderModule(...)` with the instance's module coordinates and values

#### Scenario: Stub renderer drives downstream phases
- **WHEN** a test supplies a stub `Renderer` returning a fixed `*RenderResult`
- **THEN** the reconcile loop consumes that result and executes apply, prune, drift, and impersonation phases normally without OCI access

### Requirement: Renderer in controller struct
`ModuleInstanceReconciler` in `internal/controller` MUST include a `Renderer`
field of type `render.ModuleRenderer` and pass it through to
`ModuleInstanceParams` when constructing the params for each reconcile.

#### Scenario: Controller threads renderer into params
- **WHEN** the reconciler builds `ModuleInstanceParams` for a reconcile call
- **THEN** `params.Renderer` is set to `r.Renderer` rather than constructed inline
