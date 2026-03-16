## Why

When a module release changes and resources are removed from the desired set, the controller must clean up stale resources that are no longer needed. Without pruning, orphaned resources accumulate in the cluster. The design docs specify pruning as opt-in via `spec.prune` with safety exclusions for Namespaces and CRDs.

## What Changes

- Implement `Prune` function in `internal/apply` that deletes stale resources from the cluster.
- Enforce safety exclusions: never auto-delete Namespaces or CustomResourceDefinitions.
- Return a `PruneResult` with counts of deleted and skipped resources.

## Capabilities

### New Capabilities
- `prune-stale`: Delete resources present in the previous inventory but absent from the current desired set, with safety exclusions for Namespaces and CRDs.

### Modified Capabilities

## Impact

- `internal/apply/prune.go` — stub replaced with real prune logic.
- Uses inventory stale set computation from change 1.
- Tests require envtest.
- Depends on: change 1 (inventory stale set), change 8 (SSA apply for test setup).
- SemVer: MINOR — new capability.
