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

### Requirement: Registry fetch failures are transient wherever they occur

When a render fails, the ModuleInstance and ModulePackage reconcilers SHALL classify the failure by its type, never by its message text, with one shared classification so the two cannot drift. A failure is transient when its error chain holds the library's typed registry fetch failure (`*oerrors.FetchError`, of any `Kind`: unreachable, not found, unauthorized or other) and holds none of the typed terminal causes (an identity mismatch, a wrong artifact kind, a structurally invalid package, a missing required field, unresolved platform demands, unmatched components). This holds for every kernel call the render makes: module acquisition, values compile, instance synthesis, the render build and the package load.

A transient failure SHALL set `Ready=False` with reason `ResolutionFailed`, SHALL NOT set `Stalled=True`, SHALL emit a Warning event carrying the error message, and SHALL produce the outcome `FailedTransient`, so the object requeues on the exponential backoff capped at 5 minutes and `status.nextRetryAt` and `failureCounters.reconcile` follow the transient path.

Every other failure SHALL set `Ready=False` and `Stalled=True` and produce `FailedStalled`, requeued on the 30-minute recheck. Its reason follows the typed cause; a failure during module acquisition or package load that carries no other typed cause is `ResolutionFailed`. The status message SHALL be the error's message unchanged.

#### Scenario: Registry outage during ModuleInstance acquisition retries on the backoff

- **GIVEN** a ModuleInstance with `failureCounters.reconcile=0` and a generated platform
- **WHEN** fetching its module from the registry fails because the registry cannot be reached
- **THEN** the ModuleInstance reports `Ready=False` with reason `ResolutionFailed`, no `Stalled` condition, a Warning event carrying the acquisition error, and the controller returns `RequeueAfter: 5s`

#### Scenario: Registry outage while loading a ModulePackage retries on the backoff

- **GIVEN** a ModulePackage whose artifact fetched and whose path resolved
- **WHEN** loading the package fails because a CUE dependency in its `cue.mod/module.cue` cannot be fetched from the registry
- **THEN** the ModulePackage reports `Ready=False` with reason `ResolutionFailed`, no `Stalled` condition, and requeues on the exponential backoff rather than the 30-minute recheck

#### Scenario: Registry failure during synthesis or render retries on the backoff

- **GIVEN** a ModuleInstance whose module was acquired
- **WHEN** instance synthesis or the render build fails with a typed registry fetch failure
- **THEN** the ModuleInstance reports `Ready=False` with reason `ResolutionFailed`, no `Stalled` condition, and requeues on the exponential backoff

#### Scenario: Not-found and refused credentials retry on the backoff

- **WHEN** a fetch fails because the registry does not hold the module or version, or refuses the credentials
- **THEN** the object reports `Ready=False` with reason `ResolutionFailed`, no `Stalled` condition, and requeues on the exponential backoff capped at 5 minutes

#### Scenario: Repeated fetch failures cap at five minutes

- **GIVEN** an object with `failureCounters.reconcile=10` whose fetch keeps failing
- **WHEN** the next reconcile fails the same way
- **THEN** the controller requeues after 5 minutes, not 30

#### Scenario: Recovery once the registry answers

- **GIVEN** an object reporting a transient `ResolutionFailed` after a registry fetch failure
- **WHEN** the next retry acquires the module and renders it
- **THEN** the object reports `Ready=True`, `status.nextRetryAt` is nil and `failureCounters.reconcile` resets

#### Scenario: Typed terminal causes stay stalled

- **WHEN** a render fails with an identity mismatch, a wrong artifact kind, a structurally invalid package or a missing required field, even when a registry fetch failure is joined to it
- **THEN** the object reports `Ready=False` and `Stalled=True` and requeues on the 30-minute recheck; the reason is `ResolutionFailed`, except a ModulePackage of the wrong kind, which keeps `UnsupportedKind`

#### Scenario: Author defect in a package stalls

- **GIVEN** a ModulePackage whose package has a CUE syntax error, values that conflict with `#config`, or non-concrete values
- **WHEN** loading the package fails
- **THEN** the ModulePackage reports `Ready=False` with reason `ResolutionFailed` and `Stalled=True`, and requeues on the 30-minute recheck, not the backoff

#### Scenario: Unclassified acquisition failure stalls

- **WHEN** a ModuleInstance's module acquisition fails with an error that is not a typed registry fetch failure, for example a version that does not parse
- **THEN** the ModuleInstance reports `Ready=False` with reason `ResolutionFailed` and `Stalled=True`, and requeues on the 30-minute recheck

#### Scenario: Message text does not classify

- **WHEN** a render fails with an untyped error whose message contains `loading package`, `resolving`, `fetching`, `connection refused` or `not found`
- **THEN** the failure is classified by its typed cause alone: with none, the object reports `Stalled=True` on the 30-minute recheck

### Requirement: A render timeout retries on the backoff

When a ModuleInstance or ModulePackage render does not finish within `--render-timeout`, or a reconcile finds that an earlier timed-out render of the same object is still running, the reconciler SHALL classify the attempt as a timeout before any other render classification, whatever error the render returned. A timeout SHALL set `Ready=False` with reason `RenderTimedOut` and `Reconciling=True`, SHALL NOT set `Stalled=True`, SHALL emit a Warning event with reason `RenderTimedOut` carrying the message, and SHALL produce the outcome `FailedTransient`, so the object requeues on the exponential backoff capped at 5 minutes and `status.nextRetryAt`, `failureCounters.reconcile` and the history follow the transient path. The message SHALL name the timeout and say that the render's slot stays held until the render returns, or, for a render still running, that no new render was started. Inventory and the last-applied digests SHALL keep the last success, and nothing SHALL be applied or pruned.

A render error that carries `context.DeadlineExceeded` from anything other than the render deadline, for example a registry client's own timeout in a render that returned in time, SHALL keep the classification of "Registry fetch failures are transient wherever they occur".

#### Scenario: A ModuleInstance render times out

- **GIVEN** a ModuleInstance with `failureCounters.reconcile=0` and a renderer that blocks until its context is done
- **WHEN** the render deadline passes
- **THEN** the ModuleInstance reports `Ready=False` with reason `RenderTimedOut`, no `Stalled` condition, a Warning event with reason `RenderTimedOut`, `failureCounters.reconcile=1`, a failure history entry, `status.nextRetryAt` set, and the controller returns `RequeueAfter: 5s`

#### Scenario: A ModulePackage render times out

- **GIVEN** a ModulePackage whose artifact fetched and whose renderer blocks until its context is done
- **WHEN** the render deadline passes
- **THEN** the ModulePackage reports `Ready=False` with reason `RenderTimedOut`, no `Stalled` condition, and requeues on the exponential backoff rather than the 30-minute recheck

#### Scenario: Repeated timeouts cap at five minutes

- **GIVEN** an object with `failureCounters.reconcile=10` whose render keeps timing out
- **WHEN** the next render times out the same way
- **THEN** the controller requeues after 5 minutes, not 30

#### Scenario: A render that fails at its deadline is still a timeout

- **WHEN** a render returns a library stage error that wraps the expired render context, or returns a success, after its deadline has passed
- **THEN** the object reports `RenderTimedOut`, not `ResolutionFailed` or `RenderFailed`, and the result is not applied

#### Scenario: Recovery after a timeout

- **GIVEN** an object reporting `RenderTimedOut`
- **WHEN** a later render finishes within the timeout
- **THEN** the object reports `Ready=True`, `status.nextRetryAt` is nil and `failureCounters.reconcile` resets
