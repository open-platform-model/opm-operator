## Context

Kubernetes finalizers prevent object deletion until a controller performs cleanup. The ModuleRelease controller must ensure owned resources are removed before the CR itself is deleted. The prune logic from change 09 already implements safety exclusions (never auto-delete Namespaces or CRDs). Deletion cleanup reuses that same prune path.

## Goals / Non-Goals

**Goals:**
- Register finalizer `releases.opmodel.dev/cleanup` during Phase 0 if not present.
- Detect deletion (non-zero `DeletionTimestamp`) in Phase 0 and branch to cleanup.
- Prune all inventory entries using existing `internal/apply.Prune`, respecting safety exclusions.
- Remove finalizer after successful cleanup.
- Honor `spec.prune` — if false, skip resource deletion but still remove finalizer.

**Non-Goals:**
- Finalizer for BundleRelease (not in scope).
- Cascading deletion order (resources are deleted individually, Kubernetes handles cascading).
- Blocking deletion when suspend is true (suspend MUST NOT prevent cleanup).

## Decisions

### 1. Finalizer name follows API group convention

Use `releases.opmodel.dev/cleanup` as the finalizer string. This follows the Kubernetes convention of `<api-group>/<purpose>`.

### 2. Deletion branch in Phase 0 short-circuits the reconcile loop

When `DeletionTimestamp` is set, the reconciler skips phases 1-7 entirely. It runs the cleanup path: prune inventory entries → remove finalizer → return. No source resolution, rendering, or status commit needed.

### 3. Reuse existing Prune function for cleanup

The `internal/apply.Prune` function already handles safety exclusions and fail-slow error collection. Deletion cleanup passes the full `status.inventory.entries` as the stale set.

### 4. spec.prune=false means orphan on delete

If the user explicitly set `spec.prune=false`, they've opted out of automatic resource cleanup. On deletion, the controller removes the finalizer without pruning. Resources become unmanaged.

### 5. Suspend does not block deletion

Even if `spec.suspend=true`, the deletion cleanup path MUST execute. Suspend only gates normal reconciliation, not Kubernetes object lifecycle.

## Risks / Trade-offs

- **[Risk] Partial cleanup failure** — If some resources fail to delete, the finalizer blocks CR deletion. Mitigation: fail-slow error collection logs individual failures; operator can manually delete stuck resources and the next reconcile retries.
- **[Risk] Large inventory cleanup** — Deleting many resources may take time. Mitigation: acceptable for v1alpha1; operator can force-delete the finalizer if needed.
- **[Trade-off] Safety exclusions leave orphans** — Namespaces and CRDs skipped during cleanup remain on cluster. This is intentional per ADR-011.
