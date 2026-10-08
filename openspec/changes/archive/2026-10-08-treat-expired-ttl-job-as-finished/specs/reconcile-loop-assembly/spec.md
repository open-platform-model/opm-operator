## MODIFIED Requirements

### Requirement: Status always patched
The reconciler MUST patch `ModuleRelease.status` at the end of every reconcile
attempt, including `NoOp`. On meaningful outcomes (`Applied`, `AppliedAndPruned`,
`FailedTransient`, `FailedStalled`), the patch updates conditions,
`lastAttempted*`, `lastApplied*` (on success), `inventory` (on success),
history, failure counters, and `nextRetryAt`. On `NoOp`, the patch is bounded
to: drift condition (`Drifted`), the `Healthy` condition (`instance-health`),
failure counter deltas (incl. drift counter), `nextRetryAt` clearing,
`requiredContracts`, and `lastAppliedVersion` and `lastAppliedInputs` when the
attempt rendered, and the removal of expired Job entries from
`inventory.entries` (`drift-detection`, "A missing object is restored").
`lastAttempted*` and history MUST NOT be modified on `NoOp`, and `inventory`
MUST NOT be modified in any other way: those fields describe meaningful
reconcile outcomes.

A reconcile that skips its render because its inputs are unchanged
(`render-input-key`) is not an attempt. It judges health (`instance-health`)
and MUST NOT patch anything but the `Healthy` condition, which it patches only
when the judgement changed it: every other condition it read is already final,
and nothing else it could record has moved.

On a successful outcome the patch also carries the `Healthy` condition the
reconcile judged (`instance-health`). A failed attempt leaves `Healthy` as it
was.

`requiredContracts` is in the `NoOp` set deliberately, and it is the one field
there that does not describe an outcome. It describes what the instance
demands, which a reconcile can observe as changed without any digest changing:
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
- **AND** `status.lastAttempted*` and `status.history` are NOT modified
- **AND** `status.inventory` is NOT modified, except that an expired Job entry
  is removed

#### Scenario: NoOp removes an expired Job entry
- **WHEN** the outcome is `NoOp` and a rendered Job that sets
  `ttlSecondsAfterFinished` does not exist on the cluster
- **THEN** the patch removes that Job from `status.inventory.entries` and sets
  `status.inventory.count` to the number of entries left
- **AND** `status.inventory.digest` and `status.inventory.revision` keep their
  values

#### Scenario: NoOp refreshes the demand the render just reported
- **WHEN** the outcome is `NoOp` and the render that preceded it reported a
  different contract demand than the status carries
- **THEN** `status.requiredContracts` is updated to what the render reported

#### Scenario: NoOp patch does not trigger reconcile loop
- **WHEN** a NoOp patch updates `status` (drift condition or counters)
- **THEN** the resulting watch event is filtered by `GenerationChangedPredicate`
- **AND** no follow-up reconcile is queued from the status patch alone

#### Scenario: A skipped render patches nothing
- **WHEN** a reconcile skips its render because its inputs are unchanged and
  its `Healthy` judgement is the one already on status
- **THEN** no status patch is sent
- **AND** `lastAttemptedAt`, history and `status.lastAppliedInputs.renderedAt`
  keep their values

#### Scenario: A skipped render records a changed health judgement
- **WHEN** a reconcile skips its render and the inventory's Deployment has
  rolled out since the last judgement (`Healthy` was `NotRolledOut`)
- **THEN** the only status change is `Healthy=True` with reason `RolledOut`
- **AND** `observedGeneration`, `lastAttemptedAt`, history and
  `status.lastAppliedInputs.renderedAt` keep their values

### Requirement: Inventory updated only on full success
The `status.inventory` MUST only be replaced after a fully successful apply (and prune, if enabled). One narrower write is allowed: a ModuleInstance reconcile whose digests are unchanged MUST remove the entries of expired Jobs (`drift-detection`, "A missing object is restored") from `status.inventory.entries`, on a `NoOp` and on a restore. `status.inventory.digest` MUST stay the digest of the rendered set, so after such a removal it is not the digest of the entries listed.

#### Scenario: Partial failure preserves inventory
- **WHEN** apply succeeds but prune fails
- **THEN** `status.inventory` remains at the previous successful value

#### Scenario: An expired Job leaves the inventory without an apply
- **WHEN** a reconcile renders with unchanged digests, sends no apply, and a rendered Job that sets `ttlSecondsAfterFinished` does not exist
- **THEN** `status.inventory.entries` no longer lists the Job
- **AND** the next reconcile with unchanged digests is a `NoOp`
