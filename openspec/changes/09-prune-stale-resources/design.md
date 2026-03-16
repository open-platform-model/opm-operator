## Context

The design doc (`ssa-ownership-and-drift-policy.md`) defines pruning rules:
- Prune only runs when `spec.prune=true`
- Stale = previous `status.inventory` entries not in current desired set
- Safety exclusions: never auto-delete `Namespace` or `CustomResourceDefinition`
- Prune runs only after apply succeeds (caller enforces ordering)

## Goals / Non-Goals

**Goals:**
- Delete stale resources using the Kubernetes client.
- Skip Namespaces and CRDs with a logged warning.
- Return `PruneResult` with deleted/skipped counts.

**Non-Goals:**
- Deciding whether to prune (that's the reconcile loop's job based on `spec.prune`).
- Computing the stale set (that's the inventory bridge from change 1).
- Cascading deletion or finalizer handling.

## Decisions

### 1. Simple client.Delete, not Flux's DeleteAll

Use direct `client.Delete` per resource rather than `ssa.ResourceManager.DeleteAll`. This gives per-resource error control and allows skip logic for safety exclusions.

### 2. Continue-on-error for individual deletes

If one resource fails to delete, log the error and continue to the next. Collect all errors and return them as a joined error. This follows the fail-slow pattern used elsewhere.

### 3. Safety exclusions are hard-coded, not configurable

Namespace and CRD exclusions are always enforced. No spec field to override them. This matches the design doc's safety-first stance.

## Risks / Trade-offs

- **[Risk] Stale resource not found** — If a stale resource was already deleted (e.g., by another actor), the delete call returns NotFound. Mitigation: treat NotFound as success (resource is already gone).
- **[Risk] RBAC insufficient** — The controller may lack permission to delete certain resource types. Mitigation: the RBAC markers on the reconciler must be broad enough, but this is inherent to Kubernetes.
