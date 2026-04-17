## ADDED Requirements

### Requirement: Failure counters initialized
The controller MUST initialize `status.failureCounters` if nil on first status update.

#### Scenario: First reconcile initializes counters
- **GIVEN** a ModuleRelease with `status.failureCounters` nil
- **WHEN** the controller commits status in Phase 7
- **THEN** `status.failureCounters` is initialized with all fields at 0

### Requirement: Reconcile counter incremented on failure
The `reconcile` counter MUST be incremented when any phase fails.

#### Scenario: Transient failure increments reconcile counter
- **GIVEN** a ModuleRelease with `status.failureCounters.reconcile=2`
- **WHEN** Phase 2 (artifact fetch) fails with a transient error
- **THEN** `status.failureCounters.reconcile` becomes 3

#### Scenario: Stalled failure increments reconcile counter
- **GIVEN** a ModuleRelease with `status.failureCounters.reconcile=0`
- **WHEN** Phase 3 (render) fails with a stalled error
- **THEN** `status.failureCounters.reconcile` becomes 1

### Requirement: Reconcile counter reset on success
The `reconcile` counter MUST be reset to 0 on successful reconcile.

#### Scenario: Success resets reconcile counter
- **GIVEN** a ModuleRelease with `status.failureCounters.reconcile=5`
- **WHEN** the reconcile completes with outcome `Applied`
- **THEN** `status.failureCounters.reconcile` becomes 0

### Requirement: Apply counter tracks Phase 5
The `apply` counter MUST be incremented when Phase 5 fails and reset when it succeeds.

#### Scenario: Apply failure increments apply counter
- **GIVEN** a ModuleRelease with `status.failureCounters.apply=1`
- **WHEN** Phase 5 (Apply) fails
- **THEN** `status.failureCounters.apply` becomes 2

#### Scenario: Apply success resets apply counter
- **GIVEN** a ModuleRelease with `status.failureCounters.apply=3`
- **WHEN** Phase 5 (Apply) succeeds
- **THEN** `status.failureCounters.apply` becomes 0

### Requirement: Prune counter tracks Phase 6
The `prune` counter MUST be incremented when Phase 6 fails and reset when it succeeds or is skipped.

#### Scenario: Prune failure increments prune counter
- **GIVEN** a ModuleRelease with `status.failureCounters.prune=0` and `spec.prune=true`
- **WHEN** Phase 6 (Prune) fails
- **THEN** `status.failureCounters.prune` becomes 1

### Requirement: Drift counter tracks detection failures
The `drift` counter MUST be incremented when the drift detection API call fails and reset when it succeeds.

#### Scenario: Drift detection failure increments drift counter
- **GIVEN** a ModuleRelease with `status.failureCounters.drift=0`
- **WHEN** Phase 4 drift detection dry-run fails
- **THEN** `status.failureCounters.drift` becomes 1

### Requirement: Per-counter reset independence
Counter resets MUST be independent. A successful apply MUST NOT reset the prune counter.

#### Scenario: Independent counter reset
- **GIVEN** `status.failureCounters` with `apply=3, prune=2`
- **WHEN** Phase 5 (Apply) succeeds but Phase 6 (Prune) fails
- **THEN** `apply` becomes 0 and `prune` becomes 3
