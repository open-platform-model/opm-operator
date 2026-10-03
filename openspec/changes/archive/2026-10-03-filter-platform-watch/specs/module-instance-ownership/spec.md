## MODIFIED Requirements

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
- **WHEN** the controller reconciles it again (e.g. after a requeue or an informer resync)
- **THEN** the resulting status patch is empty and no condition transition timestamp changes
