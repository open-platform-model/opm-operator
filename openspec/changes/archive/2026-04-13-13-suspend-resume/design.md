## Context

Flux controllers implement a standard suspend pattern: when `spec.suspend=true`, the controller skips reconciliation, sets a condition, and returns without requeueing. Change 07 already defines a `Suspended` reason constant. Change 11's Phase 0 mentions "check suspend" but doesn't specify the full behavior.

## Goals / Non-Goals

**Goals:**
- Implement suspend check in Phase 0 after finalizer handling.
- Set `Ready=False` with reason `Suspended` and clear `Reconciling` when suspended.
- Return `ctrl.Result{}` (no requeue) when suspended.
- On unsuspend: controller-runtime's generation-based predicate triggers a reconcile naturally.
- Log entering/exiting suspend state.

**Non-Goals:**
- Pausing in-flight operations mid-reconcile (suspend is checked at entry only).
- Suspend for BundleRelease.
- Clearing status digests or inventory on suspend (state preserved).

## Decisions

### 1. Suspend check position: after finalizer, before source

Suspend MUST NOT block deletion cleanup (per finalizer-and-deletion change). The check runs after the deletion branch in Phase 0, before Phase 1 source resolution. This means a suspended resource with a deletion timestamp still gets cleaned up.

### 2. No explicit requeue on unsuspend

When `spec.suspend` changes from `true` to `false`, the spec generation increments, which triggers a reconcile via the default controller-runtime predicate. No special watch or requeue logic needed.

### 3. Condition state during suspend

- `Ready` → `False`, reason `Suspended`, message "Reconciliation is suspended"
- `Reconciling` → removed (not actively reconciling)
- `Stalled` → removed (not stalled, just paused)
- `SourceReady` → preserved as-is (no source check performed)

### 4. Status is preserved across suspend/resume

`status.inventory`, digests, and history are not cleared when entering suspend. On resume, the normal reconcile loop detects whether anything changed via digest comparison.

## Risks / Trade-offs

- **[Trade-off] No mid-reconcile pause** — If a reconcile is in progress when suspend is set, it completes. Suspend is only checked at the start of the next reconcile. This is the standard Flux behavior and keeps the reconcile loop simple.
