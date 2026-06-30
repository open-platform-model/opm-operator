## ADDED Requirements

### Requirement: ModuleInstance carries an ownership marker

`ModuleInstanceSpec` SHALL provide an optional `owner` field of a typed enum with exactly two valid values, `cli` and `operator`, serialized as `owner` with `omitempty`. The field SHALL NOT define a CRD-level default. The API SHALL define exported constants for both values. An absent or empty `owner` SHALL be treated by the controller as operator-managed (see the skip requirement).

#### Scenario: Field accepts the two enum values

- **WHEN** a `ModuleInstance` is created with `spec.owner` set to `cli` or to `operator`
- **THEN** the API server accepts it
- **AND** a value other than `cli` or `operator` is rejected by enum validation

#### Scenario: Field is optional with no default

- **WHEN** a `ModuleInstance` is created with no `spec.owner`
- **THEN** the API server accepts it
- **AND** the stored object's `spec.owner` remains empty (no value is defaulted in)

### Requirement: The operator skips CLI-owned instances before registering a finalizer

When `spec.owner == cli`, the controller MUST return from `Reconcile` without rendering, applying, pruning, or running deletion cleanup, and MUST NOT add the `opmodel.dev/cleanup` finalizer. The owner check MUST occur before finalizer registration so that a CLI-owned instance never carries the operator's finalizer.

#### Scenario: CLI-owned instance is not reconciled and gets no finalizer

- **GIVEN** a `ModuleInstance` with `spec.owner == cli` and no `DeletionTimestamp`
- **WHEN** the controller reconciles it
- **THEN** no resources are rendered or applied
- **AND** the `opmodel.dev/cleanup` finalizer is NOT present in `metadata.finalizers`
- **AND** the controller returns without requeueing for work

#### Scenario: Deleting a CLI-owned instance is a no-op for the operator

- **GIVEN** a `ModuleInstance` with `spec.owner == cli` and a non-zero `DeletionTimestamp`
- **WHEN** the controller reconciles it
- **THEN** the controller prunes no resources
- **AND** the controller does not block deletion (no finalizer was ever added)

#### Scenario: Operator-managed instances are unaffected

- **GIVEN** a `ModuleInstance` with `spec.owner` absent, empty, or `operator`
- **WHEN** the controller reconciles it
- **THEN** the controller proceeds with the normal reconcile (finalizer registration, render, apply, prune, status) exactly as before this change

### Requirement: The operator acknowledges a CLI-owned instance with a single condition

On a non-deleting `ModuleInstance` with `spec.owner == cli`, the controller MUST set `Ready: Unknown` with reason `ManagedExternally` and MUST NOT write any other status field — specifically it MUST NOT set `status.observedGeneration`, and MUST NOT modify `status.inventory`, the `lastApplied*` digests, `instanceUUID`, or any field the CLI writes. The `internal/status` package MUST expose a `ManagedExternally` reason constant and a helper that sets this condition (clearing `Reconciling` and `Stalled`). The acknowledgement MUST be idempotent: reconciling an already-acknowledged instance MUST produce no status change.

#### Scenario: ManagedExternally condition is set

- **GIVEN** a `ModuleInstance` with `spec.owner == cli`
- **WHEN** the controller reconciles it
- **THEN** the `Ready` condition is `Unknown` with reason `ManagedExternally`
- **AND** the `Reconciling` and `Stalled` conditions are absent

#### Scenario: No observedGeneration and no CLI-written status is touched

- **GIVEN** a `ModuleInstance` with `spec.owner == cli` carrying CLI-written `status.inventory` and `lastApplied*` digests
- **WHEN** the controller reconciles it
- **THEN** `status.observedGeneration` is not set by the controller
- **AND** `status.inventory`, the `lastApplied*` digests, and `instanceUUID` are unchanged

#### Scenario: Re-acknowledgement is a no-op

- **GIVEN** a `ModuleInstance` with `spec.owner == cli` already carrying `Ready: Unknown / ManagedExternally`
- **WHEN** the controller reconciles it again (e.g. after a Platform change re-enqueue)
- **THEN** the resulting status patch is empty and no condition transition timestamp changes

### Requirement: Ownership handoff falls through to a normal reconcile

When a `ModuleInstance`'s `spec.owner` changes from `cli` to `operator`, the next reconcile MUST proceed through the normal path: register the finalizer, render, apply, prune, and write the real `Ready` status, overwriting the `ManagedExternally` condition.

#### Scenario: Flip to operator adopts the instance

- **GIVEN** a `ModuleInstance` previously reconciled with `spec.owner == cli` (carrying `ManagedExternally`, no finalizer)
- **WHEN** `spec.owner` is set to `operator` and the controller reconciles it
- **THEN** the `opmodel.dev/cleanup` finalizer is added
- **AND** the instance is rendered and applied
- **AND** the `Ready` condition reflects the real reconcile outcome (no longer `ManagedExternally`)
