## MODIFIED Requirements

### Requirement: Status always patched
The `ReleaseReconciler` MUST patch `Release.status` at the end of every reconcile attempt, including NoOp. The status shape mirrors ModuleRelease: conditions, digests, inventory, history, failure counters, `nextRetryAt`, and `lastAppliedInputs`. A reconcile that skips its render because its inputs are unchanged (`render-input-key`) is not an attempt and MUST NOT patch status; it requeues after `spec.interval`.

#### Scenario: Status updated on failure
- **WHEN** a phase fails
- **THEN** status conditions, `lastAttempted*` fields, `failureCounters`, and `nextRetryAt` are updated

#### Scenario: Successful reconcile status
- **WHEN** all phases succeed
- **THEN** `Ready=True`, `lastApplied*` digests are set, inventory is replaced, and a success history entry is recorded

#### Scenario: A skipped interval patches nothing
- **WHEN** the interval requeue finds the package's inputs unchanged within the drift render interval
- **THEN** no status patch is sent and the reconcile requeues after `spec.interval`
