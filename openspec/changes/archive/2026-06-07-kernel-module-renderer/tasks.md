## 1. Compiled → Resource adapter

- [x] 1.1 Add `core.ResourceFromCompiled(c *librarycore.Compiled) *core.Resource` in `pkg/core` (copy `Value`, `Release`, `Component`, `Transformer`)
- [x] 1.2 Unit test asserting the field copy

## 2. KernelModuleRenderer

- [x] 2.1 Add `ErrPlatformNotReady` sentinel in `internal/render`
- [x] 2.2 Create `internal/render/kernel_module_renderer.go`: `KernelModuleRenderer{Kernel *kernel.Kernel; Store *platform.Store; Registry string; RuntimeName string}` implementing `ModuleRenderer`
- [x] 2.3 `RenderModule`: `Store.Get()`; if absent return `ErrPlatformNotReady` (no further work)
- [x] 2.4 `moduleacquire.Acquire(ctx, k, modulePath, moduleVersion, registry)` → `*module.Module`
- [x] 2.5 Convert `*RawValues` → `cue.Value` via `k.CueContext().CompileBytes` (zero value when nil/empty); wrap compile errors
- [x] 2.6 `Kernel.SynthesizeRelease(synth.ReleaseInput{Module, Name, Namespace, Values})` then `Kernel.Compile(kernel.CompileInput{ModuleRelease, Platform: mp, RuntimeName})`
- [x] 2.7 Adapt `out.Compiled` → `[]*core.Resource` via `ResourceFromCompiled`; build inventory via existing `buildInventoryEntries`; return `RenderResult{Resources, InventoryEntries, Warnings: out.Warnings}`
- [x] 2.8 Ignore the `prov *provider.Provider` parameter (kept only to satisfy the unchanged interface)

## 3. Tests + validation gates

- [x] 3.1 Integration test (reuse `test-registry-lifecycle`; skip under `-short`/unreachable registry): seed a `*platform.Store` with a materialized platform, call `RenderModule` for a published module, assert resources + provenance + inventory entries
- [x] 3.2 Test: empty store → `RenderModule` returns `ErrPlatformNotReady`, no acquisition/compile attempted
- [x] 3.3 Confirm `cmd/main.go` is unchanged (still `RegistryRenderer`) — reconcile behavior unaffected
- [x] 3.4 `task dev:fmt dev:vet`
- [x] 3.5 `task dev:lint`
- [x] 3.6 `task dev:test`
