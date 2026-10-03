## Purpose

The `reconcile-backoff` capability defines how the controller schedules the
next attempt after a failed reconcile: transient failures retry on a bounded
exponential backoff driven by `failureCounters.reconcile`, while stalled
failures wait for the fixed stalled recheck interval.

## Requirements

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

### Requirement: Acquisition failures without a typed terminal cause are transient

When a ModuleInstance's module acquisition or a ModulePackage's package load fails, the controller SHALL classify the failure by its type, never by its message text. A failure that carries none of the typed terminal causes (an identity mismatch, a wrong artifact kind, a structurally invalid package, a missing required field, unresolved platform demands) SHALL set `Ready=False` with reason `ResolutionFailed`, SHALL NOT set `Stalled=True`, SHALL emit a Warning event carrying the error message, and SHALL produce the outcome `FailedTransient`, so the object requeues on the exponential backoff capped at 5 minutes and `status.nextRetryAt` and `failureCounters.reconcile` follow the transient path. A failure that carries a typed terminal cause SHALL set `Ready=False` and `Stalled=True` and produce `FailedStalled`, requeued on the 30-minute recheck. The ModuleInstance and ModulePackage loops SHALL use one shared classification so the two cannot drift. The status message SHALL be the acquisition error's message unchanged.

#### Scenario: Registry outage during ModuleInstance acquisition retries on the backoff

- **GIVEN** a ModuleInstance with `failureCounters.reconcile=0` and a generated platform
- **WHEN** fetching its module from the registry fails with an error that carries no typed cause
- **THEN** the ModuleInstance reports `Ready=False` with reason `ResolutionFailed`, no `Stalled` condition, a Warning event carrying the acquisition error, and the controller returns `RequeueAfter: 5s`

#### Scenario: Registry outage while loading a ModulePackage retries on the backoff

- **GIVEN** a ModulePackage whose artifact fetched and whose path resolved
- **WHEN** loading the package fails because a CUE dependency in its `cue.mod/module.cue` cannot be resolved from the registry
- **THEN** the ModulePackage reports `Ready=False` with reason `ResolutionFailed`, no `Stalled` condition, and requeues on the exponential backoff rather than the 30-minute recheck

#### Scenario: Repeated acquisition failures cap at five minutes

- **GIVEN** an object with `failureCounters.reconcile=10` whose acquisition keeps failing without a typed cause
- **WHEN** the next reconcile fails the same way
- **THEN** the controller requeues after 5 minutes, not 30

#### Scenario: Recovery once the registry answers

- **GIVEN** an object reporting a transient `ResolutionFailed` after an acquisition failure
- **WHEN** the next retry acquires the module and renders it
- **THEN** the object reports `Ready=True`, `status.nextRetryAt` is nil and `failureCounters.reconcile` resets

#### Scenario: Typed terminal causes stay stalled

- **WHEN** an acquisition fails with an identity mismatch, a wrong artifact kind, a structurally invalid package or a missing required field
- **THEN** the object reports `Ready=False` and `Stalled=True` and requeues on the 30-minute recheck; the reason is `ResolutionFailed`, except a ModulePackage of the wrong kind, which keeps `UnsupportedKind`

#### Scenario: Message text does not classify

- **WHEN** a render fails after acquisition, for example in instance synthesis, with a message that contains `loading package`, `resolving` or `synthesizing release`
- **THEN** the failure is classified by its typed cause alone: with none, the object reports `RenderFailed` and `Stalled=True`
