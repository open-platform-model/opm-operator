## Why

The ModuleRelease CRD defines `status.failureCounters` with four counters (reconcile, apply, prune, drift) but no change implements the increment/reset logic. Without failure counters, operators cannot distinguish between a resource that failed once transiently and one that has been failing repeatedly. Counters enable alerting thresholds, debugging patterns, and tie into ADR-005's failure classification model.

## What Changes

- Implement helpers to increment individual failure counters after phase failures.
- Reset relevant counters after successful phases (e.g., reset `apply` counter after successful apply).
- Initialize `status.failureCounters` on first reconcile if nil.
- Integrate counter updates into the reconcile loop's Phase 7 (status commit).

## Capabilities

### New Capabilities
- `failure-counters`: Failure counter increment/reset helpers and reconcile loop integration.

### Modified Capabilities

## Impact

- `internal/status/` — New `IncrementFailureCounter` and `ResetFailureCounter` helpers.
- `internal/reconcile/modulerelease.go` — Phase 7 updates counters based on reconcile outcome.
- No API changes. Uses existing `status.failureCounters` fields.
- SemVer: MINOR — new capability.
