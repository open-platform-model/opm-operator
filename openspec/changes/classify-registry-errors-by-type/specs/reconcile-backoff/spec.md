## REMOVED Requirements

### Requirement: Acquisition failures without a typed terminal cause are transient
**Reason**: The requirement keys retry on the acquisition phase. With the library's typed fetch failures (library `v1.0.0-beta.6`, 0021:D8:R12) the operator keys retry on the cause: an author defect in a ModulePackage no longer retries, and a registry failure during synthesis or render no longer stalls. The scenario "Message text does not classify" would turn false for a synthesis-phase registry failure, and OpenSpec refuses a MODIFIED that drops a scenario, so the requirement is replaced under a new name.
**Migration**: "Registry fetch failures are transient wherever they occur" below carries the shared classification, the backoff, the cap, the recovery, the typed terminal causes and the no-message-text rule, and adds the synthesis and render phases and the unclassified-failure stall.

## ADDED Requirements

### Requirement: Registry fetch failures are transient wherever they occur

When a render fails, the ModuleInstance and ModulePackage reconcilers SHALL classify the failure by its type, never by its message text, with one shared classification so the two cannot drift. A failure is transient when its error chain holds the library's typed registry fetch failure (`*oerrors.FetchError`, of any `Kind`: unreachable, not found, unauthorized or other) or `context.DeadlineExceeded`, and holds none of the typed terminal causes (an identity mismatch, a wrong artifact kind, a structurally invalid package, a missing required field, unresolved platform demands, unmatched components). This holds for every kernel call the render makes: module acquisition, values compile, instance synthesis, the render build and the package load.

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
