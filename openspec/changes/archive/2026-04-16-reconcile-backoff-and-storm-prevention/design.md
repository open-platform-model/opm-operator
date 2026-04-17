## Context

The controller currently has no explicit backoff control. Three problems exist:

1. **NoOp reconcile storm**: Status patches bump `resourceVersion`, triggering watch events that re-enqueue reconciles. The current mitigation (`shouldSkipStatusPatch` in `internal/reconcile/modulerelease.go`) catches NoOp outcomes and skips the patch, but this is a band-aid at the wrong layer — the reconcile function still runs, including expensive CUE synthesis + OCI module loading, before discovering nothing changed.

2. **Opaque transient retries**: `FailedTransient` returns `err`, delegating to controller-runtime's default workqueue rate limiter (5ms base, 1000s max). The 5ms floor means the first retry runs CUE synthesis almost immediately after a failure. Operators cannot see when the next retry will occur.

3. **No safety net for unknown storms**: If a new code path introduces unexpected requeue behavior, there's no floor on retry frequency.

The `GenerationChangedPredicate` from controller-runtime filters Update events where `metadata.generation` didn't change. With the status subresource enabled (`+kubebuilder:subresource:status`), status patches do NOT increment generation — only spec changes do. Scheduled requeues via `RequeueAfter` bypass predicates entirely (they go directly into the workqueue), so future periodic drift detection is unaffected.

## Goals / Non-Goals

**Goals:**
- Prevent reconcile storms from status-only watch events at the informer level (cheapest layer).
- Give operators visibility into retry timing via `status.nextRetryAt`.
- Control backoff curve explicitly rather than depending on opaque workqueue defaults.
- Add a safety-net rate limiter for unexpected error returns.
- Add periodic safety recheck for `FailedStalled` to guard against misclassification.

**Non-Goals:**
- Periodic drift detection scheduling (separate concern, future change).
- Per-phase backoff (e.g., different curves for apply vs render failures). Single reconcile-level backoff is sufficient for v1alpha1.
- Configurable backoff parameters via CRD spec. Hardcoded constants are appropriate until real-world tuning data exists.

## Decisions

### Decision 1: GenerationChangedPredicate on controller setup

Add `predicate.GenerationChangedPredicate{}` via `WithEventFilter` in `SetupWithManager`. This filters out Update events where `metadata.generation` didn't change, preventing status-only patches from enqueuing reconciles.

**Alternative considered**: Keep `shouldSkipStatusPatch` and add a generation check inside the reconcile function. Rejected because the reconcile function still runs (including expensive CUE work) before the check, and the predicate prevents enqueue entirely — strictly cheaper.

**Consequence**: `shouldSkipStatusPatch` becomes redundant and MUST be removed to reduce complexity.

**Note on annotations**: `metadata.generation` does NOT increment on label/annotation changes. If a "force reconcile" annotation pattern is needed in the future, it would require an additional predicate (e.g., `AnnotationChangedPredicate`). This is not a blocker — no such pattern exists today.

### Decision 2: Explicit backoff via RequeueAfter for FailedTransient

Instead of returning `err` (which triggers the opaque workqueue rate limiter), return `ctrl.Result{RequeueAfter: backoff}, nil` where `backoff` is computed from `failureCounters.reconcile`.

Backoff formula: `min(baseDelay * 2^(failures-1), maxDelay)`
- `baseDelay`: 5 seconds
- `maxDelay`: 5 minutes

| Consecutive failures | Delay |
|---------------------|-------|
| 1 | 5s |
| 2 | 10s |
| 3 | 20s |
| 4 | 40s |
| 5 | 80s |
| 6 | 160s |
| 7+ | 300s (cap) |

The backoff function lives in `internal/reconcile/` alongside the outcome types.

**Alternative considered**: Keep returning `err` and reverse-engineer the workqueue delay for `nextRetryAt`. Rejected because the delay is non-deterministic (depends on workqueue state) and can't be accurately predicted for the status field.

### Decision 3: Periodic safety recheck for FailedStalled

`FailedStalled` currently waits indefinitely for a spec change. Add a long periodic recheck (30 minutes) as a safety net against misclassification. If a transient error was incorrectly classified as stalled, the controller will eventually retry.

Return `ctrl.Result{RequeueAfter: 30m}, nil` for `FailedStalled` and set `nextRetryAt` accordingly.

### Decision 4: Custom workqueue rate limiter

Replace the default workqueue rate limiter with one that has a 1s floor (instead of 5ms). This only activates if the reconcile function returns a non-nil error, which should be rare after Decision 2 (most paths return explicit `RequeueAfter`). Acts as a safety net for unexpected panics or bugs.

Configuration: `ItemExponentialFailureRateLimiter(1s, 5m)` + `BucketRateLimiter(10 qps, burst 100)`.

### Decision 5: nextRetryAt status field

Add `NextRetryAt *metav1.Time` to `ModuleReleaseStatus`. Set it before returning `RequeueAfter` for failed outcomes. Clear it (set to nil) on success or NoOp.

This field is written in the deferred status patch, alongside other status fields. The generation predicate ensures this status patch does not trigger a re-reconcile.

## Risks / Trade-offs

**Risk: Generation predicate filters out annotation-triggered reconciles** → Accepted. No annotation-based reconcile pattern exists today. If needed, add `Or(GenerationChangedPredicate, AnnotationChangedPredicate)` as a future change.

**Risk: FailedStalled 30m recheck adds unnecessary reconciles for truly stalled resources** → Acceptable cost. One reconcile per 30 minutes per stalled resource is negligible. The alternative (waiting indefinitely on misclassification) is worse.

**Risk: Explicit RequeueAfter means the workqueue's per-key dedup no longer applies for failures** → Mitigated. `RequeueAfter` still deduplicates with pending items for the same key. If a spec change triggers a watch event before the timer fires, the reconcile runs immediately (correct behavior).

**Trade-off: Removing shouldSkipStatusPatch simplifies code but removes a defense layer** → The generation predicate is the correct fix at the correct layer. Keeping both adds reasoning complexity for no benefit.
