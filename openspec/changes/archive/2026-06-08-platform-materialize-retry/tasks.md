## 1. Intervals + classification

- [x] 1.1 Add a short transient retry interval constant for the platform controller; reuse `reconcile.StalledRecheckInterval` (import `internal/reconcile`) for the default/long interval
- [x] 1.2 Add `isTransientMaterialize(err error) bool`: unwrap to the cause and report transient via `errors.As` for `net.Error` (Timeout), `*url.Error`, and `context.DeadlineExceeded`; default false (→ long interval)
- [x] 1.3 Unit test the classifier: transient causes → true; semantic/unknown → false

## 2. Reconcile failure branches

- [x] 2.1 In `internal/controller/platform_controller.go`, on the `SynthesizePlatform` and `Materialize` failure branches, set `plat.Status.ObservedGeneration = plat.Generation` before patching
- [x] 2.2 Choose `RequeueAfter` via `isTransientMaterialize` (short for transient, `StalledRecheckInterval` otherwise) and return `ctrl.Result{RequeueAfter: interval}, r.patchStatus(...)`
- [x] 2.3 Emit the warning event only on transition into / message-change of the failed state (compare prior condition before patch), not on every recheck
- [x] 2.4 Leave the store untouched on failure (preserve last-good — unchanged)

## 3. Tests + validation gates

- [x] 3.1 Test: transient materialize failure → result has the short `RequeueAfter`; `Ready=False`/`MaterializeFailed`
- [x] 3.2 Test: semantic/unclassifiable failure → result has the long `RequeueAfter`
- [x] 3.3 Test: `observedGeneration` is set to the reconciled generation on the failure path
- [x] 3.4 Test: a repeated identical failure does not re-emit the warning event
- [x] 3.5 Test: a previously stored good platform is still held after a failed reconcile
- [x] 3.6 `task dev:fmt dev:vet`
- [x] 3.7 `task dev:lint`
- [x] 3.8 `task dev:test`
- [x] 3.9 Registry-backed integration spec (`test/integration/reconcile/platform_recovery_test.go`) for spec scenario "Recovery without a spec change": same Platform CR (unchanged generation) goes `MaterializeFailed` → `Ready` once the registry clears; skips without a reachable registry + `OPM_TEST_CATALOG_PATH`
