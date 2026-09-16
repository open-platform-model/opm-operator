## MODIFIED Requirements

### Requirement: Status always patched
The reconciler MUST patch `ModuleRelease.status` at the end of every reconcile
attempt, including `NoOp`. On meaningful outcomes (`Applied`, `AppliedAndPruned`,
`FailedTransient`, `FailedStalled`), the patch updates conditions,
`lastAttempted*`, `lastApplied*` (on success), `inventory` (on success),
history, failure counters, and `nextRetryAt`. On `NoOp`, the patch is bounded
to: drift condition (`Drifted`), failure counter deltas (incl. drift counter),
`nextRetryAt` clearing, and `requiredContracts`. `lastAttempted*`, `inventory`,
and history MUST NOT be modified on `NoOp` — those fields describe meaningful
reconcile outcomes.

`requiredContracts` is in the `NoOp` set deliberately, and it is the one field
there that does not describe an outcome. It describes what the instance
demands, which a reconcile can observe as changed without any digest changing —
a regenerated platform re-enqueues every instance, and the render that follows
may be a no-op for apply while being the only evidence that the demand moved.
Bounding it out of the `NoOp` patch would leave the field describing a render
the operator has already superseded.

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

#### Scenario: NoOp refreshes the demand the render just reported
- **WHEN** the outcome is `NoOp` and the render that preceded it reported a
  different contract demand than the status carries
- **THEN** `status.requiredContracts` is updated to what the render reported

#### Scenario: NoOp patch does not trigger reconcile loop
- **WHEN** a NoOp patch updates `status` (drift condition or counters)
- **THEN** the resulting watch event is filtered by `GenerationChangedPredicate`
- **AND** no follow-up reconcile is queued from the status patch alone

## ADDED Requirements

### Requirement: The instance's contract demand is recorded on its status

The reconciler SHALL record on `ModuleInstance.status.requiredContracts` the set of contract FQNs the instance's components declare, as the render reports it, and SHALL write it on every reconcile whose render succeeds. The field SHALL be derived from the rendered instance and SHALL NOT be readable from, or influenced by, anything a module author writes into `spec`.

A reconcile that does not render SHALL leave the field at its previous value. This includes a failed render, a suspended instance and a CLI-owned instance: a stale value over-reports demand, which blocks a deletion that could have proceeded, and the opposite error would let a provider abandon its dependents.

#### Scenario: A successful render records the demand

- **WHEN** an instance renders successfully
- **THEN** `status.requiredContracts` lists, sorted and without duplicates, every contract FQN its components declare

#### Scenario: A failed render leaves the previous value

- **WHEN** a render fails
- **THEN** `status.requiredContracts` is unchanged from the last successful render

#### Scenario: The demand follows the spec

- **WHEN** an instance's module is changed to one whose components declare a different contract set, and it renders successfully
- **THEN** `status.requiredContracts` reports the new set and no longer reports the contracts only the previous module declared
