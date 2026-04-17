## Why

The ModuleRelease controller currently has no finalizer registration or deletion cleanup logic. When a ModuleRelease is deleted, its managed resources are orphaned on the cluster. The controller must register a finalizer to ensure owned resources (tracked in `status.inventory`) are pruned before the ModuleRelease object is removed. This is a prerequisite for safe resource lifecycle management. ADR-011 mandates safety exclusions (Namespaces and CRDs are never auto-pruned), which must also apply during deletion cleanup.

## What Changes

- Register a finalizer (`releases.opmodel.dev/cleanup`) on first reconcile if not present.
- On deletion: prune all `status.inventory` entries, respecting ADR-011 safety exclusions (skip Namespaces and CRDs).
- Remove the finalizer after successful cleanup, allowing Kubernetes to complete deletion.
- If `spec.prune` is `false`, skip resource cleanup on deletion but still remove the finalizer (resources are intentionally orphaned).
- If `spec.suspend` is `true` during deletion, still perform cleanup (suspend must not block deletion).

## Capabilities

### New Capabilities
- `finalizer-and-deletion`: Finalizer registration, deletion detection, inventory-based cleanup with safety exclusions, and finalizer removal.

### Modified Capabilities

## Impact

- `internal/reconcile/modulerelease.go` — Phase 0 gains finalizer registration and deletion branch.
- `internal/apply/prune.go` — Reuses existing `Prune` function for deletion cleanup.
- `internal/controller/modulerelease_controller.go` — RBAC markers may need update for finalizer patching.
- No new API fields. Uses existing `status.inventory` and `spec.prune`.
- SemVer: MINOR — new capability, no breaking changes.
