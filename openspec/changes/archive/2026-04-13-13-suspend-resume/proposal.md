## Why

The ModuleRelease CRD has a `spec.suspend` field but no defined controller behavior for it. When an operator sets `suspend=true`, the controller should skip reconciliation entirely and surface a clear condition. When suspend is flipped back to `false`, the controller should immediately resume reconciliation. This is a standard Flux pattern that operators expect for maintenance windows, debugging, and staged rollouts.

## What Changes

- When `spec.suspend=true`: skip all reconcile phases, set `Ready=False` with reason `Suspended`, clear `Reconciling` condition.
- When `spec.suspend` transitions from `true` to `false`: requeue immediately for a full reconcile.
- Emit a log message when entering/exiting suspend.
- Suspend check happens in Phase 0, after finalizer handling but before source resolution.

## Capabilities

### New Capabilities
- `suspend-resume`: Suspend detection, condition management during suspension, and immediate resume on unsuspend.

### Modified Capabilities

## Impact

- `internal/reconcile/modulerelease.go` — Phase 0 suspend check logic.
- `internal/status/` — May need a `Suspended` reason constant (likely already defined in change 07).
- No new API fields. Uses existing `spec.suspend`.
- SemVer: MINOR — new capability.
