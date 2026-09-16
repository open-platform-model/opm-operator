## Requirements

### Requirement: Full reconcile loop execution
The `ModuleReleaseReconciler` MUST execute phases 0-7 sequentially when a ModuleRelease is reconciled.

#### Scenario: First successful reconcile
- **WHEN** a ModuleRelease is created with a valid sourceRef and the OCIRepository is ready
- **THEN** the controller resolves the source, fetches the artifact, renders resources, applies them via SSA, updates status with conditions/digests/inventory/history, and sets `Ready=True`

#### Scenario: Source not yet ready
- **WHEN** the referenced OCIRepository exists but is not ready
- **THEN** the controller sets `SourceReady=False`, `Ready=Unknown`, `Reconciling=True`, and waits for a source event

#### Scenario: Render failure
- **WHEN** the CUE module fails to evaluate (e.g., invalid values)
- **THEN** the controller sets `Ready=False`, `Stalled=True` with reason `RenderFailed`, and does NOT modify inventory or attempt apply

#### Scenario: Apply failure
- **WHEN** SSA apply fails (e.g., API server error)
- **THEN** the controller sets `Ready=False` with reason `ApplyFailed`, does NOT prune, and does NOT update `lastApplied*` digests

### Requirement: Suspend check
The reconciler MUST skip reconciliation when `spec.suspend` is true.

#### Scenario: Suspended release
- **WHEN** `spec.suspend` is true
- **THEN** the controller sets condition reason `Suspended` and returns without requeue

### Requirement: No-op detection
The reconciler MUST detect no-op reconciliations and skip apply/prune when nothing changed.

#### Scenario: All digests match
- **WHEN** source, config, render, and inventory digests all match the last applied values
- **THEN** the controller skips apply and prune, keeps `Ready=True`, and does not record a new history entry

### Requirement: Outcome classification
The reconciler MUST classify each reconcile attempt as one of: `NoOp`, `Applied`, `AppliedAndPruned`, `FailedTransient`, `FailedStalled`.

#### Scenario: Transient failure requeues with explicit backoff
- **WHEN** the outcome is `FailedTransient`
- **THEN** the controller returns `ctrl.Result{RequeueAfter: backoff}` with nil error, where backoff is computed from `failureCounters.reconcile`

#### Scenario: Stalled failure requeues with safety interval
- **WHEN** the outcome is `FailedStalled`
- **THEN** the controller returns `ctrl.Result{RequeueAfter: 30m}` with nil error

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

### Requirement: Inventory updated only on full success
The `status.inventory` MUST only be replaced after a fully successful apply (and prune, if enabled).

#### Scenario: Partial failure preserves inventory
- **WHEN** apply succeeds but prune fails
- **THEN** `status.inventory` remains at the previous successful value

### Requirement: Temp directory cleanup
The reconciler MUST clean up any temporary directories used for artifact extraction, even on error.

#### Scenario: Cleanup on error
- **WHEN** a phase fails after artifact extraction
- **THEN** the temp directory is removed via deferred cleanup

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
