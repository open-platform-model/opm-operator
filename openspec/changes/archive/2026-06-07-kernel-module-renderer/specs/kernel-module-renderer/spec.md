## ADDED Requirements

### Requirement: Render a ModuleRelease through the kernel

The operator SHALL provide a `KernelModuleRenderer` that implements the existing `ModuleRenderer` interface and renders a `ModuleRelease` entirely through the library kernel. Given a name, namespace, module path, module version, and optional values, it SHALL: read the current `*MaterializedPlatform` from the platform store; acquire the target module via the module-acquisition helper; convert the supplied `*RawValues` to a `cue.Value` (or no values, applying `#config` defaults); call `Kernel.SynthesizeRelease`; call `Kernel.Compile` with the materialized platform and a runtime identity; and return a `RenderResult` whose resources are adapted from the compiled output. The legacy `*provider.Provider` parameter SHALL be ignored.

#### Scenario: Renders resources from a materialized platform

- **WHEN** the store holds a materialized platform and `RenderModule` is called for a module resolvable in the registry
- **THEN** it returns a `RenderResult` whose `Resources` correspond to the kernel's compiled output
- **AND** each resource carries the release, component, and transformer provenance from the compiled item

#### Scenario: Values are applied when supplied

- **WHEN** non-nil `RawValues` are passed
- **THEN** they are compiled to a `cue.Value` and supplied to `SynthesizeRelease`
- **AND** when no values are supplied the module's `#config` defaults apply

### Requirement: Gate rendering on a materialized platform

When the platform store holds no materialized platform, `KernelModuleRenderer.RenderModule` SHALL return a typed `ErrPlatformNotReady` and SHALL NOT acquire a module, synthesize, or compile. This slice defines the renderer's error; mapping it to a custom-resource condition is out of scope here.

#### Scenario: Empty store yields ErrPlatformNotReady

- **WHEN** `RenderModule` is called while the store holds no platform
- **THEN** it returns `ErrPlatformNotReady`
- **AND** no module acquisition, synthesis, or compile is attempted

### Requirement: Adapt compiled output to operator resources

The operator SHALL provide `core.ResourceFromCompiled` converting a library `*core.Compiled` to an operator `*core.Resource` by copying the CUE value and the release, component, and transformer provenance. Inventory entries SHALL be built from the adapted resources using the existing inventory bridge.

#### Scenario: Compiled item maps to a resource

- **WHEN** a library `Compiled` with a value and provenance is adapted
- **THEN** the resulting `core.Resource` carries the same value, release, component, and transformer
- **AND** an inventory entry can be built from it via the existing `ToUnstructured` path

### Requirement: Renderer is built but not wired

This slice SHALL NOT change which renderer the reconcilers use. `cmd/main.go` SHALL continue to wire `RegistryRenderer`, and reconcile behavior SHALL be unchanged. `KernelModuleRenderer` SHALL be exercised directly by tests.

#### Scenario: Reconcile behavior unchanged

- **WHEN** the operator runs with this slice applied
- **THEN** the ModuleRelease reconciler still uses the legacy renderer
- **AND** `KernelModuleRenderer` is reachable only through direct construction in tests
