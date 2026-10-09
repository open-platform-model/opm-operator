## ADDED Requirements

### Requirement: The event of a stalled render failure stays inside the event note limit

The `Warning` event the controller emits when a render or a package load stalls a ModuleInstance or a ModulePackage SHALL carry a message of at most 1024 characters, the longest note events.k8s.io/v1 accepts. When the failure's message fits, the event message SHALL be the message of the `Ready` condition. When it does not fit and the failure lists findings, the event message SHALL keep the text in front of the findings and whole findings from the first, and SHALL end with `; and <N> more findings`, where N is the number of findings left out. Any other message that does not fit SHALL be cut between two characters and end with ` ... (<N> more characters)`. The `Ready` condition SHALL keep the whole message.

#### Scenario: Twenty unset required values

- **WHEN** a ModuleInstance leaves twenty required `#config` values of its module unset
- **THEN** the `Ready` condition message names all twenty as `values.<field>`
- **AND** the `Warning` event with reason `RenderFailed` has a message of at most 1024 characters that names the first values in whole findings and ends with `; and <N> more findings`, and the values named and N add up to twenty

#### Scenario: A message that fits

- **WHEN** a ModuleInstance leaves three required values unset
- **THEN** the event message is the message of the `Ready` condition

#### Scenario: A long message without findings

- **WHEN** a render stalls with a message of 3000 characters that lists no findings
- **THEN** the event message is at most 1024 characters, starts as the condition message and ends with ` ... (<N> more characters)`
