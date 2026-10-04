## ADDED Requirements

### Requirement: The operator recognises the instance that deploys it

The controller SHALL treat a `ModuleInstance` as the operator's own instance when any one of these holds, each read from the object as stored, without rendering the module:

- its `metadata.name` is `opm-operator` and its `metadata.namespace` is `opm-operator-system`;
- its `spec.module.path`, with any `@<major>` suffix removed, is `opmodel.dev/modules/opm_operator`;
- its `status.inventory.entries` holds an entry of group `apiextensions.k8s.io` and kind `CustomResourceDefinition` whose name, after its first `.`, is the operator's own API group `opmodel.dev`.

The coordinates are the ones every CLI install records the operator's instance under; the module path is the one the operator module is published under, in any major. A refusal must be decided before the finalizer is registered, which is why every signal reads stored fields only.

#### Scenario: Fixed coordinates identify the instance

- **GIVEN** a `ModuleInstance` named `opm-operator` in namespace `opm-operator-system` whose module path is any value
- **WHEN** the controller reconciles it
- **THEN** the controller treats it as the operator's own instance

#### Scenario: The operator module identifies the instance in any namespace and any major

- **GIVEN** a `ModuleInstance` named `team-ops` in namespace `platform` whose `spec.module.path` is `opmodel.dev/modules/opm_operator@v0`, `opmodel.dev/modules/opm_operator@v1` or `opmodel.dev/modules/opm_operator`
- **WHEN** the controller reconciles it
- **THEN** the controller treats it as the operator's own instance

#### Scenario: A recorded operator CRD identifies the instance

- **GIVEN** a `ModuleInstance` with another name, namespace and module path whose `status.inventory.entries` holds `CustomResourceDefinition` `moduleinstances.opmodel.dev`
- **WHEN** the controller reconciles it
- **THEN** the controller treats it as the operator's own instance

#### Scenario: Similar names are not the operator's own instance

- **GIVEN** a `ModuleInstance` named `opm-operator` in namespace `default`, whose module path is `opmodel.dev/modules/opm_operator_dashboard@v0`, and whose inventory holds only the `CustomResourceDefinition`s `widgets.example.opmodel.dev` and `widgets.example.opmodel.dev.io`
- **WHEN** the controller reconciles it
- **THEN** the controller does not treat it as the operator's own instance and reconciles it as its `spec.owner` says

### Requirement: The operator never reconciles its own instance

On the operator's own instance, whatever `spec.owner` says, the controller MUST never apply, never prune and never add the `opmodel.dev/cleanup` finalizer. An own instance with `spec.owner: cli` is handled by the owner-skip gate and acknowledged with `ManagedExternally`; an own instance with `spec.owner` absent or `operator` MUST be refused before finalizer registration, before the suspend check, and before any render. On refusal the controller MUST:

- set `Ready=False` and `Stalled=True`, both with reason `SelfManagementRefused`, and remove `Reconciling`, `ModuleResolved` and `Drifted`, with a message that names the signal that matched and says to set `spec.owner` to `cli`;
- set `status.observedGeneration` to the instance's generation, so a client waiting on that generation reads a final verdict instead of timing out;
- emit a Warning event with reason `SelfManagementRefused` when the instance was not already refused;
- leave `status.inventory`, the `lastApplied*` digests, `instanceUUID`, `lastAttempted*`, history and failure counters unchanged;
- not requeue: the refusal is re-evaluated when the instance changes.

The `internal/status` package MUST expose a `SelfManagementRefused` reason constant. Re-refusing an already-refused instance at the same generation MUST produce an empty status patch.

Rationale: the operator is the one workload whose failure stops every other instance, so re-running install must be able to repair or replace it without any step taken by the running operator. Nothing the operator does to its own instance may stand between a re-run install and the cluster.

#### Scenario: An own instance flipped to operator is refused without a finalizer

- **GIVEN** the operator's own instance with `spec.owner: operator`, `spec.prune: true` and no `opmodel.dev/cleanup` finalizer
- **WHEN** the controller reconciles it
- **THEN** the finalizer is not added
- **AND** nothing is rendered, applied or pruned
- **AND** `Ready` is `False` and `Stalled` is `True`, both with reason `SelfManagementRefused`
- **AND** `status.observedGeneration` equals `metadata.generation`

#### Scenario: A suspended own instance is refused, not suspended

- **GIVEN** the operator's own instance with `spec.owner: operator` and `spec.suspend: true`
- **WHEN** the controller reconciles it
- **THEN** `Ready` is `False` with reason `SelfManagementRefused`, not `Suspended`
- **AND** the `opmodel.dev/cleanup` finalizer is not added

#### Scenario: Refusal clears conditions left by an earlier adoption

- **GIVEN** the operator's own instance with `spec.owner: operator` carrying `ModuleResolved=True` and `Drifted=True` from an earlier reconcile
- **WHEN** the controller refuses it
- **THEN** `ModuleResolved` and `Drifted` are removed

#### Scenario: An own instance with no owner is refused

- **GIVEN** the operator's own instance with no `spec.owner`
- **WHEN** the controller reconciles it
- **THEN** the controller refuses it exactly as an own instance with `spec.owner: operator`

#### Scenario: Refusal leaves CLI-written status alone

- **GIVEN** the operator's own instance flipped to `spec.owner: operator`, carrying a CLI-written `status.inventory`, `lastApplied*` digests and `instanceUUID`
- **WHEN** the controller refuses it
- **THEN** those fields are unchanged

#### Scenario: Re-refusal is a no-op

- **GIVEN** the operator's own instance already refused at its current generation
- **WHEN** the controller reconciles it again
- **THEN** the status patch is empty, no condition transition time changes, and no event is emitted

#### Scenario: Handing the instance back to the CLI clears the refusal

- **GIVEN** a refused own instance
- **WHEN** its `spec.owner` is set to `cli`
- **THEN** the next reconcile acknowledges it with `Ready=Unknown` reason `ManagedExternally` and removes `Stalled`

### Requirement: The operator releases its finalizer from its own instance without pruning

When the operator's own instance with `spec.owner` absent or `operator` carries the `opmodel.dev/cleanup` finalizer, which an earlier operator release may have added, the controller MUST remove that finalizer and MUST NOT delete any object in the instance's inventory, whether or not the instance is being deleted, whatever its `spec.prune`. On an own instance being deleted, the controller MUST write no status. An own instance with `spec.owner: cli` is left to the owner-skip gate, which changes no finalizer.

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

- **GIVEN** the operator's own instance with `spec.owner: cli` and the `opmodel.dev/cleanup` finalizer
- **WHEN** the controller reconciles it
- **THEN** the finalizer stays, nothing is pruned, and the instance is acknowledged with `ManagedExternally`

## MODIFIED Requirements

### Requirement: ModuleInstance carries an ownership marker

`ModuleInstanceSpec` SHALL provide an optional `owner` field of a typed enum with exactly two valid values, `cli` and `operator`, serialized as `owner` with `omitempty`. The field SHALL NOT define a CRD-level default. The API SHALL define exported constants for both values. An absent or empty `owner` SHALL be treated by the controller as operator-managed (see the skip requirement), except on the operator's own instance, which the controller never reconciles (see the requirement that the operator never reconciles its own instance).

#### Scenario: Field accepts the two enum values

- **WHEN** a `ModuleInstance` is created with `spec.owner` set to `cli` or to `operator`
- **THEN** the API server accepts it
- **AND** a value other than `cli` or `operator` is rejected by enum validation

#### Scenario: Field is optional with no default

- **WHEN** a `ModuleInstance` is created with no `spec.owner`
- **THEN** the API server accepts it
- **AND** the stored object's `spec.owner` remains empty (no value is defaulted in)

#### Scenario: Empty owner is reconciled as operator-managed

- **WHEN** a `ModuleInstance` with no `spec.owner` that is not the operator's own instance is reconciled
- **THEN** the controller SHALL register the cleanup finalizer and perform a normal render/apply reconcile (no owner-skip, no `ManagedExternally` acknowledgement)

#### Scenario: Unknown owner values cannot reach the reconciler

- **WHEN** a client attempts to create a `ModuleInstance` with `spec.owner: "future-actor"`
- **THEN** the API server SHALL reject it via enum validation, so the controller never observes an unknown owner value

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

- **GIVEN** a `ModuleInstance` with `spec.owner` absent, empty, or `operator` that is not the operator's own instance
- **WHEN** the controller reconciles it
- **THEN** the controller proceeds with the normal reconcile (finalizer registration, render, apply, prune, status) exactly as before this change

### Requirement: Ownership handoff falls through to a normal reconcile

When a `ModuleInstance`'s `spec.owner` changes from `cli` to `operator`, the next reconcile MUST proceed through the normal path: register the finalizer, render, apply, prune, and write the real `Ready` status, overwriting the `ManagedExternally` condition. The operator's own instance is the exception: flipping its owner to `operator` MUST lead to the refusal of the requirement that the operator never reconciles its own instance, never to an adoption.

#### Scenario: Flip to operator adopts the instance

- **GIVEN** a `ModuleInstance` that is not the operator's own instance, previously reconciled with `spec.owner == cli` (carrying `ManagedExternally`, no finalizer)
- **WHEN** `spec.owner` is set to `operator` and the controller reconciles it
- **THEN** the `opmodel.dev/cleanup` finalizer is added
- **AND** the instance is rendered and applied
- **AND** the `Ready` condition reflects the real reconcile outcome (no longer `ManagedExternally`)

#### Scenario: Flip to operator on the operator's own instance is refused

- **GIVEN** the operator's own instance, previously reconciled with `spec.owner == cli` (carrying `ManagedExternally`, no finalizer)
- **WHEN** `spec.owner` is set to `operator` and the controller reconciles it
- **THEN** no finalizer is added and nothing is rendered, applied or pruned
- **AND** `Ready` is `False` with reason `SelfManagementRefused`
