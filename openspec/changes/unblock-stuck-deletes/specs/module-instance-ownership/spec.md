## MODIFIED Requirements

### Requirement: The operator skips CLI-owned instances before registering a finalizer

When `spec.owner == cli`, the controller MUST return from `Reconcile` without rendering, applying, pruning, or running deletion cleanup, and MUST NOT add the `opmodel.dev/cleanup` finalizer. The owner check MUST occur before finalizer registration so that an instance that has always been CLI-owned never carries the operator's finalizer.

An instance that was operator-owned before its `spec.owner` became `cli` still carries the finalizer. When such an instance is being deleted, the controller MUST remove the `opmodel.dev/cleanup` finalizer, MUST NOT delete any object in the instance's inventory whatever its `spec.prune`, and MUST write no status. On a CLI-owned instance that is not being deleted the controller MUST NOT change the finalizers.

Rationale: only the operator removes its finalizer, and the owner-skip gate is the only code that runs for a CLI-owned instance. Without the release the delete never completes.

#### Scenario: CLI-owned instance is not reconciled and gets no finalizer

- **GIVEN** a `ModuleInstance` with `spec.owner == cli` and no `DeletionTimestamp`
- **WHEN** the controller reconciles it
- **THEN** no resources are rendered or applied
- **AND** the `opmodel.dev/cleanup` finalizer is NOT present in `metadata.finalizers`
- **AND** the controller returns without requeueing for work

#### Scenario: Deleting a CLI-owned instance is a no-op for the operator

- **GIVEN** a `ModuleInstance` with `spec.owner == cli`, no `opmodel.dev/cleanup` finalizer and a non-zero `DeletionTimestamp`
- **WHEN** the controller reconciles it
- **THEN** the controller prunes no resources
- **AND** the controller does not block deletion (no finalizer was ever added)

#### Scenario: Deleting a CLI-owned instance that carries the finalizer releases it

- **GIVEN** a `ModuleInstance` with `spec.owner == cli`, `spec.prune: true`, a non-empty `status.inventory`, the `opmodel.dev/cleanup` finalizer and a non-zero `DeletionTimestamp`
- **WHEN** the controller reconciles it
- **THEN** the finalizer is removed and the apiserver deletes the instance
- **AND** every object in the inventory still exists
- **AND** the controller returns without requeueing

#### Scenario: A live CLI-owned instance keeps a leftover finalizer

- **GIVEN** a `ModuleInstance` with `spec.owner == cli`, the `opmodel.dev/cleanup` finalizer and no `DeletionTimestamp`
- **WHEN** the controller reconciles it
- **THEN** the finalizer stays and the instance is acknowledged with `ManagedExternally`

#### Scenario: Operator-managed instances are unaffected

- **GIVEN** a `ModuleInstance` with `spec.owner` absent, empty, or `operator` that is not the operator's own instance
- **WHEN** the controller reconciles it
- **THEN** the controller proceeds with the normal reconcile (finalizer registration, render, apply, prune, status) exactly as before this change

### Requirement: The operator releases its finalizer from its own instance without pruning

When the operator's own instance with `spec.owner` absent or `operator` carries the `opmodel.dev/cleanup` finalizer, which an earlier operator release may have added, the controller MUST remove that finalizer and MUST NOT delete any object in the instance's inventory, whether or not the instance is being deleted, whatever its `spec.prune`. On an own instance being deleted, the controller MUST write no status. An own instance with `spec.owner: cli` is left to the owner-skip gate, which changes no finalizer on a live instance and releases the finalizer, without pruning, from one that is being deleted.

Rationale: a finalizer only a running operator can clear would block the uninstall and the reinstall of the operator, which is the operator's one recovery path.

#### Scenario: Deleting an own instance with a leftover finalizer prunes nothing

- **GIVEN** the operator's own instance with `spec.owner: operator`, `spec.prune: true`, a non-empty `status.inventory`, the `opmodel.dev/cleanup` finalizer and a non-zero `DeletionTimestamp`
- **WHEN** the controller reconciles it
- **THEN** the finalizer is removed
- **AND** every object in the inventory still exists

#### Scenario: A live own instance loses a leftover finalizer

- **GIVEN** the operator's own instance with `spec.owner: operator` and the `opmodel.dev/cleanup` finalizer, not being deleted
- **WHEN** the controller reconciles it
- **THEN** the finalizer is removed and the instance is refused with reason `SelfManagementRefused`

#### Scenario: A CLI-owned own instance is left alone

- **GIVEN** the operator's own instance with `spec.owner: cli` and the `opmodel.dev/cleanup` finalizer, not being deleted
- **WHEN** the controller reconciles it
- **THEN** the finalizer stays, nothing is pruned, and the instance is acknowledged with `ManagedExternally`
