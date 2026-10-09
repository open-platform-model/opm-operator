## ADDED Requirements

### Requirement: An unset required value that a component reads is named as a value

When a ModulePackage is refused because its values leave a required `#config` value unset, the message SHALL name that value as `values.<field>` also when a component reads it. The message SHALL NOT name the place in a component that reads the value in its stead. When more than one required value is unset, the message SHALL name the first as `values.<field>` and SHALL count the others as `(and N more errors)`. The text is the kernel's, unchanged after the prefix `loading package: `. The reason SHALL stay `ResolutionFailed` with `Ready=False` and `Stalled=True`.

#### Scenario: The component reads the unset value

- **WHEN** a ModulePackage's module declares `#config: {greeting: string, note: string}` with no defaults, a component reads `greeting`, and the package's values set `note` only
- **THEN** the ModulePackage reports `Ready=False` and `Stalled=True` with reason `ResolutionFailed` and the message `loading package: Kernel.AcquireInstanceFromDir: instance "<name>": not fully concrete: values.greeting: incomplete value string`

#### Scenario: Two values are unset

- **WHEN** the same package's values set neither
- **THEN** the message ends in `not fully concrete: values.greeting: incomplete value string (and 1 more errors)`
