## MODIFIED Requirements

### Requirement: Status always patched
The reconciler MUST patch `ModuleRelease.status` at the end of every reconcile
attempt, including `NoOp`. On meaningful outcomes (`Applied`, `AppliedAndPruned`,
`FailedTransient`, `FailedStalled`), the patch updates conditions,
`lastAttempted*`, `lastApplied*` (on success), `inventory` (on success),
history, failure counters, and `nextRetryAt`. On `NoOp`, the patch is bounded
to: drift condition (`Drifted`), failure counter deltas (incl. drift counter),
and `nextRetryAt` clearing. `lastAttempted*`, `inventory`, and history MUST NOT
be modified on `NoOp` — those fields describe meaningful reconcile outcomes.

Storm safety is provided by `WithEventFilter(predicate.GenerationChangedPredicate{})`
on the controller. Status subresource patches do not bump
`metadata.generation`, so the predicate filters them at the watch boundary.

#### Scenario: Status updated on failure
- **WHEN** a phase fails
- **THEN** status conditions, `lastAttempted*` fields, `failureCounters`, and
  `nextRetryAt` are updated

#### Scenario: NoOp patches drift and counters
- **WHEN** the outcome is `NoOp` and drift detection ran (with or without
  detecting drift)
- **THEN** `status.conditions[Drifted]` reflects the drift detection result
- **AND** `status.failureCounters` reflects phase counter deltas
  (incl. `drift` counter increment on dry-run failure)
- **AND** `status.nextRetryAt` is cleared
- **AND** `status.lastAttempted*`, `status.inventory`, and `status.history`
  are NOT modified

#### Scenario: NoOp patch does not trigger reconcile loop
- **WHEN** a NoOp patch updates `status` (drift condition or counters)
- **THEN** the resulting watch event is filtered by `GenerationChangedPredicate`
- **AND** no follow-up reconcile is queued from the status patch alone
