## MODIFIED Requirements

### Requirement: A package that leaves a required config value unset is refused

A ModulePackage whose instance values leave a required `#config` value of its module unset SHALL be refused when the package loads, whether or not a component reads the value. A required value is a field declared `foo!`, or a field of a bare type with no default. The same SHALL hold for a value that the package's values give a default other than the `#config` default: the two do not unify to a concrete value. The refusal SHALL be the kernel's: the operator SHALL report it as a package load failure, with reason `ResolutionFailed`, `Ready=False` and `Stalled=True`, and SHALL NOT retry it on the registry backoff. The message SHALL be the prefix `loading package: `, the kernel's error text up to its first finding, and the kernel's findings, each with its positions; it names the unset value as `values.<field>`. Nothing SHALL be applied or pruned.

#### Scenario: Unset required value that no component reads

- **WHEN** a ModulePackage's module declares `#config: note: string` with no default, no component reads `note`, and the package's values do not set it
- **THEN** the ModulePackage reports `Ready=False` and `Stalled=True` with reason `ResolutionFailed`, the message contains `not fully concrete: values.note: incomplete value string`, and no resource is applied

#### Scenario: A default that disagrees with the config default

- **WHEN** the module declares `#config: tier: string | *"a"`, no component reads `tier`, and the package's values hold `tier: string | *"b"`
- **THEN** the ModulePackage reports `Ready=False` and `Stalled=True` with reason `ResolutionFailed`, and the message contains `not fully concrete: values.tier: incomplete value`

#### Scenario: The value is set

- **WHEN** the same package's values set `note`
- **THEN** the package loads and the render proceeds to the platform gate

## REMOVED Requirements

### Requirement: An unset required value that a component reads is named as a value

**Reason**: The requirement makes the kernel's own text normative: the first unset value and `(and N more errors)` for the others. A user with three unset values then reads one name. The operator now words the kernel's findings one by one on a ModulePackage, as it does on a ModuleInstance.

**Migration**: Match on the reason `ResolutionFailed`, which does not change. A message that ended in `(and N more errors)` now lists every finding, and every finding ends with its positions in parentheses. The requirement "Every unset required value of a package is named" replaces this one.

## ADDED Requirements

### Requirement: Every unset required value of a package is named

When a ModulePackage is refused because its values leave required `#config` values unset, the message SHALL name every one of them, one finding for each, as `values.<field>: incomplete value <type> (<file>:<line>:<column>)` with the position of its `#config` declaration, joined by `; ` after `loading package: Kernel.AcquireInstanceFromDir: instance "<name>": not fully concrete: `. A nested field SHALL be named by its full path. A value a component reads SHALL be named like a value no component reads, and the message SHALL NOT name the place in a component that reads it. The message SHALL NOT replace a finding with `(and N more errors)`. The controller SHALL word the findings from the kernel's typed error tree. The reason SHALL stay `ResolutionFailed` with `Ready=False` and `Stalled=True`.

#### Scenario: The component reads the unset value

- **WHEN** a ModulePackage's module declares `#config: {greeting: string, note: string}` with no defaults, a component reads `greeting`, and the package's values set `note` only
- **THEN** the ModulePackage reports `Ready=False` and `Stalled=True` with reason `ResolutionFailed` and the message `loading package: Kernel.AcquireInstanceFromDir: instance "<name>": not fully concrete: values.greeting: incomplete value string (instance.cue:<line>:<column>)`

#### Scenario: Two values are unset

- **WHEN** the same package's values set neither
- **THEN** the message names `values.greeting` and `values.note`, each with its position, and holds no `more errors`

#### Scenario: Three values are unset, one nested

- **WHEN** the module declares `#config: {greeting: string, db: host: string, note: string}` and the package's values set none of them
- **THEN** the message names `values.greeting`, `values.db.host` and `values.note`

### Requirement: A package values failure reports every finding with stable positions

When the load of a ModulePackage fails with findings of the kernel (a value of the wrong type, a constraint a value does not meet, a field the module's `#config` does not allow, a value left unset), the message SHALL list every finding with the positions the kernel attributed it to, and SHALL NOT replace findings after the first with a count while the message is inside the condition's limit of 32768 characters; past that limit it SHALL keep whole findings from the first and end with `; and <N> more findings`. A position in a file of the package SHALL be written relative to the package's CUE module root, so the message of an unchanged package is the same on every reconcile although the operator extracts the package to a new temporary directory each time. A position in any other file SHALL be written as the kernel reports it. The wording SHALL NOT change how the failure is classified, and a registry fetch failure SHALL keep its own text.

#### Scenario: A value of the wrong type

- **WHEN** a package's module declares `#config: note: string` and the package's values hold `note: 7`
- **THEN** the message contains `#module.#config.note: conflicting values string and 7 (mismatched types string and int) (instance.cue:<line>:<column>, instance.cue:<line>:<column>)`, with reason `ResolutionFailed` and `Stalled=True`

#### Scenario: A constraint a value does not meet

- **WHEN** the module declares `#config: port: int & >0 | *80` and the package's values hold `port: -1`
- **THEN** the message lists three findings for `#module.#config.port`, among them `invalid value -1 (out of bound >0)` with its positions, and holds no `more errors`

#### Scenario: A field the module does not declare

- **WHEN** the package's values hold a field the module's `#config` does not allow
- **THEN** the message contains `field not allowed (instance.cue:<line>:<column>)`

#### Scenario: The same package in another directory

- **WHEN** the same refused package is loaded from two different extraction directories
- **THEN** both messages are equal and neither names an extraction directory in a position

#### Scenario: A registry fetch failure keeps its text

- **WHEN** the package load fails with the library's typed registry fetch failure
- **THEN** the message is `loading package: ` followed by the kernel's error text unchanged, and the failure retries on the backoff as before
