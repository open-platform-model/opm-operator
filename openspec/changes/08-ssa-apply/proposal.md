## Why

After rendering produces Kubernetes resources, the controller must apply them to the cluster using Server-Side Apply (SSA). This is Phase 5 of the reconcile loop. The current `internal/apply` package has only a type alias for Flux's `ResourceManager` and an empty `Prune` struct. The controller needs staged apply logic (CRDs/Namespaces first, then everything else) with configurable force-conflicts behavior.

## What Changes

- Implement `Apply` function in `internal/apply` using Flux's SSA `ResourceManager`.
- Use `opm-controller` as the SSA field manager name.
- Implement staged apply: CRDs and Namespaces first, then all other resources.
- Use locally copied `pkg/resourceorder.GetWeight` for resource ordering.
- Support `spec.rollout.forceConflicts` for SSA force mode.

## Capabilities

### New Capabilities
- `ssa-apply`: Apply rendered Kubernetes resources to the cluster using SSA with staged ordering and configurable force-conflicts.

### Modified Capabilities

## Impact

- `internal/apply/` — stubs replaced with real SSA apply logic.
- Uses `fluxcd/pkg/ssa.ResourceManager` for apply operations.
- Uses locally copied `pkg/resourceorder.GetWeight` for ordering (copied from CLI in change 1).
- Tests require envtest.
- SemVer: MINOR — new capability.
