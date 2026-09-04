## Purpose

Reconcile the cluster-scoped singleton `Platform` resource by synthesizing and
materializing it through the library kernel, holding the result in a
process-local single-slot store for concurrent read by render paths, and
surfacing the materialize outcome on the `Platform` status.

## Requirements

### Requirement: Materialize the singleton Platform on reconcile

The operator SHALL reconcile the `Platform` named `cluster` by generating its platform CUE module on the operator's own disk, building it through the kernel's shape-gated platform loader, and recording the generated module (generation, directory, built platform) together with the resolved skew policy (`spec.skewPolicy`, `Warn` when unset) in the process-local store. The reconciler SHALL reconcile only the object named `cluster`; any other name SHALL be ignored without error. No materialized twin exists.

#### Scenario: Valid platform materializes

- **WHEN** a `Platform` named `cluster` with resolvable pins is applied
- **THEN** the reconciler generates and builds its module
- **AND** the store's record carries the CR's `metadata.generation`, the module directory and the skew policy

#### Scenario: Non-cluster object ignored

- **WHEN** the reconciler is triggered for an object whose name is not `cluster`
- **THEN** it performs no generation and returns without error

### Requirement: Surface materialize outcome on status

The reconciler SHALL record the outcome on the `Platform` status: `Ready=True` with reason `Generated` on success, `Ready=False` with reason `BuildFailed` (a dependency did not resolve or the module did not build; the message names the dependency or the registry entry) or `GenerateFailed` (the module could not be written). `status.observedGeneration` SHALL be set on every outcome. A failure SHALL NOT overwrite a previously recorded good module.

#### Scenario: Success sets Ready and observedGeneration

- **WHEN** generation and build succeed for generation N
- **THEN** `status.conditions` carries `Ready=True` (reason `Generated`) and `status.observedGeneration == N`

#### Scenario: Materialize failure surfaces structured error

- **WHEN** a pinned build does not exist or an entry's key disagrees with its imported catalog
- **THEN** `status.conditions` carries `Ready=False` (reason `BuildFailed`) with a message naming the path and version or the registry entry

#### Scenario: Failure preserves last-good materialized platform

- **WHEN** a previously recorded good module exists and a subsequent reconcile fails
- **THEN** the store still returns the last-good record and the failure is reflected only on status

### Requirement: Reconciler stamps status.operatorVersion on every outcome

`PlatformReconciler` SHALL set `status.operatorVersion` to the running operator's version on every status patch it makes, on `Generated`, `BuildFailed` and `GenerateFailed` alike.

#### Scenario: Stamped on success and failure

- **WHEN** the singleton Platform reconciles to any outcome
- **THEN** `status.operatorVersion` equals the running operator's version string

### Requirement: Single-slot generation-keyed store for concurrent read

The store SHALL hold at most one generated-module record, keyed on the Platform CR's `metadata.generation`, safe for a single writer and many readers. Readers SHALL obtain the record through a lease that they release when their render completes; the store SHALL report which generations are leased. Superseded module directories SHALL be removed only when no lease holds their generation.

#### Scenario: Generation change replaces the slot

- **WHEN** the Platform spec changes (new generation M) and builds successfully
- **THEN** the store returns the record for generation M and the prior record is no longer current

#### Scenario: A leased generation survives the swap

- **WHEN** a render holds a lease on generation N while generation N+1 is recorded
- **THEN** generation N's directory is not removed until the lease is released, and is removed by a later reconcile once unleased

#### Scenario: Concurrent reads are safe

- **WHEN** multiple goroutines lease and release the record while the reconciler records a new generation
- **THEN** reads return a consistent record or absence without data races, and lease counts stay exact

### Requirement: Clear the store on Platform deletion

When the `Platform` named `cluster` is deleted, the reconciler SHALL clear the store slot so no materialized platform is held. Deleting the Platform SHALL NOT itself delete or modify any workload resources (freeze-don't-teardown; release behavior under a missing platform is defined in a later slice).

#### Scenario: Delete clears the slot

- **WHEN** the `Platform` `cluster` is deleted
- **THEN** the store reports no held platform
- **AND** no workload resources are modified as a direct result

### Requirement: Materialize failures requeue on a bounded interval

When closure derivation, generation or the build fails, the `PlatformReconciler` SHALL requeue the `Platform` after a bounded interval rather than waiting for a spec change; no such failure is terminal. The reconciler SHALL set the failure reason (`BuildFailed` or `GenerateFailed`) and SHALL preserve any previously recorded good module.

#### Scenario: Failure requeues instead of stalling indefinitely

- **WHEN** the build fails for the `cluster` Platform
- **THEN** the reconcile result carries a non-zero `RequeueAfter`, the status is `Ready=False`, and the last-good record, if any, is still held

#### Scenario: Recovery without a spec change

- **WHEN** a Platform is in `BuildFailed` and the registry condition clears with no change to the spec
- **THEN** a subsequent automatic reconcile builds successfully and sets `Ready=True` (reason `Generated`)

### Requirement: Transient failures retry faster than semantic failures

The reconciler SHALL requeue clearly-transient failures (network/timeout causes, detected best-effort from the wrapped cause) on a short interval and every other failure on the long stalled-recheck interval; an unrecognized cause SHALL default to the long interval.

#### Scenario: Transient cause retries quickly

- **WHEN** the build fails because the registry is unreachable or times out
- **THEN** the reconcile requeues on the short interval

#### Scenario: Semantic or unknown cause retries slowly

- **WHEN** the build fails on a pin that does not exist or an unclassifiable cause
- **THEN** the reconcile requeues on the long stalled-recheck interval

### Requirement: Observed generation is recorded on failure

The reconciler SHALL set `status.observedGeneration` to the reconciled generation on the failure paths as well as on success.

#### Scenario: Stalled platform reports its generation

- **WHEN** the build fails for generation N
- **THEN** `status.observedGeneration == N` and the condition is `Ready=False` with reason `BuildFailed` or `GenerateFailed`

### Requirement: Failure events are emitted on transition, not every recheck

The reconciler SHALL emit the failure warning event only when the failure state is entered or its reason or message changes, not on every periodic recheck.

#### Scenario: No event spam while stuck failing

- **WHEN** a Platform remains in the same failed state across multiple rechecks
- **THEN** the warning event is not re-emitted on each recheck

### Requirement: Subscription mapping and materialize failures

Each subscription's `version` SHALL be used verbatim as the generated module's pin and as the entry's stamped expected version. A subscription with an empty version (a stored object predating the required field) SHALL surface as `BuildFailed` naming the path before any registry I/O. A version absent from the registry SHALL surface as `BuildFailed` naming the path and version. A pin whose bytes disagree with the stamp SHALL surface as `BuildFailed` naming the registry entry. All retain the stalled recheck interval.

#### Scenario: Missing version surfaces as MaterializeFailed

- **WHEN** the stored Platform carries a subscription without a version
- **THEN** the Platform's Ready condition is False with reason `BuildFailed` and a message naming the subscription path

#### Scenario: Named build not published

- **WHEN** a subscription names a version the registry does not have
- **THEN** `BuildFailed` names the path and version

#### Scenario: Identity mismatch surfaces with both values

- **WHEN** a pinned catalog's bytes disagree with the stamped expected version or the entry's key disagrees with the catalog's declared module path
- **THEN** `BuildFailed` carries the conflict naming the registry entry and both values

### Requirement: The skew policy is recorded with the generated module

The reconciler SHALL resolve `spec.skewPolicy` (`Warn` when unset) and record it beside the generated module so every render of that generation applies the same policy. A change to the policy alone SHALL regenerate (the generation bumps) and SHALL re-enqueue the workloads through the existing Platform watch.

#### Scenario: Policy edit re-enqueues workloads

- **WHEN** `spec.skewPolicy` changes from `Warn` to `Refuse` with no other edit
- **THEN** the Platform reconciles to a new generation with reason `Generated`, the store record carries `Refuse`, and blocked or rendered workloads are re-enqueued
