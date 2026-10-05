## ADDED Requirements

### Requirement: A render is bounded by a manager timeout that keeps its slot

The manager SHALL accept `--render-timeout` (a duration, default `10m`) and SHALL refuse a negative value at startup. While a ModuleInstance or ModulePackage reconcile holds its render slot, the renderer call (platform lease, acquisition, synthesis, the render build) and the export of the result for apply SHALL run under a context whose deadline is that timeout. The wait for a slot SHALL NOT count against it, and the reconcile's own context, which the status patch, events, apply and prune use, SHALL carry no such deadline. A value of `0` SHALL disable the deadline, and the render then runs on the reconcile's context as before.

When the deadline passes before the render returns, the reconcile SHALL stop waiting for it and record the attempt (see reconcile-backoff, "A render timeout retries on the backoff"). The render SHALL keep its slot until it really returns, at the next stage boundary or when the running stage ends, so `--max-concurrent-renders` still bounds the builds held in memory. A render that returns after its deadline SHALL be treated as timed out, and its result SHALL be discarded. A reconcile SHALL NOT read anything the abandoned render writes, and nothing the abandoned render reads SHALL be changed or removed under it: a ModuleInstance render reads a copy of the spec inputs, and a ModulePackage's extracted artifact directory is removed only after both the reconcile and its render are done with it.

A panic in the render before its deadline SHALL reach the reconcile as before, after the slot is free, so it is recorded as `ReconcilePanic`. A panic after the reconcile stopped waiting SHALL be logged at error level with its value and stack, SHALL free the slot, and SHALL NOT stop the process. The operator SHALL log a timed-out render with its timeout, and SHALL log when an abandoned render returns, with how long it ran. The flag's help SHALL state that the slot stays held until the render returns and that `0` disables the deadline.

#### Scenario: Default timeout

- **WHEN** the manager starts without `--render-timeout`
- **THEN** every render runs under a 10-minute deadline counted from when it holds its slot

#### Scenario: Negative timeout is refused

- **WHEN** the manager starts with `--render-timeout=-1s`
- **THEN** it logs the invalid value and exits before starting any controller

#### Scenario: Zero disables the deadline

- **WHEN** the manager starts with `--render-timeout=0`
- **THEN** a render runs on the reconcile's context with no deadline, as it did before the flag existed

#### Scenario: A timed-out render keeps its slot until it returns

- **GIVEN** a pool of one slot and a ModuleInstance whose render blocks past its deadline and keeps running after it
- **WHEN** the deadline passes
- **THEN** the ModuleInstance reports `RenderTimedOut` while the slot is still taken, and a ModulePackage render enqueued next starts only after the blocked render returns

#### Scenario: Queued renders are not timed out

- **GIVEN** a pool of one slot held by a long render
- **WHEN** another reconcile waits for the slot longer than `--render-timeout`
- **THEN** that reconcile is not reported as timed out while it waits, and its own deadline starts when it takes the slot

#### Scenario: A render that finishes in time is unaffected

- **WHEN** a render returns before its deadline
- **THEN** the reconcile reads its result and continues to apply as before

#### Scenario: The status patch outlives the render deadline

- **WHEN** a render times out
- **THEN** the object's status is patched with the failure, because the patch uses the reconcile's context and not the expired render context

#### Scenario: A ModulePackage render past its deadline keeps its files

- **GIVEN** a ModulePackage whose render is still running after its deadline
- **WHEN** the reconcile has returned
- **THEN** the extracted artifact directory still exists, and it is removed once the render returns

#### Scenario: A panic after the deadline does not stop the operator

- **WHEN** an abandoned render panics after its reconcile has returned
- **THEN** the operator logs the panic with its stack, the slot is free again, and the process keeps running
