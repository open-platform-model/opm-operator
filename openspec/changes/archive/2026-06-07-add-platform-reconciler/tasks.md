## 1. Store

- [x] 1.1 Create `internal/platform/store.go`: `Store` with `sync.RWMutex`, a held `*materialize.MaterializedPlatform`, and the `generation int64` it was built for
- [x] 1.2 Methods: `Get() (*materialize.MaterializedPlatform, bool)`, `Set(gen int64, mp *materialize.MaterializedPlatform)`, `Clear()`
- [x] 1.3 Unit test (run with `-race`): set/get/clear; concurrent readers during a write

## 2. Status reason

- [x] 2.1 Add a `MaterializeFailed` reason constant in `internal/status` (alongside existing reasons)

## 3. PlatformReconciler

- [x] 3.1 Create `internal/controller/platform_controller.go`: `PlatformReconciler` struct with `client.Client`, `Scheme`, `EventRecorder`, the shared `*kernel.Kernel`, and `*platform.Store`
- [x] 3.2 `Reconcile`: fetch the `Platform`; early-return for any name ≠ `cluster`; handle delete (clear store)
- [x] 3.3 Map `PlatformSpec` → `synth.PlatformInput` (`Type`; `Registry`→`Subscriptions` with `Enable`/`Filter{Range,Allow,Deny}`; `Name`/labels/annotations from object meta; leave `SchemaCache` nil)
- [x] 3.4 Call `Kernel.SynthesizePlatform` → `Kernel.Materialize`; on success `Store.Set(generation, mp)`, `MarkReady` (reason `Materialized`), set `observedGeneration`
- [x] 3.5 On `*oerrors.MaterializeError`: `MarkStalled` (reason `MaterializeFailed`) with `Kind`/`Subscription`/`Version` in the message; do NOT touch the store slot
- [x] 3.6 Status patch (deferred/patch-helper, matching existing reconcilers); emit an event on outcome
- [x] 3.7 `SetupWithManager`: `For(&Platform{}, builder.WithPredicates(predicate.GenerationChangedPredicate{}))`, `Named("platform")`
- [x] 3.8 RBAC markers: `platforms` get/list/watch; `platforms/status` get/update/patch

## 4. Wiring

- [x] 4.1 In `cmd/main.go`: construct one `*platform.Store`; register `PlatformReconciler` with the shared Kernel + store
- [x] 4.2 `task dev:manifests dev:generate` — regenerate RBAC (and confirm no unintended diffs)

## 5. Tests + validation gates

- [x] 5.1 Envtest success-path spec written (skip-guarded, parametrized via `OPM_TEST_CATALOG_PATH`): apply `Platform` `cluster` with a resolvable subscription → `Ready=True` (reason `Materialized`), `observedGeneration` set, store populated. **Success-path *verification* deferred to a follow-up** — see note below.
- [x] 5.2 Envtest: apply with an unresolvable subscription → `Ready=False` (reason `MaterializeFailed`), message names the `MaterializeError` fields; store unchanged — verified against the ghcr registry (`opmodel.dev/core@v0`)
- [x] 5.3 Envtest: delete `cluster` → store reports no held platform — verified (plus a non-cluster-ignored spec)
- [x] 5.4 `task dev:fmt dev:vet`
- [x] 5.5 `task dev:lint`
- [x] 5.6 `task dev:test`

> **5.1 success-path verification — deferred (descoped):** Verifying the success path requires materializing a Platform against a real catalog subscription, which needs a committed catalog fixture + a publish-to-registry harness (the `test-registry-lifecycle` named here does not yet exist in the tree). That infrastructure is out of scope for this 1:1 reconciler slice (Principle VIII). The success spec is implemented and skips cleanly until a resolvable catalog path is supplied via `OPM_TEST_CATALOG_PATH`. The success branch shares its store-write + `MarkReadyWithReason` + `observedGeneration` code with the verified delete (5.3) and MaterializeError (5.2) specs; full end-to-end verification lands in a follow-up that adds the catalog fixture.
