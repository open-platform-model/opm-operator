## ADDED Requirements

### Requirement: A withheld resource is excluded from drift detection

Drift detection SHALL skip a resource the reconcile withheld from apply, and its difference from live state SHALL NOT set the `Drifted` condition.

Drift reports that the cluster diverged from what the operator asserts. A withheld resource is one the operator is deliberately not asserting, so reporting it as drift would name a difference the operator created on purpose and intends not to close — a condition that never clears, and one that would bury real drift on the same instance behind it. The refusal that withheld the resource carries that signal instead.

#### Scenario: A withheld resource does not set Drifted

- **WHEN** a reconcile withholds a resource whose live state differs from the rendered one
- **THEN** the `Drifted` condition is not set by that difference

#### Scenario: Real drift is still reported alongside

- **WHEN** an instance has both a withheld resource and another resource that genuinely drifted
- **THEN** the `Drifted` condition is set, reflecting only the genuinely drifted resource

#### Scenario: Drift returns when the resource stops being withheld

- **WHEN** a previously withheld resource is applied on a later reconcile
- **THEN** it is included in drift detection from that point on
