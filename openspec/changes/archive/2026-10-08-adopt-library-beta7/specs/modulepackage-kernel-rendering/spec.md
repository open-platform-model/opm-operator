## ADDED Requirements

### Requirement: A package that leaves a required config value unset is refused

A ModulePackage whose instance values leave a required `#config` value of its module unset SHALL be refused when the package loads, whether or not a component reads the value. A required value is a field declared `foo!`, or a field of a bare type with no default. The same SHALL hold for a value that the package's values give a default other than the `#config` default: the two do not unify to a concrete value. The refusal SHALL be the kernel's: the operator SHALL report it as a package load failure, with reason `ResolutionFailed`, `Ready=False` and `Stalled=True`, and SHALL NOT retry it on the registry backoff. The message SHALL carry the kernel's error unchanged after the prefix `loading package: `, and it names the unset value as `values.<field>`. Nothing SHALL be applied or pruned.

#### Scenario: Unset required value that no component reads

- **WHEN** a ModulePackage's module declares `#config: note: string` with no default, no component reads `note`, and the package's values do not set it
- **THEN** the ModulePackage reports `Ready=False` and `Stalled=True` with reason `ResolutionFailed`, the message contains `not fully concrete: values.note: incomplete value string`, and no resource is applied

#### Scenario: A default that disagrees with the config default

- **WHEN** the module declares `#config: tier: string | *"a"`, no component reads `tier`, and the package's values hold `tier: string | *"b"`
- **THEN** the ModulePackage reports `Ready=False` and `Stalled=True` with reason `ResolutionFailed`, and the message contains `not fully concrete: values.tier: incomplete value`

#### Scenario: The value is set

- **WHEN** the same package's values set `note`
- **THEN** the package loads and the render proceeds to the platform gate
