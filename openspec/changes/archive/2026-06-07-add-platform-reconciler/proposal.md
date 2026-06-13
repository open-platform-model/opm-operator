## Why

The `Platform` CRD exists (`add-platform-crd`) but nothing acts on it — applying a `Platform` is inert. Enhancement 0001 §8.2/§8.3 calls for a reconciler that turns the applied `Platform` CR into a sealed, materialized platform the render path can later consume: build `synth.PlatformInput` from the spec, `SynthesizePlatform → Materialize`, and hold the result in a single cache slot keyed on the CR's generation, surfacing success/failure on the CR's status. This slice lands that reconciler and the holder — establishing the materialized-platform source the render-core rewrite reads next.

## What Changes

- New **`internal/platform` store** — a single-slot, RWMutex-guarded holder of the current `*materialize.MaterializedPlatform`, keyed on the Platform CR's `.metadata.generation` (§8.3 — the library `materialize/cache` LRU is overkill for one platform). Constructed once in `cmd/main.go`, injected into the reconciler. The held `*MaterializedPlatform` is safe for concurrent read-only sharing (the library's v0.17 guarantee), so future readers take a read lock.
- New **`PlatformReconciler`** (`internal/controller`) holding the shared `*kernel.Kernel` and the store. On reconcile of `cluster`:
  - Map `PlatformSpec` → `synth.PlatformInput` (`Type`, `Subscriptions` from `Registry`; `SchemaCache` left nil so the Kernel defaults it; `Name`/labels/annotations from object meta).
  - `Kernel.SynthesizePlatform` → `Kernel.Materialize`.
  - On success: store the materialized platform under the generation key; set `Ready=True` (reason `Materialized`), `observedGeneration`.
  - On `*oerrors.MaterializeError`: set `Ready=False` (reason `MaterializeFailed`) with the error's `Kind`/`Subscription`/`Version` in the message; do not overwrite a previously-good slot.
  - Reconcile **only** the object named `cluster` (defense-in-depth alongside the CRD's CEL singleton).
  - On delete: clear the store slot (workloads are untouched — §8.4 freeze-don't-teardown applies to releases, addressed in the gating slice).
- Register `PlatformReconciler` in `cmd/main.go` with the shared Kernel + store, watching `Platform`.
- `Materialized` summarized via the existing Flux `Ready` condition + `internal/status` helpers; add a `MaterializeFailed` reason constant.

**Out of scope (later slices):** the render path reading the store; gating `ModuleRelease`/`Release` on platform readiness (`PlatformNotReady`/`NoPlatform`); re-enqueueing releases on platform generation change (meaningless until releases read the store); the render-core rewrite; BundleRelease. No release-reconcile behavior changes here.

## Capabilities

### New Capabilities

- `platform-reconciler`: reconciles the singleton `Platform` CR into a materialized platform held in a generation-keyed single-slot store, via `SynthesizePlatform → Materialize`, surfacing materialize success/failure on the CR's `Ready` condition and `observedGeneration`, reconciling only `cluster`, and clearing the slot on delete.

### Modified Capabilities

None — additive. Existing release/module reconcilers are untouched this slice; they do not yet read the store.

## Impact

- **Code**: new `internal/platform/store.go`; new `internal/controller/platform_controller.go`; `cmd/main.go` (construct store, register reconciler); a `MaterializeFailed` reason in `internal/status`.
- **APIs/CRDs**: none (uses the `Platform` kind from `add-platform-crd`); new RBAC for the controller to get/list/watch `platforms` and patch `platforms/status`.
- **Controllers**: one new controller registered (`platform`). No change to `modulerelease`/`release`/`bundlerelease`.
- **Tests**: envtest exercising `MaterializeError` surfacing (verified against a real registry), delete-clears-slot, and non-cluster-ignored. The materialize **success** path needs a committed catalog fixture that does not yet exist; its spec is implemented but skip-guarded, with end-to-end verification deferred to a follow-up (see design Risks).
- **Enhancement**: implements 0001 §8.2/§8.3; provides the materialized-platform source the render-core rewrite consumes next.
- **SemVer**: MINOR — additive controller + internal package; no existing behavior changes.
- **Complexity justification (Principle VII)**: a hand-rolled single-slot store (vs. the library LRU) is the minimum for one global platform per §8.3; the store is consumed by the very next slice, not speculative.
