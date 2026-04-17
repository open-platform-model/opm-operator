## ADDED Requirements

### Requirement: Exponential backoff for transient failures
The controller MUST compute an exponential backoff delay from `failureCounters.reconcile` when the reconcile outcome is `FailedTransient`, using the formula `min(baseDelay * 2^(failures-1), maxDelay)` with `baseDelay=5s` and `maxDelay=5m`.

#### Scenario: First transient failure
- **GIVEN** a ModuleRelease with `failureCounters.reconcile=0`
- **WHEN** the reconcile outcome is `FailedTransient`
- **THEN** the controller returns `RequeueAfter: 5s` and does NOT return a non-nil error

#### Scenario: Third consecutive transient failure
- **GIVEN** a ModuleRelease with `failureCounters.reconcile=2`
- **WHEN** the reconcile outcome is `FailedTransient`
- **THEN** the controller returns `RequeueAfter: 20s`

#### Scenario: Backoff cap reached
- **GIVEN** a ModuleRelease with `failureCounters.reconcile=10`
- **WHEN** the reconcile outcome is `FailedTransient`
- **THEN** the controller returns `RequeueAfter: 5m` (capped at maxDelay)

### Requirement: Periodic safety recheck for stalled failures
The controller MUST return `RequeueAfter: 30m` when the reconcile outcome is `FailedStalled`, as a safety net against misclassification.

#### Scenario: Stalled failure schedules recheck
- **WHEN** the reconcile outcome is `FailedStalled`
- **THEN** the controller returns `RequeueAfter: 30m` and does NOT return a non-nil error

### Requirement: nextRetryAt status field
The controller MUST set `status.nextRetryAt` to the computed retry time when returning `RequeueAfter` for failed outcomes, and MUST clear it (set to nil) on successful outcomes (`NoOp`, `Applied`, `AppliedAndPruned`).

#### Scenario: nextRetryAt set on transient failure
- **GIVEN** a ModuleRelease with `failureCounters.reconcile=1`
- **WHEN** the reconcile outcome is `FailedTransient`
- **THEN** `status.nextRetryAt` is set to approximately `now + 10s`

#### Scenario: nextRetryAt set on stalled failure
- **WHEN** the reconcile outcome is `FailedStalled`
- **THEN** `status.nextRetryAt` is set to approximately `now + 30m`

#### Scenario: nextRetryAt cleared on success
- **GIVEN** a ModuleRelease with `status.nextRetryAt` set
- **WHEN** the reconcile outcome is `Applied`
- **THEN** `status.nextRetryAt` is nil

#### Scenario: nextRetryAt cleared on no-op
- **GIVEN** a ModuleRelease with `status.nextRetryAt` set
- **WHEN** the reconcile outcome is `NoOp`
- **THEN** `status.nextRetryAt` is nil

### Requirement: Generation-based event filtering
The controller MUST use `predicate.GenerationChangedPredicate` as an event filter so that status-only updates (which do not increment `metadata.generation`) do not enqueue a reconcile.

#### Scenario: Status-only update filtered
- **WHEN** a ModuleRelease status patch changes `resourceVersion` but not `metadata.generation`
- **THEN** the controller does NOT enqueue a reconcile for that event

#### Scenario: Spec change passes through
- **WHEN** a ModuleRelease spec change increments `metadata.generation`
- **THEN** the controller enqueues a reconcile for that event

#### Scenario: Scheduled requeue unaffected
- **WHEN** a reconcile returns `RequeueAfter: 5s`
- **THEN** the workqueue re-enqueues the item after 5s regardless of predicates

### Requirement: Custom workqueue rate limiter
The controller MUST configure a custom workqueue rate limiter with a 1s base delay and 5m max delay, replacing the default 5ms base.

#### Scenario: Safety-net rate limiting
- **WHEN** a reconcile returns a non-nil error (unexpected failure path)
- **THEN** the workqueue rate limiter applies at least a 1s delay before the next attempt
