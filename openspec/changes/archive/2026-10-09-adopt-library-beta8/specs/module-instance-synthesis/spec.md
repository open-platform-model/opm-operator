## ADDED Requirements

### Requirement: Every unset required config value is named

When the `spec.values` of a ModuleInstance leave more than one required `#config` value of its module unset, the refusal SHALL name every one of them, one finding for each, as `#config.<field>` with the position of its declaration. A nested field SHALL be named by its full path (`#config.db.host`). A value a component reads and a value no component reads SHALL be named alike. The findings SHALL be joined by `; ` after the prefix `validating values against the module's #config: `.

The fields named SHALL be the fields the kernel names for the same values when it refuses the instance during synthesis, where it writes them as `values.<field>`, at the same positions. The two reports SHALL NOT differ in which values they name.

#### Scenario: Three unset values, one read by a component

- **WHEN** a module declares `#config: {note: string, db: host: string, greeting: string}` with no defaults, one component reads `greeting`, and a ModuleInstance of it sets none of them
- **THEN** the render fails with one message that names `#config.note`, `#config.db.host` and `#config.greeting`, each as `incomplete value string` with its position
- **AND** the ModuleInstance reports `Ready=False` and `Stalled=True` with reason `RenderFailed`

#### Scenario: The kernel names the same values

- **WHEN** the same values are given to the kernel's instance synthesis without the check of `spec.values`
- **THEN** the kernel refuses with findings `values.note`, `values.db.host` and `values.greeting`, at the same positions and with the same text after the path

#### Scenario: Setting one value removes its finding only

- **WHEN** the ModuleInstance sets `note` and leaves the other two unset
- **THEN** the message names `#config.db.host` and `#config.greeting` and does not name `#config.note`
