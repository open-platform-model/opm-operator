## 1. API

- [x] 1.1 Add `NextRetryAt *metav1.Time` field to `ModuleReleaseStatus` in `api/v1alpha1/modulerelease_types.go` with `+optional` marker and `json:"nextRetryAt,omitempty"` tag
- [x] 1.2 Add `+kubebuilder:printcolumn` marker for `nextRetryAt` (priority=1) on the `ModuleRelease` type
- [x] 1.3 Run `make manifests generate` to regenerate CRD and DeepCopy

## 2. Backoff Computation

- [x] 2.1 Add `ComputeBackoff(failureCount int64) time.Duration` function in `internal/reconcile/` with 5s base, 5m max, formula `min(5s * 2^(failures-1), 5m)`
- [x] 2.2 Add unit tests for `ComputeBackoff` covering: first failure (5s), third failure (20s), cap at 7+ failures (5m), zero failures (5s floor)

## 3. Controller Setup

- [x] 3.1 Add `predicate.GenerationChangedPredicate{}` via `WithEventFilter` in `SetupWithManager`
- [x] 3.2 Add custom workqueue rate limiter (`ItemExponentialFailureRateLimiter(1s, 5m)` + `BucketRateLimiter(10 qps, burst 100)`) via `WithOptions(controller.Options{RateLimiter: ...})`

## 4. Reconcile Loop Changes

- [x] 4.1 Change `FailedTransient` return path: replace `return ctrl.Result{}, err` with `return ctrl.Result{RequeueAfter: ComputeBackoff(...)}, nil`; set `status.NextRetryAt` before returning
- [x] 4.2 Change `FailedStalled` return path: replace `return ctrl.Result{}, nil` with `return ctrl.Result{RequeueAfter: 30m}, nil`; set `status.NextRetryAt` before returning
- [x] 4.3 Clear `status.NextRetryAt` (set to nil) in the deferred status commit for success outcomes (`Applied`, `AppliedAndPruned`, `NoOp`)
- [x] 4.4 Remove `shouldSkipStatusPatch` function and its call site in the deferred status commit

## 5. Validation

- [x] 5.1 Update existing tests that assert `RequeueAfter == 0` for `FailedStalled` paths to assert `RequeueAfter == 30m`
- [x] 5.2 Update existing tests that assert error return for `FailedTransient` paths to assert `RequeueAfter > 0` and nil error
- [x] 5.3 Run `make fmt vet lint test`
