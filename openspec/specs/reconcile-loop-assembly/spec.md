## Purpose

Define how the operator's reconcile loops for ModuleInstance and ModulePackage
run their phases in order, classify each attempt's outcome, and always commit
an honest status for it, including an attempt that ends in a recovered panic,
so that Ready, the counters, the history and the inventory describe what the
attempt actually did.

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

### Requirement: A rendered resource may be withheld from apply

The reconciler SHALL evaluate the rendered resources before applying them and MAY withhold an individual resource whose application would break a guarantee the operator holds. A withheld resource SHALL NOT be applied, and every other rendered resource SHALL be applied as usual, so one refused object does not strand the rest of the module.

Withholding SHALL NOT change the inventory. The inventory records which resources the instance owns, and the operator still owns a resource it declined to update; removing it would mark it stale and prune the very object the refusal exists to protect.

A reconcile that withheld a resource SHALL NOT report ready and SHALL NOT be treated as a no-op, because the cluster does not hold what the render produced.

#### Scenario: The rest of the module still applies

- **WHEN** one rendered resource is withheld
- **THEN** every other rendered resource is applied, and the apply is not reported as failed

#### Scenario: The withheld resource stays owned

- **WHEN** a resource is withheld
- **THEN** the inventory still lists it, and the prune does not delete it

#### Scenario: A withheld resource keeps the instance unconverged

- **WHEN** a reconcile withholds a resource
- **THEN** the instance reports not ready, and the outcome is not a no-op

#### Scenario: Nothing withheld behaves as before

- **WHEN** no rendered resource is withheld
- **THEN** apply, inventory, prune and the reported outcome are exactly as they were

### Requirement: A recovered panic is recorded as a failed attempt

When a ModuleInstance or ModulePackage reconcile panics after its deferred status commit is installed, the reconciler MUST NOT report the attempt as a success. Before the panic leaves the reconcile function, the deferred commit MUST record the attempt as a transient failure and patch status:

- `Ready=False` with reason `ReconcilePanic` and a message that starts with `reconcile panicked: ` followed by the panic value.
- `Reconciling=True`, and no `Stalled` condition.
- `lastAttempted*` set for this attempt, and a failure history entry carrying the same message.
- `failureCounters.reconcile` incremented, as for any `FailedTransient` outcome.
- The drift, apply and prune failure counters left as they were. A phase that was in flight when the panic hit did not succeed, so a panicking attempt never resets a phase counter.
- `nextRetryAt` cleared, because the retry is scheduled by the controller runtime's rate limiter, not by the operator's backoff.
- `lastApplied*` and `inventory` left as they were.

Fields the attempt already wrote on the object before the panic (`requiredContracts`, `instanceUUID`, the `Drifted` condition, a ModulePackage's `status.source`) are committed as on any other failed attempt.

A panicking attempt is counted as `FailedTransient` for failure counters, history and reconcile metrics only. It returns no `ctrl.Result`: the requeue scenarios of "Outcome classification" and the `nextRetryAt` rule of the reconcile-backoff capability do not apply to it.

The reconciler MUST then re-panic with the original panic value, so the controller runtime still recovers, logs (with the stack) and counts the panic and requeues the object. The operator SHOULD log the panic value with the object's name and namespace before it re-panics.

#### Scenario: A panicking render does not mark a ready instance Ready

- **WHEN** a ModuleInstance that is `Ready=True` from an earlier successful reconcile is reconciled and its renderer panics
- **THEN** the panic propagates out of the reconcile with its original value
- **AND** the stored instance has `Ready=False` with reason `ReconcilePanic`, `Reconciling=True`, no `Stalled` condition, `failureCounters.reconcile` incremented, a failure history entry and no `nextRetryAt`
- **AND** its `inventory` and `lastAppliedRenderDigest` are unchanged from the earlier reconcile

#### Scenario: A panicking render does not mark a ready package Ready

- **WHEN** a ModulePackage that is `Ready=True` from an earlier successful reconcile is reconciled and its renderer panics
- **THEN** the panic propagates out of the reconcile with its original value
- **AND** the stored package has `Ready=False` with reason `ReconcilePanic`, `Reconciling=True`, no `Stalled` condition, `failureCounters.reconcile` incremented, a failure history entry and no `nextRetryAt`
- **AND** its `inventory` and `lastAppliedRenderDigest` are unchanged from the earlier reconcile

#### Scenario: A panic during conversion is recorded and frees the slot

- **WHEN** the export of a ModuleInstance's render result for apply panics while the reconcile holds a render slot
- **THEN** the panic propagates with its original value, the render slot is free again, and the stored instance has `Ready=False` with reason `ReconcilePanic`

#### Scenario: A panic inside a phase does not reset that phase's counter

- **WHEN** a reconcile panics inside drift detection (ModuleInstance) or apply (ModulePackage), after that phase has started
- **THEN** the drift, apply and prune failure counters keep their earlier values and `failureCounters.reconcile` is incremented
