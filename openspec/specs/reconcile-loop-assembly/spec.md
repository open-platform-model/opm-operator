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
The reconciler MUST detect no-op reconciliations and skip apply/prune when nothing changed. A reconcile is a no-op only when every digest matches, no restorable rendered object is missing from the cluster (`drift-detection`, "A missing object is restored"), and no rendered object that the apply verdict allows exists outside `status.inventory` (`reconcile-loop-assembly`, "An allowed object outside the inventory is taken in").

#### Scenario: All digests match
- **WHEN** source, config, render, and inventory digests all match the last applied values
- **AND** no restorable rendered object is missing from the cluster
- **AND** every rendered object that exists is in `status.inventory` or is adopted by another instance
- **THEN** the controller skips apply and prune, keeps `Ready=True`, and does not record a new history entry

#### Scenario: All digests match and an object is missing
- **WHEN** all four digests match the last applied values and a restorable rendered object does not exist on the cluster
- **THEN** the controller applies the missing object, prunes nothing, and records a history entry

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
The `status.inventory` MUST only be replaced after a fully successful apply (and prune, if enabled). Two narrower writes are allowed. A ModuleInstance reconcile whose digests are unchanged MUST remove the entries of expired Jobs (`drift-detection`, "A missing object is restored") from `status.inventory.entries`, on a `NoOp` and on a restore. A reconcile that renders MUST leave out of the inventory it records, on a `NoOp` as well, the entry of every object the apply verdict refuses as `adopted-elsewhere` (0012:D8:R8). `status.inventory.digest` MUST stay the digest of the rendered set, so after such a removal it is not the digest of the entries listed.

A reconcile that is refused by the apply verdict, or that fails, MUST NOT change `status.inventory`.

#### Scenario: Partial failure preserves inventory
- **WHEN** apply succeeds but prune fails
- **THEN** `status.inventory` remains at the previous successful value

#### Scenario: An expired Job leaves the inventory without an apply
- **WHEN** a reconcile renders with unchanged digests, sends no apply, and a rendered Job that sets `ttlSecondsAfterFinished` does not exist
- **THEN** `status.inventory.entries` no longer lists the Job
- **AND** the next reconcile with unchanged digests is a `NoOp`

#### Scenario: An adopted object leaves the inventory without an apply
- **GIVEN** a Ready ModuleInstance and an inventoried ConfigMap that a user annotates `opmodel.dev/adopt` with another instance's UUID
- **WHEN** a reconcile renders with unchanged digests
- **THEN** it sends no apply, `status.inventory.entries` no longer lists the ConfigMap and `Ready` stays `True`
- **AND** the next reconcile with unchanged digests is a `NoOp`

#### Scenario: A refused reconcile keeps the inventory
- **WHEN** the apply verdict refuses a reconcile
- **THEN** `status.inventory` holds the entries it held before

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

### Requirement: A healthy ModuleInstance is reconciled periodically

The operator SHALL take an instance reconcile interval from the flag `--instance-reconcile-interval` (default 10 minutes). When a ModuleInstance reconcile ends with outcome `NoOp`, `Applied` or `AppliedAndPruned`, or skips its render because its inputs are unchanged, and the health judgement asks for no requeue, the reconcile SHALL requeue after the interval plus a random addition of at most 10 percent of it. When the health judgement asks for a requeue, that requeue SHALL apply unchanged. The periodic requeue SHALL NOT set `status.nextRetryAt` and SHALL NOT count as a failure.

An interval of `0` SHALL disable the periodic requeue. The operator MUST refuse to start with a negative interval. The interval is operator-wide: the ModuleInstance API SHALL NOT gain a field for it.

A suspended ModuleInstance, a CLI-owned ModuleInstance and the operator's own instance SHALL NOT be requeued by the interval. A failed reconcile SHALL keep the requeue of its failure class.

The periodic reconcile of an instance whose inputs and objects are unchanged SHALL end as a skipped render or as a `NoOp`: it sends no apply, records no history entry and changes no condition.

#### Scenario: A rolled-out instance requeues on the interval

- **WHEN** an operator-owned ModuleInstance reconcile ends `Applied` with `Healthy=True` and the interval is 10 minutes
- **THEN** the reconcile requeues after at least 10 and at most 11 minutes
- **AND** `status.nextRetryAt` is unset

#### Scenario: A NoOp and a skipped render requeue on the interval

- **WHEN** a later reconcile of the same unchanged instance ends as a `NoOp` or skips its render
- **THEN** it requeues after at least 10 and at most 11 minutes

#### Scenario: A skipped periodic reconcile writes nothing

- **WHEN** the periodic reconcile of an unchanged, rolled-out instance skips its render
- **THEN** no patch and no status patch is sent for the instance

#### Scenario: A suspended instance is not requeued

- **WHEN** a ModuleInstance has `spec.suspend: true` and the interval is 10 minutes
- **THEN** its reconcile returns without a requeue

#### Scenario: A CLI-owned instance is not requeued

- **WHEN** a ModuleInstance has `spec.owner: cli` and the interval is 10 minutes
- **THEN** its reconcile returns without a requeue

#### Scenario: The interval is disabled

- **WHEN** the interval is `0` and a ModuleInstance reconcile ends rolled out
- **THEN** the reconcile returns without a requeue

#### Scenario: A negative interval stops the operator

- **WHEN** the operator starts with `--instance-reconcile-interval=-1s`
- **THEN** it logs the invalid value and exits with a non-zero status

### Requirement: The instance identities are stored before the first write of an apply
A ModuleInstance and a ModulePackage MUST carry in `status.instanceUUID` the identity of the instance the operator last rendered and began to apply, and in the optional field `status.previousInstanceUUID` the identity it had before, for as long as a change of identity is not settled. Source: owner decision of 2026-10-08 (the status holds the earlier and the new identity until the prune of that reconcile succeeded; then the earlier one is cleared).

When a render's identity differs from `status.instanceUUID`, the reconciler MUST store the new state in the object's status before it applies any object, and MUST NOT apply when that write fails:

- `status.instanceUUID` empty: it becomes the render's identity and `status.previousInstanceUUID` stays empty.
- `status.instanceUUID` is `A`, `status.previousInstanceUUID` is empty and the render carries `B`: `status.instanceUUID` becomes `B` and `status.previousInstanceUUID` becomes `A`.
- `status.instanceUUID` is `B`, `status.previousInstanceUUID` is `A` and the render carries `A`: the two values swap.

`status.previousInstanceUUID` MUST be cleared in the status commit of a reconcile whose apply and prune succeeded, together with the new inventory, and MUST NOT be cleared by any other outcome: a failed or refused apply, a failed prune, a reconcile that applies nothing, or a recovered panic. A reconcile that applies nothing MUST write `status.instanceUUID` when it is empty and MUST leave both fields alone otherwise.

While `status.previousInstanceUUID` is set, or a render is about to set it, a reconcile MUST NOT end as one that applies nothing, also when the render equals what was last applied: it MUST apply and prune, so that the objects an unsettled apply relabelled are relabelled back and the change is settled.

#### Scenario: First reconcile records the identity
- **GIVEN** a new ModuleInstance whose render carries identity `A`
- **WHEN** the reconcile applies with success
- **THEN** `status.instanceUUID` is `A` and `status.previousInstanceUUID` is empty

#### Scenario: Both identities are stored before the apply
- **GIVEN** a ModuleInstance with `status.instanceUUID` `A` whose `spec.module.path` changes, so that its render carries identity `B`
- **WHEN** the apply fails
- **THEN** the stored object has `status.instanceUUID` `B` and `status.previousInstanceUUID` `A`
- **AND** `status.inventory` is unchanged

#### Scenario: A refused apply keeps both identities
- **GIVEN** the same change of identity, and an apply that succeeds while the reconcile is refused afterwards because dependents remain
- **WHEN** the reconcile ends
- **THEN** `status.instanceUUID` is `B` and `status.previousInstanceUUID` is `A`

#### Scenario: A panic keeps both identities
- **GIVEN** the same change of identity, and a reconcile that panics after the identities were stored
- **WHEN** the panic was recorded as a failed attempt
- **THEN** `status.instanceUUID` is `B` and `status.previousInstanceUUID` is `A`

#### Scenario: Full success settles the change
- **GIVEN** a ModuleInstance with `status.instanceUUID` `B` and `status.previousInstanceUUID` `A`
- **WHEN** a reconcile applies and prunes with success
- **THEN** `status.previousInstanceUUID` is empty and `status.instanceUUID` is `B`

#### Scenario: Going back before the change is settled
- **GIVEN** a ModuleInstance with `status.instanceUUID` `B` and `status.previousInstanceUUID` `A`, whose `spec.module.path` is set back so that its render carries `A`
- **WHEN** the apply starts
- **THEN** `status.instanceUUID` is `A` and `status.previousInstanceUUID` is `B`

#### Scenario: Going back renders what was last applied
- **GIVEN** a ModuleInstance whose last successful apply carried identity `A`, with `status.instanceUUID` `B` and `status.previousInstanceUUID` `A` after an apply of `B` whose prune failed, and whose `spec.module.path` is set back so that its render equals what was last applied
- **WHEN** the controller reconciles
- **THEN** the render is applied, and the objects that carry `B` carry `A` again
- **AND** after the reconcile `status.instanceUUID` is `A`, `status.previousInstanceUUID` is empty and `Ready` is `True`

#### Scenario: An object without the field gains it without an apply
- **GIVEN** a ModuleInstance with an inventory and an empty `status.instanceUUID`, whose render equals what was applied
- **WHEN** the controller reconciles and applies nothing
- **THEN** `status.instanceUUID` holds the render's identity and `status.previousInstanceUUID` is empty

#### Scenario: The identities cannot be stored
- **GIVEN** a change of identity, and an API server that refuses the status write
- **WHEN** the controller reconciles
- **THEN** no object of the render is applied and the reconcile is retried

### Requirement: A second identity change waits until the first is settled
While `status.previousInstanceUUID` is set, a render whose identity is neither `status.instanceUUID` nor `status.previousInstanceUUID` MUST NOT be applied, and nothing MUST be pruned for it. The reconcile MUST report `Ready=False` and `Stalled=True` with reason `IdentityChangeUnsettled`, MUST leave both identity fields and the inventory as they are, and MUST recheck on the stalled interval. The deletion cleanup is not affected.

#### Scenario: A third identity is refused
- **GIVEN** a ModuleInstance with `status.instanceUUID` `B` and `status.previousInstanceUUID` `A`, whose `spec.module.path` changes again so that its render carries identity `C`
- **WHEN** the controller reconciles
- **THEN** no object is applied or deleted
- **AND** `Ready` is `False` and `Stalled` is `True` with reason `IdentityChangeUnsettled`
- **AND** `status.instanceUUID` is `B` and `status.previousInstanceUUID` is `A`

#### Scenario: The refusal clears when the earlier path is restored
- **GIVEN** that ModuleInstance
- **WHEN** `spec.module.path` is set back so that the render carries `B`, and the reconcile applies and prunes with success
- **THEN** `Ready` is `True` and `status.previousInstanceUUID` is empty

#### Scenario: Deletion still works
- **GIVEN** that ModuleInstance with reason `IdentityChangeUnsettled` and `spec.prune=true`
- **WHEN** it is deleted
- **THEN** the cleanup deletes its inventory objects that carry `A` or `B` and removes the finalizer

### Requirement: A reconcile the apply verdict refuses is retried and not stalled
When the apply verdict refuses a reconcile (`ssa-apply`, "An apply is judged by the ownership verdict before its first write"), the reconciler MUST set `Ready=False` with reason `ApplyRefused`, MUST NOT set `Stalled`, MUST count the attempt as a failed apply and MUST retry on the bounded backoff. `status.inventory`, the applied digests, `status.instanceUUID` and `status.previousInstanceUUID` MUST keep their values. A reconcile whose digests are unchanged MUST NOT refuse because an inventoried object is being deleted: it writes nothing over that object.

#### Scenario: A refusal is transient
- **GIVEN** a changed render that names a live object OPM does not manage
- **WHEN** the controller reconciles
- **THEN** `Ready` is `False` with reason `ApplyRefused`, `Stalled` is absent, and the reconcile is requeued on the backoff

#### Scenario: The refusal ends when the object is adopted
- **GIVEN** the instance of the scenario above
- **WHEN** a user annotates the object `opmodel.dev/adopt` with the instance's UUID and the retry runs
- **THEN** the render is applied and `Ready` is `True`

#### Scenario: A refused identity change stores nothing
- **GIVEN** a ModuleInstance whose `spec.module.path` changed, and a rendered object that the verdict refuses
- **WHEN** the controller reconciles
- **THEN** `status.instanceUUID` keeps the earlier identity and `status.previousInstanceUUID` stays empty

#### Scenario: An inventoried object being deleted on unchanged digests
- **GIVEN** a Ready ModuleInstance with unchanged digests and an inventoried Deployment that carries a deletion timestamp
- **WHEN** the controller reconciles
- **THEN** `Ready` does not change to `ApplyRefused`

#### Scenario: A rendered object held by another instance on unchanged digests
- **GIVEN** a ModuleInstance with unchanged digests whose render names an object that exists outside its inventory and carries another instance's UUID label and no adopt annotation
- **WHEN** the controller reconciles
- **THEN** nothing is written and `Ready` is `False` with reason `ApplyRefused`

### Requirement: An allowed object outside the inventory is taken in
When a rendered object exists, is not in `status.inventory`, and the apply verdict allows it, the reconciler MUST apply it and MUST record it in `status.inventory` in the same reconcile, also when every digest is unchanged. With unchanged digests a ModuleInstance MUST apply only such objects and the restorable missing objects, as a restore does, and MUST NOT rewrite an object that is in the inventory. Source: 0012:D8:R2.

#### Scenario: An adopted object is taken in on unchanged digests
- **GIVEN** a Ready ModuleInstance whose render names a ConfigMap it let go earlier, and a user who sets the ConfigMap's `opmodel.dev/adopt` annotation to this instance's UUID
- **WHEN** a reconcile renders with unchanged digests
- **THEN** the ConfigMap is applied and listed in `status.inventory`
- **AND** no other existing object is written

#### Scenario: A kept claim is taken back
- **GIVEN** a PersistentVolumeClaim that a prune kept, so it is out of the inventory and carries this instance's labels, and a render that names it again
- **WHEN** the controller reconciles
- **THEN** the claim is applied and listed in `status.inventory`
