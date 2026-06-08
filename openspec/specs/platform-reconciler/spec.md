## Purpose

Reconcile the cluster-scoped singleton `Platform` resource by synthesizing and
materializing it through the library kernel, holding the result in a
process-local single-slot store for concurrent read by render paths, and
surfacing the materialize outcome on the `Platform` status.

## Requirements

### Requirement: Materialize the singleton Platform on reconcile

The operator SHALL reconcile the `Platform` named `cluster` by mapping its spec to `synth.PlatformInput`, calling `Kernel.SynthesizePlatform` then `Kernel.Materialize`, and holding the resulting `*MaterializedPlatform` in a process-local store. The `PlatformSpec.Type` SHALL map to `PlatformInput.Type` and each `PlatformSpec.Registry` entry SHALL map to a `PlatformInput.Subscriptions` entry (`Enable`, `Filter{Range, Allow,Deny}`) under the same module-path key. The reconciler SHALL reconcile only the object named `cluster`; any other name SHALL be ignored without error.

#### Scenario: Valid platform materializes

- **WHEN** a `Platform` named `cluster` with a resolvable `registry` subscription is applied
- **THEN** the reconciler synthesizes and materializes it
- **AND** the materialized platform is held in the store keyed on the CR's `metadata.generation`

#### Scenario: Non-cluster object ignored

- **WHEN** the reconciler is triggered for an object whose name is not `cluster`
- **THEN** it performs no materialize and returns without error

### Requirement: Surface materialize outcome on status

The reconciler SHALL record the outcome on the `Platform` status. On success it SHALL set `Ready=True` with reason `Materialized` and set `status.observedGeneration` to the reconciled generation. On a `*oerrors.MaterializeError` it SHALL set `Ready=False` with reason `MaterializeFailed` and a message naming the error's `Kind`, `Subscription`, and `Version`. A materialize failure SHALL NOT overwrite a previously stored good materialized platform.

#### Scenario: Success sets Ready and observedGeneration

- **WHEN** materialize succeeds for generation N
- **THEN** `status.conditions` carries `Ready=True` (reason `Materialized`)
- **AND** `status.observedGeneration == N`

#### Scenario: Materialize failure surfaces structured error

- **WHEN** materialize fails (e.g. an unresolvable subscription path)
- **THEN** `status.conditions` carries `Ready=False` (reason `MaterializeFailed`)
- **AND** the condition message identifies the `MaterializeError` kind, subscription path, and version

#### Scenario: Failure preserves last-good materialized platform

- **WHEN** a previously stored good platform exists and a subsequent reconcile fails to materialize
- **THEN** the store still returns the last-good materialized platform
- **AND** the failure is reflected only on status

### Requirement: Single-slot generation-keyed store for concurrent read

The store SHALL hold at most one materialized platform and SHALL be keyed on the Platform CR's `metadata.generation`. The store SHALL be safe for concurrent access: a single writer (the reconciler) and many readers (future render paths) under a read/write lock. Reads SHALL return the held `*MaterializedPlatform` and whether one is present.

#### Scenario: Generation change replaces the slot

- **WHEN** the Platform spec changes (new generation M) and re-materializes successfully
- **THEN** the store returns the platform for generation M
- **AND** the prior generation's platform is no longer held

#### Scenario: Concurrent reads are safe

- **WHEN** multiple goroutines read the store while the reconciler writes a new slot
- **THEN** reads return a consistent held platform or absence without data races

### Requirement: Clear the store on Platform deletion

When the `Platform` named `cluster` is deleted, the reconciler SHALL clear the store slot so no materialized platform is held. Deleting the Platform SHALL NOT itself delete or modify any workload resources (freeze-don't-teardown; release behavior under a missing platform is defined in a later slice).

#### Scenario: Delete clears the slot

- **WHEN** the `Platform` `cluster` is deleted
- **THEN** the store reports no held platform
- **AND** no workload resources are modified as a direct result

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
