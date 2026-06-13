## Context

`add-platform-crd` landed the cluster-scoped singleton `Platform` (group `releases.opmodel.dev/v1alpha1`, `spec.type` + `spec.registry` of `Subscription{Enable *bool, Filter{Range,Allow,Deny}}`, conditions + accessors). `wire-library-kernel` put one long-lived `*kernel.Kernel` in `cmd/main.go`. This slice connects them: a reconciler materializes the CR into a held platform.

Confirmed library surface:

- `Kernel.SynthesizePlatform(ctx, synth.PlatformInput) (*platform.Platform, error)` — defaults `PlatformInput.SchemaCache` to the Kernel's cache when nil.
- `Kernel.Materialize(ctx, *platform.Platform) (*materialize.MaterializedPlatform, error)` — performs registry I/O (version enumeration + OCI pulls); explicit, caller-driven, no kernel cache (§8.3 / D14).
- `oerrors.MaterializeError{Kind, Subscription, Version, Cause}` with `MaterializeKindCatalog` / `MaterializeKindCoreSchema`.
- `materialize/cache` offers an `LRU` + content-hash `Key(p)`; §8.3 deliberately does NOT use it — one global platform needs one slot, not an LRU.

Spec→input mapping is mechanical because the CRD was authored as a 1:1 projection: `Type→Type`, `Registry[path]{Enable,Filter}→Subscriptions[path]{Enable,Filter{Range,Allow,Deny}}`.

## Goals / Non-Goals

**Goals:**

- A `PlatformReconciler` that materializes the `cluster` CR and surfaces success / `MaterializeError` on its `Ready` condition + `observedGeneration`.
- A single-slot, generation-keyed, concurrency-safe store holding the current `*MaterializedPlatform`.
- Reconcile only `cluster`; clear the slot on delete.

**Non-Goals:**

- Any render path reading the store (next slice).
- Gating `ModuleRelease`/`Release` on platform readiness, `PlatformNotReady`/`NoPlatform` release conditions, freeze-don't-teardown release behavior (gating slice).
- Re-enqueueing releases on platform generation change — pointless until releases read the store; lands with the consumer slice.
- Render-core rewrite; BundleRelease.

## Decisions

### Hand-rolled single-slot store, not the library LRU

**Decision:** `internal/platform.Store` — `sync.RWMutex` + one `*MaterializedPlatform` + the `generation int64` it was built for. `Get() (*MaterializedPlatform, bool)`, `Set(gen int64, mp *MaterializedPlatform)`, `Clear()`.

**Rationale:** §8.3 — one global Platform per cluster; an LRU keyed on a content hash is overkill. Generation is the natural cache key (the CR's own version counter) and ties invalidation to `spec` changes for free. The held value is safe for concurrent read-only sharing (library v0.17 guarantee), so an `RWMutex` lets future readers run concurrently with reconciler writes.

**Alternatives considered:** library `materialize/cache.LRU` (extra dependency surface, content-hash key, capacity semantics — all unused for n=1, rejected); re-materialize per release (defeats materialize-once/share-many, rejected).

### Ready condition as the summary; reuse Flux helpers

**Decision:** Use the existing `internal/status` Flux condition helpers — `MarkReady` on success (reason `Materialized`), `MarkStalled`/`MarkNotReady` on `MaterializeError` (reason `MaterializeFailed`, message carries `Kind`/`Subscription`/`Version`). `Platform` satisfies `conditions.Setter` via its `Get/SetConditions`.

**Rationale:** Consistency with every other CRD's `Ready` printcolumn and the Flux conditions machinery already in the repo; no new status plumbing. A materialize failure is a semantic (not transient) error → `Stalled`, matching Principle V.

**Alternatives considered:** a bespoke `Materialized` condition type — redundant with `Ready` for a single-purpose resource; the §8.2 "Materialized=True" intent is captured by `Ready=True`/reason `Materialized`.

### Don't clobber last-good on failure

**Decision:** On `MaterializeError`, set status but leave the store slot unchanged.

**Rationale:** A transient registry blip shouldn't blank the platform the cluster is running on. Status reflects the failure; the held platform stays serviceable for future readers. Mirrors §8.4's freeze posture.

### Reconcile only `cluster`

**Decision:** Early-return for any object whose name ≠ `cluster`, even though the CRD's CEL rule already forbids other names.

**Rationale:** Defense-in-depth (§8.1); cheap and explicit. The watch can also use a name predicate, but the in-reconcile guard is the authority.

## Risks / Trade-offs

- **Materialize requires a reachable OCI registry; envtest has none by default** → the failure path (assert `MaterializeError` surfacing with an unresolvable subscription) is verified against a real registry (`opmodel.dev/core@v0` on ghcr) and needs no fixture. The success path needs a *resolvable* catalog subscription — i.e. a committed catalog fixture + publish harness, which does not yet exist in the tree (no `test-registry-lifecycle`). **Descoped:** the success spec is implemented but skip-guarded (parametrized via `OPM_TEST_CATALOG_PATH`); its end-to-end verification is deferred to a follow-up that adds the catalog fixture. The success branch reuses the store-write/`Ready=True`/`observedGeneration` code already exercised by the delete and failure specs.
- **Store written by reconciler, read by future render paths across goroutines** → `RWMutex` + the library's read-only-share guarantee; spec includes a race scenario (run tests with `-race`).
- **A reconciler whose output nothing yet reads looks inert** → intentional; the CR's status is the observable contract this slice delivers, and the store is consumed by the very next slice.
- **Generation key vs. status `observedGeneration` confusion** → both derive from `metadata.generation`; the store key and `observedGeneration` move together on a successful reconcile.

## Migration Plan

1. `internal/platform/store.go` (+ unit test, `-race`).
2. `internal/controller/platform_controller.go`: `Reconcile` + `SetupWithManager` (`For(&Platform{})`, generation-change predicate); `MaterializeFailed` reason in `internal/status`.
3. RBAC markers for `platforms` get/list/watch + `platforms/status` patch; `task dev:manifests`.
4. Register in `cmd/main.go` with the shared Kernel + store.
5. Envtest: success (registry fixture) + failure (`MaterializeError`) + delete-clears-slot.
6. Validation gates.

**Rollback:** revert the commit; the new controller and package are additive and unread by existing reconcilers, so removal restores prior behavior exactly.

## Open Questions

- Does materialize need a per-reconcile `context` timeout distinct from the reconcile context (OCI pulls can be slow)? Default: use the reconcile context; add a bounded timeout only if pulls prove to stall reconciles. Confirm during implementation.
