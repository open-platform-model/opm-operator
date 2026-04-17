## Why

The reconcile loop must communicate its state to users and to Kubernetes tooling through standardized conditions on the ModuleRelease status. The current `internal/status` package has only a single `ReadyCondition` constant. The controller needs a full condition management system with Flux-compatible helpers for setting conditions, tracking reasons, and patching status safely.

## What Changes

- Define all condition type constants (`Ready`, `Reconciling`, `Stalled`, `SourceReady`) and reason constants.
- Implement helper functions for setting conditions consistently.
- Integrate with `fluxcd/pkg/runtime/conditions` for condition manipulation.
- Integrate with `fluxcd/pkg/runtime/patch.SerialPatcher` for safe status patching.

## Capabilities

### New Capabilities
- `status-conditions`: Condition type and reason constants, helper functions for condition transitions, and status patching integration.

### Modified Capabilities

## Impact

- `internal/status/conditions.go` — expanded from single constant to full condition management.
- New dependency on `fluxcd/pkg/runtime/conditions` and `fluxcd/pkg/runtime/patch`.
- SemVer: MINOR — new capability. Can run in parallel with changes 3-5.
