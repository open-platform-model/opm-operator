## ADDED Requirements

### Requirement: Materialize failures requeue on a bounded interval

When `SynthesizePlatform` or `Materialize` fails, the `PlatformReconciler` SHALL requeue the `Platform` after a bounded interval rather than waiting for a spec change. Because materialize resolves against a mutable external registry, no materialize failure SHALL be treated as permanently terminal — periodic retry SHALL continue until materialize succeeds or the `Platform` is deleted. The reconciler SHALL still set `Ready=False` with reason `MaterializeFailed` and SHALL still preserve any previously stored good materialized platform (the store slot SHALL NOT be cleared on failure).

#### Scenario: Failure requeues instead of stalling indefinitely

- **WHEN** materialize fails for the `cluster` Platform
- **THEN** the reconcile result carries a non-zero `RequeueAfter`
- **AND** the status is `Ready=False` with reason `MaterializeFailed`
- **AND** the previously stored materialized platform, if any, is still held

#### Scenario: Recovery without a spec change

- **WHEN** a Platform is in `MaterializeFailed` and the underlying registry condition clears (e.g., the registry becomes reachable, or a version matching a subscription is published) with no change to the Platform spec
- **THEN** a subsequent automatic reconcile materializes successfully and sets `Ready=True`

### Requirement: Transient failures retry faster than semantic failures

The reconciler SHALL requeue clearly-transient failures (network/timeout causes, detected best-effort via the wrapped cause of a `MaterializeError`) on a short interval, and SHALL requeue all other failures — semantic causes and any cause it cannot classify — on the long stalled-recheck interval. Misclassification SHALL be safe: an unrecognized cause SHALL default to the long interval.

#### Scenario: Transient cause retries quickly

- **WHEN** materialize fails with a transient cause (e.g., the registry is unreachable / times out)
- **THEN** the reconcile requeues on the short interval

#### Scenario: Semantic or unknown cause retries slowly

- **WHEN** materialize fails with a semantic cause (e.g., a subscription path that cannot be resolved) or a cause that cannot be classified
- **THEN** the reconcile requeues on the long stalled-recheck interval

### Requirement: Observed generation is recorded on failure

The reconciler SHALL set `status.observedGeneration` to the reconciled generation on the failure paths as well as on success, so a stalled `Platform` reflects the generation it observed.

#### Scenario: Stalled platform reports its generation

- **WHEN** materialize fails for generation N
- **THEN** `status.observedGeneration == N`
- **AND** the condition is `Ready=False` with reason `MaterializeFailed`

### Requirement: Failure events are emitted on transition, not every recheck

Because failures now requeue periodically, the reconciler SHALL emit the materialize-failure warning event only when the failure state is entered or its message changes, not on every periodic recheck of an unchanged failure.

#### Scenario: No event spam while stuck failing

- **WHEN** a Platform remains in the same `MaterializeFailed` state across multiple periodic rechecks
- **THEN** the warning event is not re-emitted on each recheck
- **AND** a new event is emitted only when the failure is first entered or its message changes
