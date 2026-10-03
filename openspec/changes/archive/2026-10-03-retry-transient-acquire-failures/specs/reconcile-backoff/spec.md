## ADDED Requirements

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
