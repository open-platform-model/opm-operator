## Why

The controller lacks explicit backoff control and is vulnerable to reconcile storms. Status-only patches trigger watch events that re-enqueue reconciles, and the current mitigation (`shouldSkipStatusPatch`) is a band-aid at the wrong layer. Transient failure retries use controller-runtime's opaque default rate limiter (5ms base), burning CPU on expensive CUE synthesis that will fail identically. Operators have no visibility into when the next retry will occur.

## What Changes

- Add `GenerationChangedPredicate` to the controller's event filter so status-only updates never enqueue a reconcile (prevents NoOp storm at the informer level).
- Replace implicit error-based requeue for `FailedTransient` with explicit `RequeueAfter` using exponential backoff derived from `failureCounters.reconcile` (5s base, 5m cap).
- Add periodic safety recheck for `FailedStalled` (long interval, e.g. 30m) to guard against misclassification.
- Add `status.nextRetryAt` field so operators can see when the next reconcile attempt is scheduled.
- Add custom workqueue rate limiter with a 1s floor (safety net for unexpected error returns).
- Remove `shouldSkipStatusPatch` — made redundant by the generation predicate.

SemVer: **MINOR** (new status field `nextRetryAt`, no breaking changes).

## Capabilities

### New Capabilities
- `reconcile-backoff`: Exponential backoff computation for failed reconciles, `nextRetryAt` status field, and generation-based event filtering.

### Modified Capabilities
- `reconcile-loop-assembly`: FailedTransient requeue changes from returning `err` to explicit `RequeueAfter`. FailedStalled gains a periodic safety recheck. `shouldSkipStatusPatch` removed. Generation predicate added to controller setup.

## Impact

- **API**: `ModuleReleaseStatus` gains `NextRetryAt *metav1.Time` field. Requires `make manifests generate`.
- **Controller setup** (`SetupWithManager`): Predicate and custom rate limiter added.
- **Reconcile function** (`internal/reconcile/modulerelease.go`): FailedTransient and FailedStalled return paths change. `shouldSkipStatusPatch` removed.
- **Tests**: Existing tests asserting `RequeueAfter == 0` for success paths remain valid. New tests needed for backoff computation and predicate behavior.
- **No breaking changes** to existing CRD consumers. `nextRetryAt` is optional and additive.
