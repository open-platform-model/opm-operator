## Why

ADR-012 mandates that v1alpha1 detects drift but does not automatically correct it. Currently, Phase 4 (Plan Actions) only performs digest-based no-op detection. It does not perform SSA dry-run to detect whether live cluster state has drifted from the desired state. Without drift detection, operators have no visibility into whether managed resources have been modified by other actors (e.g., manual edits, webhooks, other controllers).

## What Changes

- Add SSA dry-run in Phase 4 after no-op detection to compare desired state against live cluster state.
- Define a `Drifted` condition type and set `Drifted=True` when drift is detected.
- Clear `Drifted` condition after a successful apply (drift is resolved by the apply).
- Drift detection runs even on no-op reconciles (digests unchanged but cluster may have drifted).
- No automatic correction — detection only per ADR-012.

## Capabilities

### New Capabilities
- `drift-detection`: SSA dry-run based drift detection with `Drifted` condition reporting.

### Modified Capabilities

## Impact

- `internal/apply/` — New `DetectDrift(ctx, resources)` function using SSA dry-run.
- `internal/status/` — New `DriftedCondition` type constant and `DriftDetected` reason constant.
- `internal/reconcile/modulerelease.go` — Phase 4 gains drift detection step.
- `api/v1alpha1/` — No API changes (conditions are dynamic, `Drifted` is a new condition type string).
- `status.failureCounters.drift` — Incremented when drift detection itself fails.
- SemVer: MINOR — new capability.
