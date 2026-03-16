## Why

The design docs specify bounded reconcile history as a compact metadata trail in `ModuleRelease.status.history`. This gives operators visibility into what happened and when, without storing full manifests. The current `internal/status/history.go` is an empty stub.

## What Changes

- Implement history entry construction helpers for success and failure outcomes.
- Implement bounded history append that prepends entries and trims to a configurable maximum (default 10).
- Replace the `History` stub in `internal/status/history.go`.

## Capabilities

### New Capabilities
- `history-tracking`: Bounded reconcile history management with entry construction, append, and retention.

### Modified Capabilities

## Impact

- `internal/status/history.go` — stub replaced with real history management.
- Uses `DigestSet` from change 6 and `v1alpha1.HistoryEntry` from the CRD API.
- SemVer: MINOR — new capability.
