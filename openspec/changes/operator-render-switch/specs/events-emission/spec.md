## ADDED Requirements

### Requirement: Render warnings are emitted as events on transition

When a render succeeds with warnings (catalog skew under the `Warn` policy, unhandled optional traits), the reconciler SHALL emit one Warning event per distinct warning message with reason `RenderWarning` and action `Render`, and SHALL emit them only when the object's set of warnings changes between reconciles, not on every reconcile. A render with no warnings SHALL emit none.

#### Scenario: Skew under Warn is reported once

- **WHEN** a ModuleInstance's module requires a newer catalog build than the platform pins and the policy is `Warn`
- **THEN** the instance renders, reaches `Ready=True`, and one Warning event names the path and both versions

#### Scenario: Unchanged warnings do not repeat

- **WHEN** the same instance reconciles again with the same warnings
- **THEN** no new warning event is emitted
