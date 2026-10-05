## ADDED Requirements

### Requirement: The applied module version is recorded in plain text

The ModuleInstance and ModulePackage reconcilers SHALL record on `status.lastAppliedVersion` the version of the module whose render they last applied, as plain text beside the `lastApplied*` digests. The value SHALL be the version the rendered instance's source module declares in its `metadata.version` (bare SemVer, for example `0.1.0`), read the same way on both kinds; for a ModuleInstance it is not copied from `spec.module.version`.

The field SHALL be written together with the other `lastApplied*` fields, on an attempt whose apply (and prune, when enabled) succeeded. An attempt that fails, panics, is suspended, belongs to a CLI-owned instance, or is the operator's own instance (refused) SHALL leave it as it was.

When the outcome is `NoOp` and the attempt rendered, the reconciler SHALL set it to the version that render reported, whatever the field held: a `NoOp` means every digest equals the last apply's, and the source digest pins the module version, so the rendered version is the applied one. This fills the field on an object applied before it existed and corrects it after an ownership handback, where the cli moved the digests without touching this field. A reconcile that did not render (for example one that skips the render because its inputs are unchanged) SHALL leave the field as it was.

A version that cannot be read as a concrete string SHALL NOT fail the reconcile. A successful apply then writes the field empty, so it never names a version the last apply did not render.

The version SHALL NOT be an input to no-op detection; the source digest already covers it.

Source: owner decision j5 of the kernel-plan walkthrough (2026-10-03): "Operator adds an additive status.lastAppliedVersion (plain text) next to the digest." The ADR-008 amendment (library#167) carries only j5's deletion half.

#### Scenario: A successful apply records the module version

- **WHEN** a ModuleInstance whose module declares `metadata.version: "0.1.0"` is reconciled and its apply succeeds
- **THEN** `status.lastAppliedVersion` is `0.1.0`
- **AND** it is written in the same status patch as `lastAppliedAt` and the `lastApplied*` digests

#### Scenario: A ModulePackage records the version of the module its instance renders

- **WHEN** a ModulePackage whose `instance.cue` renders a module declaring `metadata.version: "0.1.0"` is reconciled and its apply succeeds
- **THEN** `status.lastAppliedVersion` is `0.1.0`

#### Scenario: A failed apply keeps the previous version

- **WHEN** an instance that recorded `lastAppliedVersion` `0.1.0` is changed to a module at `0.2.0` and the apply fails
- **THEN** `status.lastAppliedVersion` is still `0.1.0`
- **AND** the `lastApplied*` digests are unchanged too

#### Scenario: A NoOp fills an empty field

- **WHEN** an object that was applied before the field existed has no `lastAppliedVersion` and its next reconcile is a `NoOp`
- **THEN** `status.lastAppliedVersion` is set to the version the render reported
- **AND** `lastAttempted*`, `inventory` and history are not modified

#### Scenario: A NoOp after an ownership handback corrects a stale version

- **WHEN** `status.lastAppliedVersion` holds `9.9.9`, the module renders at `0.1.0`, and the reconcile is a `NoOp` because the `lastApplied*` digests already match that render
- **THEN** `status.lastAppliedVersion` is `0.1.0`

#### Scenario: An apply whose render reports no version clears the field

- **WHEN** an instance that recorded `lastAppliedVersion` `0.1.0` renders a changed set whose module version cannot be read, and the apply succeeds
- **THEN** `status.lastAppliedVersion` is absent from the object

## MODIFIED Requirements

### Requirement: Status always patched
The reconciler MUST patch `ModuleRelease.status` at the end of every reconcile
attempt, including `NoOp`. On meaningful outcomes (`Applied`, `AppliedAndPruned`,
`FailedTransient`, `FailedStalled`), the patch updates conditions,
`lastAttempted*`, `lastApplied*` (on success), `inventory` (on success),
history, failure counters, and `nextRetryAt`. On `NoOp`, the patch is bounded
to: drift condition (`Drifted`), failure counter deltas (incl. drift counter),
`nextRetryAt` clearing, `requiredContracts`, and `lastAppliedVersion` when the
attempt rendered. `lastAttempted*`, `inventory`, and history MUST NOT be modified on
`NoOp` — those fields describe meaningful reconcile outcomes.

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
