## ADDED Requirements

### Requirement: An unset required config value is refused in the spec.values wording

A ModuleInstance whose `spec.values` leave a required `#config` value of its module unset SHALL be refused before instance synthesis by the check of `spec.values` against `#config`, whether or not a component reads the value and whether or not `spec.values` is present. The message SHALL start with `validating values against the module's #config: ` and SHALL name the `#config` field and the position of its declaration. The ModuleInstance SHALL report `Ready=False` and `Stalled=True` with reason `RenderFailed`. A kernel that refuses the same state during synthesis SHALL NOT change this message or this reason.

#### Scenario: Unset required value that no component reads

- **WHEN** a module declares `#config: note: string` with no default, no component reads `note`, and a ModuleInstance of it sets no `note` in `spec.values`
- **THEN** the render fails with `validating values against the module's #config: #config.note: incomplete value string (<file>:<line>:<column>)`, and the ModuleInstance reports `Ready=False` and `Stalled=True` with reason `RenderFailed`

#### Scenario: The value is set

- **WHEN** the same ModuleInstance sets `note` in `spec.values`
- **THEN** the values pass the check and the instance is synthesized
