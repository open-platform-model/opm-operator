## Context

The design docs specify four digests tracked in `ModuleRelease.status`:
- **Source digest** — artifact content hash from Flux (already computed).
- **Config digest** — hash of normalized user values, avoids persisting large/sensitive config.
- **Render digest** — hash of the deterministic rendered resource set, detects output changes.
- **Inventory digest** — hash of the owned resource set (implemented in `internal/inventory.ComputeDigest`, copied from CLI in change 1).

These enable no-op detection: if all four match the last applied values, skip apply/prune entirely.

## Goals / Non-Goals

**Goals:**
- Implement config and render digest functions.
- Implement `DigestSet` and `IsNoOp` comparison.
- Source digest is passthrough; inventory digest uses the existing bridge.

**Non-Goals:**
- Caching digests across reconciliations.
- Signed digests or provenance chains.

## Decisions

### 1. Config digest uses canonical JSON

Serialize `RawValues` to canonical JSON (sorted keys), then SHA-256. If values are nil, return an empty string (nil values = no config).

### 2. Render digest sorts resources before hashing

Sort resources by GVK + namespace + name (same order as inventory), serialize each to JSON, hash the concatenation. This matches `inventory.ComputeDigest` ordering for consistency.

### 3. DigestSet is a plain struct, not a map

Four named fields (`Source`, `Config`, `Render`, `Inventory`) rather than `map[string]string`. Type safety over flexibility.

## Risks / Trade-offs

- **[Risk] Digest collision** — SHA-256 collisions are astronomically unlikely. Acceptable for this use case.
- **[Trade-off] Render digest recomputation** — The render digest requires JSON serialization of all resources. For large resource sets this has a cost. Acceptable for v1alpha1.
