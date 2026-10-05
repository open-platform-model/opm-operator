## ADDED Requirements

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
