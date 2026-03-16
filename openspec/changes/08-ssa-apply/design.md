## Context

The design doc (`ssa-ownership-and-drift-policy.md`) specifies:
- Field manager name: `opm-controller`
- Default `force: false`, opt-in via `spec.rollout.forceConflicts`
- Staged apply: CRDs and Namespaces in stage 1, everything else in stage 2
- Flux's `ssa.ResourceManager.ApplyAllStaged` as the apply mechanism

## Goals / Non-Goals

**Goals:**
- Construct a Flux SSA `ResourceManager` with `opm-controller` field manager.
- Sort resources into stages using `resourceorder.GetWeight`.
- Apply stage 1 (CRDs, Namespaces) then stage 2 (everything else).
- Return an `ApplyResult` with counts of created/updated/unchanged.

**Non-Goals:**
- Pruning (that's change 09).
- Drift detection via dry-run (deferred per design doc).
- Immutable field recreation (deferred).

## Decisions

### 1. Use Flux's ApplyAllStaged

`ResourceManager.ApplyAllStaged` handles the two-stage apply pattern natively. Pass pre-sorted stage sets.

### 2. Resource ordering via locally copied resourceorder package

Use `resourceorder.GetWeight(gvk)` to classify resources. Weight < 100 goes to stage 1 (CRDs, Namespaces, ServiceAccounts, etc.), everything else to stage 2. This reuses the ordering logic copied from the CLI in change 1.

### 3. ApplyResult is a simple counter struct

`ApplyResult` carries `Created`, `Updated`, `Unchanged` counts. No per-resource detail for v1alpha1.

## Risks / Trade-offs

- **[Risk] API server rate limiting** — Large resource sets may hit API server limits. Mitigation: Flux's `ResourceManager` handles retries internally.
- **[Risk] Partial apply failure** — If stage 1 succeeds but stage 2 fails, the cluster is in a partially applied state. Mitigation: this is inherent to Kubernetes; the next reconcile retries.
