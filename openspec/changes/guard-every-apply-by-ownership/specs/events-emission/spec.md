## ADDED Requirements

### Requirement: Events emitted when an apply is refused by ownership
The controller MUST emit one `Warning` event with reason `ApplyRefused` and action `Apply` when the apply verdict refuses a reconcile. The event MUST state the count of refused objects and MUST carry, for each, the message the library words for the refusal, unchanged (at most ten objects, and fewer when the messages would take the event past 1024 characters; then the number of the rest). The event text MUST NOT carry an enhancement reference. No `Applied` event is emitted by that reconcile.

#### Scenario: A refused apply
- **GIVEN** a ModuleInstance whose render names two live objects that belong to another instance
- **WHEN** the controller reconciles
- **THEN** one `Warning` event with reason `ApplyRefused` and action `Apply` is emitted
- **AND** its message states two objects and contains the library's message for each

### Requirement: Events emitted when an object is adopted by another instance
The controller MUST emit one `Warning` event with reason `AdoptedElsewhere` and action `Apply` in every reconcile that renders and finds one or more rendered objects that the apply verdict refuses as `adopted-elsewhere`, whether the reconcile applies, restores or is a no-op, and whether or not the object is still in `status.inventory`. The event MUST state the count and MUST carry the library's message for each object, unchanged, within the limits of the `ApplyRefused` event. A reconcile that skips its render emits none. Source: 0012:D8:R8.

#### Scenario: An object is let go
- **GIVEN** an inventoried ConfigMap `team-a/settings` whose `opmodel.dev/adopt` annotation names another instance
- **WHEN** a reconcile renders
- **THEN** one `Warning` event with reason `AdoptedElsewhere` and action `Apply` is emitted, with the library's message for `ConfigMap/team-a/settings`

#### Scenario: The object stays reported
- **GIVEN** the ConfigMap above, no longer in `status.inventory`, still rendered and still annotated
- **WHEN** a later reconcile renders
- **THEN** the event is emitted again

#### Scenario: Nothing adopted elsewhere, no event
- **GIVEN** a reconcile in which the verdict allows every rendered object
- **WHEN** the reconcile completes
- **THEN** no event with reason `AdoptedElsewhere` is emitted
