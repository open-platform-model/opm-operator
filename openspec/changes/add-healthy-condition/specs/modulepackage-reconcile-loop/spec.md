## MODIFIED Requirements

### Requirement: Status always patched
The `ReleaseReconciler` MUST patch `Release.status` at the end of every reconcile attempt, including NoOp. The status shape mirrors ModuleRelease: conditions, digests, inventory, history, failure counters, `nextRetryAt`, and `lastAppliedInputs`. A reconcile that skips its render because its inputs are unchanged (`render-input-key`) is not an attempt. It judges health (`instance-health`) and MUST NOT patch anything but the `Healthy` condition, which it patches only when the judgement changed it; it never writes the transient `Reconciling` condition the attempt set before the skip. It requeues after `spec.interval`, or sooner when health asks for it. On a successful outcome and on `NoOp`, the patch also carries the judged `Healthy` condition.

#### Scenario: Status updated on failure
- **WHEN** a phase fails
- **THEN** status conditions, `lastAttempted*` fields, `failureCounters`, and `nextRetryAt` are updated

#### Scenario: Successful reconcile status
- **WHEN** all phases succeed
- **THEN** `Ready=True`, `lastApplied*` digests are set, inventory is replaced, and a success history entry is recorded

#### Scenario: A skipped interval patches nothing
- **WHEN** the interval requeue finds the package's inputs unchanged within the drift render interval, and the package is `Healthy=True` with reason `RolledOut` and still rolled out
- **THEN** no status patch is sent and the reconcile requeues after `spec.interval`

#### Scenario: A skipped interval records a changed health judgement
- **WHEN** the interval requeue skips the render and an inventory Deployment has lost its available replicas since the package was `RolledOut`
- **THEN** the only status change is `Healthy=False` with reason `NotRolledOut`, `Reconciling` is not written, and the reconcile requeues after the shorter of the health requeue and `spec.interval`
