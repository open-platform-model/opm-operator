## ADDED Requirements

### Requirement: Render a ModuleInstance through the kernel

The operator SHALL provide a `KernelModuleRenderer` that implements the existing `ModuleRenderer` interface and renders a `ModuleInstance` entirely through the library kernel. Given a name, namespace, module path, module version, and optional values, it SHALL: read the current `*MaterializedPlatform` from the platform store; acquire the target module via the module-acquisition helper; convert the supplied `*RawValues` to a `cue.Value` (or no values, applying `#config` defaults); call `Kernel.SynthesizeInstance`; call `Kernel.Compile` with the materialized platform and a runtime identity; and return a `RenderResult` whose resources are adapted from the compiled output. The legacy `*provider.Provider` parameter SHALL be ignored.

#### Scenario: Renders resources from a materialized platform

- **WHEN** the store holds a materialized platform and `RenderModule` is called for a module resolvable in the registry
- **THEN** it returns a `RenderResult` whose `Resources` correspond to the kernel's compiled output
- **AND** each resource carries the instance, component, and transformer provenance from the compiled item

#### Scenario: Values are applied when supplied

- **WHEN** non-nil `RawValues` are passed
- **THEN** they are compiled to a `cue.Value` and supplied to `SynthesizeInstance`
- **AND** when no values are supplied the module's `#config` defaults apply

## REMOVED Requirements

### Requirement: Render a ModuleRelease through the kernel

**Reason**: Renamed for Release→Instance vocabulary (enhancement 0002 D11/D12).
**Migration**: See the ADDED requirement "Render a ModuleInstance through the kernel".
