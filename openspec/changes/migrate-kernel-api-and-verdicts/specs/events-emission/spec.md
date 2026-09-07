## MODIFIED Requirements

### Requirement: Render warnings are emitted as events on transition

When a render succeeds with advisory findings (catalog skew under the `Warn` policy, unhandled optional traits), the reconciler SHALL emit one Warning event per distinct finding with reason `RenderWarning` and action `Render`, and SHALL emit them only when the object's set of findings changes between reconciles, not on every reconcile. A render with no findings SHALL emit none.

The event text SHALL be authored by the operator from the render's advisory diagnostic rows; the render carries no message string to pass through. The transition check SHALL be keyed on the facts those rows carry — for skew, the path and both versions; for an unhandled trait, the component and the trait — so that rewording an event does not present an unchanged finding set as changed.

#### Scenario: Skew under Warn is reported once

- **WHEN** a ModuleInstance's module requires a newer catalog build than the platform pins and the policy is `Warn`
- **THEN** the instance renders, reaches `Ready=True`, and one Warning event names the path and both versions

#### Scenario: Unchanged warnings do not repeat

- **WHEN** the same instance reconciles again with the same warnings
- **THEN** no new warning event is emitted

#### Scenario: Rewording does not re-emit

- **WHEN** the operator's warning wording changes while an object's advisory rows are unchanged between reconciles
- **THEN** no new warning event is emitted for that object
