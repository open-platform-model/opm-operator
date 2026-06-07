## Context

This slice is stage 2 of the render-core swap: a complete kernel-backed renderer for `ModuleRelease`, behind the existing `ModuleRenderer` seam. It consumes the two foundation slices — `internal/moduleacquire.Acquire` (registry path → `*module.Module`) and `internal/platform.Store` (the materialized platform held by the PlatformReconciler).

Confirmed operator seam and library surface:

- `ModuleRenderer.RenderModule(ctx, name, namespace, modulePath, moduleVersion string, values *RawValues, prov *provider.Provider) (*RenderResult, error)` — the interface stays unchanged this slice; `KernelModuleRenderer` ignores `prov`.
- `RenderResult{Resources []*core.Resource, InventoryEntries []InventoryEntry, Warnings []string}` — unchanged; reused.
- Current values handling: `*RawValues.Raw` (JSON bytes) → `cueCtx.CompileBytes` → `cue.Value`; no values → fall back to module `#config` defaults. The kernel path mirrors this: supply the compiled value, or the zero `cue.Value` (the library's `SynthesizeRelease`/`ProcessModuleRelease` then rely on `#config` defaults for concreteness).
- Library flow (`flow_synth_integration_test.go`): `SynthesizeRelease(ReleaseInput{Module, Name, Namespace, Values})` → `Compile(CompileInput{ModuleRelease, Platform: mp, RuntimeName})` → `CompileResult.Compiled []*core.Compiled`.
- `core.Compiled{Value, Release, Component, Transformer}` and operator `pkg/core.Resource{Value, Release, Component, Transformer}` have identical fields → the adapter is a field copy.
- `buildInventoryEntries` (→ `r.ToUnstructured()` → `inventory.NewEntryFromResource`) is renderer-agnostic and is reused verbatim.

## Goals / Non-Goals

**Goals:**

- A `KernelModuleRenderer` that fully implements `ModuleRenderer` via the kernel and returns the same `RenderResult` shape the reconcile loop already consumes.
- A trivial `core.ResourceFromCompiled` adapter.
- `ErrPlatformNotReady` when the store is empty.
- Built and proven end-to-end by tests, without changing any wiring.

**Non-Goals:**

- Wiring it into the reconciler or removing `RegistryRenderer` (next slice).
- Mapping `ErrPlatformNotReady` to a CR condition / release-gating behavior (next slice).
- `Release` CR renderer; fork deletion; provider-type removal; BundleRelease.

## Decisions

### Implement the existing interface unchanged; ignore `prov`

**Decision:** `KernelModuleRenderer` satisfies the current `ModuleRenderer` signature, including the `*provider.Provider` parameter, which it ignores. The platform comes from the injected store, not the parameter.

**Rationale:** Keeps the seam stable so the next slice can swap implementations in `cmd/main.go` with a one-line change and no interface churn. The `prov` parameter and `pkg/provider` are deleted later, in the fork-removal slice, when no implementation reads them — sequencing the signature change with the code that makes it possible.

**Alternatives considered:** change the interface now to drop `prov` — would force touching the reconciler call site, the other renderer, and tests in this slice, defeating "build but not wired."

### Read the platform from the store; gate with a typed error

**Decision:** `RenderModule` calls `Store.Get()`; on absence it returns `ErrPlatformNotReady` before doing any I/O.

**Rationale:** The store is the materialize-once/share-read-only source (the library guarantees concurrent read safety). A typed error lets the next slice map it cleanly to a `PlatformNotReady` condition without string matching. Failing before acquisition avoids pointless registry I/O when nothing can be rendered anyway.

**Alternatives considered:** materialize on demand inside the renderer — violates §8.3 (materialize is the PlatformReconciler's job, cached once per generation); re-materializing per render defeats the model.

### Fold the adapter in here

**Decision:** Add `core.ResourceFromCompiled` in this slice rather than a standalone change.

**Rationale:** It is a four-field copy with no behavior; a standalone OpenSpec change for it would be ceremony around ~10 lines. It is consumed here and nowhere else yet.

**Alternatives considered:** separate adapter slice — rejected as disproportionate.

### Runtime identity

**Decision:** Pass the controller's existing runtime identity constant as `CompileInput.RuntimeName` (the value the legacy path passes today as the render runtime name).

**Rationale:** Preserves the runtime-name semantics transformers already see; no new concept introduced.

## Risks / Trade-offs

- **Two render paths coexist after this slice** (legacy `RegistryRenderer` live, `KernelModuleRenderer` built-but-unused) → intentional and temporary; the next slice flips wiring. Tests exercise the new path directly so it is not dead-untested.
- **Integration test needs a registry and a materialized platform** → reuse `test-registry-lifecycle` with a published module fixture; construct a `*platform.Store` seeded via a real `Materialize` (or a small published platform fixture). Gate under `-short`/unreachable registry like the library flow tests.
- **Values concreteness** (zero `cue.Value` when the module has non-defaulted `#config`) → surfaces as a `SynthesizeRelease` error, same failure mode as the legacy path; tests cover the defaults-only happy path and document the requires-values case.

## Migration Plan

1. `pkg/core/compiled_adapter.go`: `ResourceFromCompiled(*librarycore.Compiled) *core.Resource` + unit test.
2. `internal/render/kernel_module_renderer.go`: `KernelModuleRenderer{Kernel, Store, Registry, RuntimeName}` implementing `RenderModule` (store read → acquire → values → `SynthesizeRelease` → `Compile` → adapt → `buildInventoryEntries`).
3. `ErrPlatformNotReady` sentinel in `internal/render`.
4. Integration test: seeded store + published module → expected resources + inventory; empty store → `ErrPlatformNotReady`.
5. Validation gates.

**Rollback:** revert the commit; nothing references the new renderer outside tests, so removal is a no-op for the running operator.

## Open Questions

- How the test seeds the store with a materialized platform: a real `Kernel.SynthesizePlatform`+`Materialize` against a published platform fixture, vs. a minimal hand-built `*MaterializedPlatform`. Prefer the real path for fidelity; confirm a suitable published platform fixture exists during implementation.
