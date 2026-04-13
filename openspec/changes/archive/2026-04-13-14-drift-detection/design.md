## Context

ADR-012 specifies drift detection without correction for v1alpha1. Detection uses SSA dry-run: apply each desired resource with `dryRun=All` and compare the result to what was sent. If the API server returns a different object, that resource has drifted. The Flux `ssa.ResourceManager` supports dry-run apply natively.

Phase 4 (Plan Actions) from ADR-009 is the designated location for drift detection. It runs after rendering (Phase 3) and before actual apply (Phase 5).

## Goals / Non-Goals

**Goals:**

- Implement `DetectDrift` in `internal/apply/` using SSA dry-run via Flux's `ResourceManager`.
- Return a `DriftResult` listing which resources have drifted.
- Set `Drifted=True` condition when any resource has drifted.
- Clear `Drifted` condition after successful apply.
- Integrate into Phase 4 of the reconcile loop.

**Non-Goals:**

- Automatic drift correction (deferred per ADR-012 to future `spec.rollout.driftCorrection`).
- Field-level diff reporting (v1alpha1 reports presence/absence of drift, not specific fields).
- Drift detection for resources not in inventory (out of scope).

## Decisions

### 1. SSA dry-run via Flux ResourceManager

Use `ResourceManager.ApplyAllStaged` with `dryRun=true` (or equivalent dry-run mode). Compare the returned objects against the desired objects. Any difference indicates drift. This reuses the same SSA machinery from change 08.

### 2. Drift detection runs on every reconcile, including no-ops

Even when digests haven't changed (no-op), cluster state may have drifted. Drift detection SHOULD run after no-op detection. If digests indicate no-op AND no drift, skip apply. If digests indicate no-op BUT drift detected, still skip apply (detection only) but set the condition.

### 3. DriftResult is a simple struct

```go
type DriftResult struct {
    Drifted   bool
    Resources []DriftedResource
}

type DriftedResource struct {
    Group     string
    Kind      string
    Namespace string
    Name      string
}
```

### 4. Drifted condition is informational only

`Drifted=True` does not change the `Ready` condition. A resource can be `Ready=True` and `Drifted=True` simultaneously. This is detection, not a failure state.

### 5. Drift counter incremented on detection failure, not detection result

`status.failureCounters.drift` is incremented when the dry-run API call itself fails (transient error), not when drift is found. Finding drift is a normal operational signal, not a failure.

## Risks / Trade-offs

- **[Risk] SSA dry-run API load** — Dry-run applies for every resource every reconcile adds API server load. Mitigation: acceptable for v1alpha1; can be gated behind a flag in future.
- **[Risk] Mutating webhooks cause false positives** — If a webhook modifies resources on apply, dry-run results will differ from desired state even though the mutation is intentional. Mitigation: this is exactly why v1alpha1 is detection-only (ADR-012 rationale).
- **[Trade-off] No field-level reporting** — Operators see "drifted: yes" but not what changed. Acceptable for v1alpha1; detailed diff can be added later.
