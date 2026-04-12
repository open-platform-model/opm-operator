## Why

The reconcile loop uses three digests (source, config, render) plus the inventory digest to detect changes and avoid unnecessary apply cycles. Without digest computation, the controller cannot implement no-op detection — it would re-apply on every reconcile even when nothing changed. The design docs specify these as first-class status fields.

## What Changes

- Implement source digest (passthrough from Flux artifact), config digest (SHA-256 of normalized values), and render digest (SHA-256 of sorted serialized resources) in `internal/status`.
- Implement `DigestSet` type and `IsNoOp` helper for comparing all four digests.
- Replace the `Digests` stub in `internal/status/digests.go`.

## Capabilities

### New Capabilities
- `digest-computation`: Compute source, config, render, and inventory digests; detect no-op reconciliations by comparing digest sets.

### Modified Capabilities

## Impact

- `internal/status/digests.go` — stub replaced with real digest functions.
- Uses locally copied `pkg/core.Resource.MarshalJSON()` for render digest serialization.
- Uses `internal/inventory.ComputeDigest` for inventory digest (from change 1).
- SemVer: MINOR — new capability.
