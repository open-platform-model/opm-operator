## Context

The render-core swap replaces the operator's fork with the library kernel behind the existing `ModuleRenderer`/`ReleaseRenderer` interfaces (the seam; they return `[]*core.Resource`). Reading the library surfaced the staging-critical facts:

- `Kernel.LoadModulePackage(ctx, dirPath, LoadOptions{Registry})` requires `dirPath` to be a **directory** (it errors otherwise) and applies the registry via `load.Config.Env`, not `os.Setenv`.
- `Kernel.NewModuleFromValue(v) (*module.Module, error)` decodes a loaded package value into a `*module.Module`.
- `Kernel.SynthesizeRelease(synth.ReleaseInput{Module, Name, Namespace, Values})` and `Kernel.Compile(...)` consume an already-loaded `*module.Module` — neither acquires it from a path.
- The reference flow (`flow_synth_integration_test.go`) loads the module from a **local dir** then `NewModuleFromValue` → `SynthesizeRelease` → `Match`/`Compile`.

The operator has no local module dir — only `spec.module = {path, version}` (a registry path). So a registry-path→`*module.Module` acquisition step is required, and it has no kernel equivalent. This slice lands exactly that, mirroring the shim the legacy `internal/synthesis` already uses — minus the catalog pin.

## Goals / Non-Goals

**Goals:**

- A kernel-backed `internal/moduleacquire` that turns `(path, version, registry)` into a `*module.Module`, cleaning up its temp dir.
- No catalog dependency or version pin in the acquisition shim.
- Concurrency-safe (per-call registry, no env mutation).

**Non-Goals:**

- Wiring acquisition into any reconciler/renderer (renderer slice).
- `SynthesizeRelease`/`Compile`, store reads, the `Compiled→Resource` adapter, platform-readiness gating (later slices).
- Deleting `internal/synthesis` and its `CatalogVersion` pin — happens when its last caller is rewired.

## Decisions

### Why staging by CR-kind renderer, not by pipeline phase

**Decision:** Stage the swap as: (1) foundations (this slice — acquisition; plus a small `Compiled→Resource` adapter slice), (2) a complete `KernelModuleRenderer` behind `ModuleRenderer` for `ModuleRelease`, (3) the same for `Release`, (4) delete the fork + legacy provider load.

**Rationale:** A per-pipeline-phase split (swap synthesis, then match, then execute) does not separate cleanly: the fork's `pkg/module.Release` and the kernel's `*module.Release` are different types threaded through every phase, so a half-swapped pipeline needs throwaway adapters between fork and kernel types. The `ModuleRenderer` interface is the real seam — replacing one implementation at a time keeps the reconcile loop, apply, inventory, and status (all consuming `[]*core.Resource`) untouched and each stage independently shippable.

**Alternatives considered:** big-bang full swap (trips the small-batch gate and is unreviewable); per-pipeline-phase (throwaway cross-type adapters — rejected).

### Temp-dir shim for acquisition, mirroring the legacy pattern minus the catalog

**Decision:** Write `cue.mod/module.cue` with one dep (`<path>@<version>`) and a one-line package file that imports and embeds the module at the root, then `LoadModulePackage` + `NewModuleFromValue`.

**Rationale:** `LoadModulePackage` needs a directory; the published module is a CUE package reachable only by import. The legacy `internal/synthesis` already proves this shape works for module resolution — this slice keeps the module-import half and drops the `#ModuleRelease` scaffolding (now `SynthesizeRelease`'s job) and the `CatalogVersion`/catalog dep (now the materialized platform's job, resolved later).

**Alternatives considered:** extend the library with `LoadModuleFromRegistry(path, version)` to skip the shim — the cleanest end state, but a library change out of this slice's scope; recorded as an upstream follow-up. Loading raw OCI bytes via `loader/bytes` — that package is a skeleton with no exported funcs.

### New package, additive — don't touch the live path

**Decision:** Put acquisition in a new `internal/moduleacquire` package; leave `internal/synthesis` and the existing render path running.

**Rationale:** Keeps this slice inert (no behavior change), so it lands and is tested in isolation. The old synthesis (and its catalog pin) is deleted only when the renderer slice removes its last caller — sequencing the pin's death with the code that replaces it, not orphaning it early.

## Risks / Trade-offs

- **Acquisition does real registry I/O; envtest has no registry** → integration test uses `test-registry-lifecycle` with a published module fixture; gate it the way the library gates its flow tests (skip under `-short`/unreachable registry).
- **Shim CUE language version drift** → emit the language version the operator's CUE toolchain expects (v0.17 line), or omit it and let the loader default; confirm during implementation against a real load.
- **A foundation slice nothing calls looks inert** → intentional and consistent with prior slices; the renderer slice consumes it next, and acquisition is unit- and integration-testable on its own.

## Migration Plan

1. `internal/moduleacquire/acquire.go`: shim writer + `Acquire(ctx, k, path, version, registry) (*module.Module, error)` with `defer os.RemoveAll`.
2. Unit test: assert emitted `cue.mod`/`.cue` content (single dep, no catalog, import+embed).
3. Integration test (registry fixture): acquire a published module, assert metadata; assert unresolvable path errors; assert no temp dir leaks.
4. Validation gates.

**Rollback:** revert the commit; the package is additive and uncalled, so removal is a no-op for existing behavior.

## Open Questions

- Exact CUE `language.version` value for the shim under the v0.17 toolchain — pin vs. omit. Resolve by loading a real module during implementation.
