## MODIFIED Requirements

### Requirement: The operator acknowledges a CLI-owned instance with a single condition

On a non-deleting `ModuleInstance` with `spec.owner == cli`, the controller MUST set `Ready: Unknown` with reason `ManagedExternally` and MUST NOT write any other status field — specifically it MUST NOT set `status.observedGeneration`, and MUST NOT modify `status.inventory`, the `lastApplied*` digests, `instanceUUID`, or any field the CLI writes. The `internal/status` package MUST expose a `ManagedExternally` reason constant and a helper that sets this condition (clearing `Reconciling`, `Stalled` and `Healthy`; the operator judges no health on an instance it does not reconcile, so a `Healthy` condition left from an operator-owned past would describe objects it no longer follows). The acknowledgement MUST be idempotent: reconciling an already-acknowledged instance MUST produce no status change.

#### Scenario: ManagedExternally condition is set

- **GIVEN** a `ModuleInstance` with `spec.owner == cli`
- **WHEN** the controller reconciles it
- **THEN** the `Ready` condition is `Unknown` with reason `ManagedExternally`
- **AND** the `Reconciling`, `Stalled` and `Healthy` conditions are absent

#### Scenario: No observedGeneration and no CLI-written status is touched

- **GIVEN** a `ModuleInstance` with `spec.owner == cli` carrying CLI-written `status.inventory` and `lastApplied*` digests
- **WHEN** the controller reconciles it
- **THEN** `status.observedGeneration` is not set by the controller
- **AND** `status.inventory`, the `lastApplied*` digests, and `instanceUUID` are unchanged

#### Scenario: Re-acknowledgement is a no-op

- **GIVEN** a `ModuleInstance` with `spec.owner == cli` already carrying `Ready: Unknown / ManagedExternally`
- **WHEN** the controller reconciles it again (e.g. after a requeue or an informer resync)
- **THEN** the resulting status patch is empty and no condition transition timestamp changes

### Requirement: The operator never reconciles its own instance

On the operator's own instance, whatever `spec.owner` says, the controller MUST never apply, never prune and never add the `opmodel.dev/cleanup` finalizer. An own instance with `spec.owner: cli` is handled by the owner-skip gate and acknowledged with `ManagedExternally`; an own instance with `spec.owner` absent or `operator` MUST be refused before finalizer registration, before the suspend check, and before any render. On refusal the controller MUST:

- set `Ready=False` and `Stalled=True`, both with reason `SelfManagementRefused`, and remove `Reconciling`, `ModuleResolved`, `Drifted` and `Healthy`, with a message that names the signal that matched and says to set `spec.owner` to `cli`;
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

- **GIVEN** the operator's own instance with `spec.owner: operator` carrying `ModuleResolved=True`, `Drifted=True` and `Healthy=True` from an earlier reconcile
- **WHEN** the controller refuses it
- **THEN** `ModuleResolved`, `Drifted` and `Healthy` are removed

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
