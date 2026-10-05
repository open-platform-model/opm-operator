## ADDED Requirements

### Requirement: The render reports the skew policy it rendered under

Both kernel renderers (`KernelModuleRenderer` for a ModuleInstance, `KernelPackageRenderer` for a ModulePackage) SHALL report on `RenderResult.SkewPolicy` the catalog skew policy of the platform record they leased for the render, spelled as `Platform.spec.skewPolicy` spells it (`Warn` or `Refuse`), beside `RenderResult.PlatformIdentity`. The reconciler builds the recorded render input key from these two, so the key names the platform the render actually used.

#### Scenario: A render under the default policy reports Warn

- **WHEN** a ModuleInstance renders against a platform record whose resolved skew policy is the default
- **THEN** the result's `SkewPolicy` is `Warn` and its `PlatformIdentity` is the record's identity

#### Scenario: A package render under Refuse reports Refuse

- **WHEN** a ModulePackage renders against a platform record whose skew policy is `Refuse`
- **THEN** the result's `SkewPolicy` is `Refuse`
