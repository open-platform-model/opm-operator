## MODIFIED Requirements

### Requirement: Unresolved platform demands classify as resolution failures

The reconciler SHALL classify a refused render by its typed cause: unresolved platform demands and unmatched components SHALL be `ResolutionFailed`; a catalog-skew refusal SHALL be `SkewRefused` with a message naming the module path, the module's required build and the platform's build; a render whose compiled objects share one apply identity SHALL be `DuplicateIdentities` with the library's message naming each identity and every producing component and transformer (enhancement 0015 D15); a transform failure, an over-subscribed provider contract, or any other refusal SHALL be `RenderFailed`. Every classified refusal SHALL set `Ready=False` and `Stalled=True`, emit a Warning event and requeue on the stalled recheck interval; the ModuleInstance and ModulePackage classifiers SHALL route typed causes through one shared function so they cannot drift.

#### Scenario: Skew refusal is distinct

- **WHEN** the platform's policy is `Refuse` and the package's module requires a newer catalog build than the platform pins
- **THEN** the package reports `Ready=False` reason `SkewRefused` naming the path and both versions, applies nothing, and requeues on the stalled recheck interval

#### Scenario: Over-subscribed provider contract is a render failure

- **WHEN** the build refuses because two enabled catalogs provide the same provider-fulfilled contract
- **THEN** the package reports `RenderFailed` with the kernel's message naming the contract key and both catalogs

#### Scenario: Package demands contracts the platform does not provide

- **WHEN** a package renders against a platform whose catalogs do not provide contracts the module demands
- **THEN** the ModulePackage reports `Ready=False` with reason `ResolutionFailed` and `Stalled=True`, and a Warning event carries the unresolved-demands message

#### Scenario: Duplicate identities are a refusal of their own

- **WHEN** a render's compiled objects share one apply identity
- **THEN** the object reports `Ready=False` with reason `DuplicateIdentities` and `Stalled=True`, the message names the identity and both producing components, nothing is applied, and the same reason is produced whether the render was a ModuleInstance's or a ModulePackage's

#### Scenario: Ordinary evaluation error keeps RenderFailed

- **WHEN** the render fails for a cause that is neither a resolution-class failure, a skew refusal nor a duplicate identity
- **THEN** the ModulePackage reports `Ready=False` with reason `RenderFailed`
