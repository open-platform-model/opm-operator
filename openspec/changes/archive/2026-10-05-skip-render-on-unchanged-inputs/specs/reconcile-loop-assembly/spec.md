## MODIFIED Requirements

### Requirement: Status always patched
The reconciler MUST patch `ModuleRelease.status` at the end of every reconcile
attempt, including `NoOp`. On meaningful outcomes (`Applied`, `AppliedAndPruned`,
`FailedTransient`, `FailedStalled`), the patch updates conditions,
`lastAttempted*`, `lastApplied*` (on success), `inventory` (on success),
history, failure counters, and `nextRetryAt`. On `NoOp`, the patch is bounded
to: drift condition (`Drifted`), failure counter deltas (incl. drift counter),
`nextRetryAt` clearing, `requiredContracts`, and `lastAppliedVersion` and
`lastAppliedInputs` when the attempt rendered. `lastAttempted*`, `inventory`,
and history MUST NOT be modified on `NoOp` — those fields describe meaningful
reconcile outcomes.

A reconcile that skips its render because its inputs are unchanged
(`render-input-key`) is not an attempt and MUST NOT patch status at all: every
condition it read is already final, and nothing it could record has moved.

`requiredContracts` is in the `NoOp` set deliberately, and it is the one field
there that does not describe an outcome. It describes what the instance
demands, which a reconcile can observe as changed without any digest changing —
a regenerated platform re-enqueues every instance, and the render that follows
may be a no-op for apply while being the only evidence that the demand moved.
Bounding it out of the `NoOp` patch would leave the field describing a render
the operator has already superseded.

`lastAppliedVersion` is in the `NoOp` set because a `NoOp` re-proves it. A
`NoOp` means the source digest equals the last apply's, and the source digest
pins the module version, so the version the render reports is the one last
applied, whoever applied it. Writing it fills the field on an object applied
before it existed and corrects it after an ownership handback. A reconcile
that did not render leaves it as it was.

`lastAppliedInputs` is in the `NoOp` set for the same reason: a `NoOp` that
rendered proves the cluster holds what those inputs produce, so its key and
render time are recorded, and the drift render interval counts from it. It is
written on a `NoOp` only while the drift render interval is greater than
zero; with the skip disabled nothing reads it.

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

#### Scenario: A skipped render patches nothing
- **WHEN** a reconcile skips its render because its inputs are unchanged
- **THEN** no status patch is sent
- **AND** `lastAttemptedAt`, history and `status.lastAppliedInputs.renderedAt`
  keep their values
