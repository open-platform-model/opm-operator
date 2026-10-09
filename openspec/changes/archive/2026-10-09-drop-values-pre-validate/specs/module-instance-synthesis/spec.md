## REMOVED Requirements

### Requirement: An unset required config value is refused in the spec.values wording

**Reason**: The requirement made the wording of the operator's own values check normative and forbade the kernel's wording. The kernel refuses an unset required `#config` value itself since library v1.0.0-beta.7, and the operator's check is deleted, so one rule has one check and one wording.

**Migration**: Match on the reason `RenderFailed`, which does not change. A match on the message text `validating values against the module's #config` no longer finds anything; the message now contains `not fully concrete: values.<field>: incomplete value <type>`. The requirement "An unset required config value is refused by instance synthesis" replaces this one.

### Requirement: Every unset required config value is named

**Reason**: The requirement words the list of unset values as the operator's own values check wrote it (`#config.<field>` after the prefix `validating values against the module's #config: `) and requires that check and the kernel to name the same values. The check is deleted, so there is one report. The requirement "An unset required config value is refused by instance synthesis" now holds what stays true: every unset value is named, nested and read values included, as `values.<field>`.

**Migration**: Match on the reason `RenderFailed`. In a message, `#config.<field>: incomplete value` becomes `values.<field>: incomplete value`, at the same position.

## ADDED Requirements

### Requirement: An unset required config value is refused by instance synthesis

A ModuleInstance whose `spec.values` leave a required `#config` value of its module unset SHALL be refused by instance synthesis, whether or not a component reads the value and whether or not `spec.values` is present. The controller SHALL report the kernel's refusal and SHALL NOT run a check of its own for this state. The message SHALL be `synthesizing release: Kernel.SynthesizeInstance: instance "<name>": not fully concrete: ` followed by one finding for every unset required value, `values.<field>: incomplete value <type> (<file>:<line>:<column>)` with the position of its `#config` declaration, joined by `; `. A nested field SHALL be named by its full path (`values.db.host`). A value a component reads SHALL be named like a value no component reads, and the message SHALL NOT name the place in a component that reads it. The controller SHALL word the findings from the kernel's typed error tree; the kernel's own text names the first finding and counts the rest. The ModuleInstance SHALL report `Ready=False` and `Stalled=True` with reason `RenderFailed`.

#### Scenario: Unset required value that no component reads

- **WHEN** a module declares `#config: note: string` with no default, no component reads `note`, and a ModuleInstance of it sets no `note` in `spec.values`
- **THEN** the render fails with a message that contains `not fully concrete: values.note: incomplete value string (<file>:<line>:<column>)`, and the ModuleInstance reports `Ready=False` and `Stalled=True` with reason `RenderFailed`

#### Scenario: No spec.values at all

- **WHEN** the same module is instantiated by a ModuleInstance without `spec.values`
- **THEN** the render fails with the same message and the same reason

#### Scenario: Two unset required values

- **WHEN** the module also declares `#config: other: int` with no default and a ModuleInstance sets neither `note` nor `other`
- **THEN** the message names both `values.note` and `values.other`, each with the position of its declaration, and does not shorten the second to a count

#### Scenario: Three unset values, one read by a component, one nested

- **WHEN** a module declares `#config: {note: string, db: host: string, greeting: string}` with no defaults, one component reads `greeting`, and a ModuleInstance of it sets none of them
- **THEN** the message names `values.note`, `values.db.host` and `values.greeting`, each as `incomplete value string` with its position, and names no path under `components`
- **AND** the ModuleInstance reports `Ready=False` and `Stalled=True` with reason `RenderFailed`

#### Scenario: Setting one value removes its finding only

- **WHEN** the same ModuleInstance sets `note` and leaves the other two unset
- **THEN** the message names `values.db.host` and `values.greeting` and does not name `values.note`

#### Scenario: The value is set

- **WHEN** the ModuleInstance sets every required value in `spec.values`
- **THEN** the instance is synthesized

### Requirement: A values failure reports every finding with its positions

When instance synthesis refuses the values of a ModuleInstance, the message on the `Ready` condition SHALL carry the kernel's error text up to its first finding, followed by every finding the kernel reported, each with the positions the kernel attributed it to, and SHALL NOT replace findings after the first with a count while the message is inside the condition's limit of 32768 characters; past that limit it SHALL keep whole findings from the first and end with `; and <N> more findings`. The event carries the same text within the event note limit (capability `events-emission`). A registry fetch failure during synthesis SHALL keep its own text. A finding caused by a value in `spec.values` SHALL name its position as `spec.values:<line>:<column>`. The wording SHALL NOT change how the failure is classified: a values failure SHALL report reason `RenderFailed` with `Stalled=True`, and a registry fetch failure during synthesis SHALL still report `ResolutionFailed` without `Stalled` and retry on the backoff.

#### Scenario: A value of the wrong type that a component reads

- **WHEN** a module declares `#config: message: string | *"hello"`, a component reads `message`, and a ModuleInstance sets `message: 42`
- **THEN** the message names `#config.message`, the conflicting values and a position `spec.values:<line>:<column>`, and the ModuleInstance reports `Ready=False` and `Stalled=True` with reason `RenderFailed`

#### Scenario: A value of the wrong type that no component reads

- **WHEN** a module declares `#config: note: string`, no component reads `note`, and a ModuleInstance sets `note: 7`
- **THEN** the message names `#config.note`, the conflicting values and a position `spec.values:<line>:<column>`, with reason `RenderFailed`

#### Scenario: Two values of the wrong type

- **WHEN** a ModuleInstance sets two values that each conflict with `#config`
- **THEN** the message names both fields, each with its positions

#### Scenario: A constraint a value does not meet

- **WHEN** a module declares `#config: port: int & >0 | *80` and a ModuleInstance sets `port: -1`
- **THEN** the message names `#config.port`, `invalid value -1 (out of bound >0)` and a position `spec.values:<line>:<column>`, with reason `RenderFailed`

#### Scenario: A field the module does not declare

- **WHEN** a ModuleInstance sets a field in `spec.values` that the module's `#config` does not allow
- **THEN** the message contains `field not allowed (spec.values:<line>:<column>)`, with reason `RenderFailed`

#### Scenario: A registry fetch failure during synthesis keeps its class

- **WHEN** instance synthesis fails with a registry fetch failure
- **THEN** the ModuleInstance reports `Ready=False` with reason `ResolutionFailed`, no `Stalled` condition, and retries on the exponential backoff
