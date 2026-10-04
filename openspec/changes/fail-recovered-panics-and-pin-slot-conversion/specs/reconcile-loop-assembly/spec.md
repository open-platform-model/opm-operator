## ADDED Requirements

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
