## ADDED Requirements

### Requirement: The render reports the module's declared version

Both kernel renderers (`KernelModuleRenderer` for a ModuleInstance, `KernelPackageRenderer` for a ModulePackage) SHALL report on `RenderResult.ModuleVersion` the version the rendered instance's source module declares in `#module.metadata.version`, as that module spells it (bare SemVer, for example `0.1.0`), read through the library's public `opm/schema` paths the same way on both paths. A version that cannot be read as a concrete string SHALL be reported as `""` and SHALL NOT fail the render.

#### Scenario: A rendered ModuleInstance reports its module's version

- **WHEN** `RenderModule` renders a module whose metadata declares `version: "0.0.12"`
- **THEN** the result's `ModuleVersion` is `0.0.12`

#### Scenario: A rendered ModulePackage reports the version of the module its instance renders

- **WHEN** `KernelPackageRenderer.Render` renders a package whose instance imports a module declaring `version: "0.0.12"`
- **THEN** the result's `ModuleVersion` is `0.0.12`

#### Scenario: An unreadable version is reported empty

- **WHEN** the instance's `#module.metadata.version` is missing, not concrete, or not a string
- **THEN** `ModuleVersion` is `""` and the render succeeds
