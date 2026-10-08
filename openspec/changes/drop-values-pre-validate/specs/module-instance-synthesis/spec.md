## REMOVED Requirements

### Requirement: An unset required config value is refused in the spec.values wording

**Reason**: The requirement made the wording of the operator's own values check normative and forbade the kernel's wording. The kernel refuses an unset required `#config` value itself since library v1.0.0-beta.7, and the operator's check is deleted, so one rule has one check and one wording.

**Migration**: Match on the reason `RenderFailed`, which does not change. A match on the message text `validating values against the module's #config` no longer finds anything; the message now contains `not fully concrete: values.<field>: incomplete value <type>`. The requirement "An unset required config value is refused by instance synthesis" replaces this one.

## ADDED Requirements

### Requirement: An unset required config value is refused by instance synthesis

A ModuleInstance whose `spec.values` leave a required `#config` value of its module unset SHALL be refused by instance synthesis, whether or not a component reads the value and whether or not `spec.values` is present. The controller SHALL report the kernel's refusal and SHALL NOT report a wording of its own for this state. The message SHALL contain `not fully concrete: values.<field>: incomplete value <type>` for the unset field, the phrase a ModulePackage reports for the same defect, and SHALL name every unset required field with the position of its `#config` declaration. The ModuleInstance SHALL report `Ready=False` and `Stalled=True` with reason `RenderFailed`.

#### Scenario: Unset required value that no component reads

- **WHEN** a module declares `#config: note: string` with no default, no component reads `note`, and a ModuleInstance of it sets no `note` in `spec.values`
- **THEN** the render fails with a message that contains `not fully concrete: values.note: incomplete value string (<file>:<line>:<column>)`, and the ModuleInstance reports `Ready=False` and `Stalled=True` with reason `RenderFailed`

#### Scenario: No spec.values at all

- **WHEN** the same module is instantiated by a ModuleInstance without `spec.values`
- **THEN** the render fails with the same message and the same reason

#### Scenario: Two unset required values

- **WHEN** the module also declares `#config: other: int` with no default and a ModuleInstance sets neither `note` nor `other`
- **THEN** the message names both `values.note` and `values.other`, each with the position of its declaration, and does not shorten the second to a count

#### Scenario: The value is set

- **WHEN** the ModuleInstance sets every required value in `spec.values`
- **THEN** the instance is synthesized

### Requirement: A values failure reports every finding with its positions

When instance synthesis refuses the values of a ModuleInstance, the message on the `Ready` condition and on the event SHALL carry the kernel's error text followed by every finding the kernel reported, each with the positions the kernel attributed it to, and SHALL NOT replace findings after the first with a count. A finding caused by a value in `spec.values` SHALL name its position as `spec.values:<line>:<column>`. The wording SHALL NOT change how the failure is classified: a values failure SHALL report reason `RenderFailed` with `Stalled=True`, and a registry fetch failure during synthesis SHALL still report `ResolutionFailed` without `Stalled` and retry on the backoff.

#### Scenario: A value of the wrong type that a component reads

- **WHEN** a module declares `#config: message: string | *"hello"`, a component reads `message`, and a ModuleInstance sets `message: 42`
- **THEN** the message names `#config.message`, the conflicting values and a position `spec.values:<line>:<column>`, and the ModuleInstance reports `Ready=False` and `Stalled=True` with reason `RenderFailed`

#### Scenario: A value of the wrong type that no component reads

- **WHEN** a module declares `#config: note: string`, no component reads `note`, and a ModuleInstance sets `note: 7`
- **THEN** the message names `#config.note`, the conflicting values and a position `spec.values:<line>:<column>`, with reason `RenderFailed`

#### Scenario: Two values of the wrong type

- **WHEN** a ModuleInstance sets two values that each conflict with `#config`
- **THEN** the message names both fields, each with its positions

#### Scenario: A field the module does not declare

- **WHEN** a ModuleInstance sets a field in `spec.values` that the module's `#config` does not allow
- **THEN** the message contains `field not allowed (spec.values:<line>:<column>)`, with reason `RenderFailed`

#### Scenario: A registry fetch failure during synthesis keeps its class

- **WHEN** instance synthesis fails with a registry fetch failure
- **THEN** the ModuleInstance reports `Ready=False` with reason `ResolutionFailed`, no `Stalled` condition, and retries on the exponential backoff
