# Proposal: fix-moduleacquire-core-v0

## Why

`internal/moduleacquire` cannot load **any** `opmodel.dev/core@v0` `#Module` from the registry. It bridges "module published by `path@version`" to the kernel's directory-only loader by writing a synthetic *wrapper* CUE package that depends on the target module and pulls it in (`import mod "…/hello@v0"`, then references `mod`). The wrapper is broken three ways — all artifacts of wrapping, not of the module itself (a plain `cue eval .` in the module's own directory renders it correctly):

1. **Self-reference collapse.** Embedding the module at the wrapper root re-evaluates `#Module`; core@v0's self-referential metadata (`modulePath: metadata.modulePath`, `version: metadata.version`) collapses to bottom and the author-set values are rejected:

   ```
   metadata.modulePath: field not allowed: …/core@v0.4.0/module.cue:14:3
   metadata.version:    field not allowed: …/core@v0.4.0/module.cue:15:3
   ```

2. **Missing transitive deps.** The wrapper's `cue.mod/module.cue` lists only the direct dependency, so the kernel's programmatic loader (`load.Instances`) cannot resolve the target's catalog/core imports: `cannot find package opmodel.dev/catalogs/opm/resources`. (The `cue` CLI auto-resolves these, which masked the problem during the original design.)

3. **Root shape-gate mismatch.** Binding the module off-root to dodge (1) leaves the wrapper root without `kind`/`metadata`, so the loader's shape gate fails: `expected kind "Module", found no kind field`.

Impact: `moduleacquire.Acquire` (and therefore the `ModuleRelease` render path via `KernelModuleRenderer.RenderModule`) fails for **every** real core@v0 module. Two integration specs fail because of it — `acquire_test.go` "acquires the module and decodes its metadata" and `kernel_module_renderer_test.go` "renders the fixture module's resources with provenance and inventory" — and `task dev:test:local` cannot go green. It was masked until the `modernize-test-fixtures` change replaced the old fixture (which failed earlier with a parse error) with a correct kernel-era one.

A spike (`experiments/moduleacquire-spike/`) confirmed the root cause and the fix: the wrapper technique is wrong; loading the module **as itself** — fetched from the registry, its own `cue.mod/module.cue` present, `kind`/`metadata` at the root — makes all three failures disappear. That capability does not belong in the operator: per the library's **Principle V** (CUE-native module resolution), custom OCI fetch + dependency resolution live in the library kernel, not downstream impls. So this change is now contingent on the library change **`add-registry-module-loader`**, which adds `Kernel.LoadModuleFromRegistry`.

## What Changes

- **Depend on the library change `add-registry-module-loader`** (adds `Kernel.LoadModuleFromRegistry(ctx, modPath, version) (cue.Value, error)` — CUE-native fetch + in-memory main-module load + shape gate). Bump the library dependency to the version that ships it.
- **Delete the wrapper shim.** Remove `internal/moduleacquire/shim.go` (`writeShim`, `packageTmpl`, the temp-dir + module-file scaffolding) and `shim_test.go`. The whole bug class — re-embedding, hand-maintained transitive deps, off-root binding — is removed, not patched.
- **Rewrite `internal/moduleacquire/acquire.go`** to call `k.LoadModuleFromRegistry(ctx, path, version)` then `k.NewModuleFromValue(...)`. Acquisition keeps its public signature (`Acquire(ctx, k, path, version, registry) (*module.Module, error)`) and its no-process-env-mutation / safe-for-concurrent-reconcilers guarantee (now the library's, via the kernel's configured registry). There is no temp directory to create or clean up.
- **Unskip/confirm** the registry-backed acquire and renderer happy-path specs pass against the local registry, and add an explicit `metadata.modulePath` assertion (the field that regressed).
- **Rewrite `test/fixtures/releases/hello`** to the kernel-era core@v0 release-package shape (deferred here from `modernize-test-fixtures`), contingent on the Release-path question (design D3).

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `module-acquisition`: acquisition MUST load the target module via the library kernel's registry module loader (`Kernel.LoadModuleFromRegistry`) rather than synthesizing a wrapper package. A registry-published core@v0 `#Module` decodes to `*module.Module` with its author-set `metadata.modulePath`/`version` intact and its transitive catalog/core deps resolved; the per-call-registry and no-process-env-mutation guarantees are preserved (now provided by the library). The temp-dir/no-leak guarantee is moot — acquisition no longer writes a temp directory.

> The `releases/hello` rewrite + Release happy-path verification remains a tasks-level deliverable, not a requirements change, and is contingent on design D3 (does the kernel's `LoadReleasePackage` → `Compile` consume a hand-authored real-module release package?). If it needs a kernel-side accommodation, it splits out and only the acquisition fix lands here.

## Impact

- **Library dependency**: requires `add-registry-module-loader` shipped; bump the pin in `go.mod` (and update any vendored references).
- **Operator Go code**: `internal/moduleacquire/shim.go` + `shim_test.go` deleted; `acquire.go` rewritten to a thin delegation to the kernel.
- **Tests**: the two failing integration specs become green against the local registry; the regression now asserts a **real** core@v0 module decodes with correct `metadata.modulePath`. Release-path coverage as below.
- **Fixtures**: `test/fixtures/releases/hello` rewritten to core@v0.
- **SemVer**: PATCH for the operator (bug fix), gated on bumping the library dependency.
- **Unblocks**: `task dev:test:local` fully green; the `modernize-test-fixtures` change's deferred acceptance.
- **Reference**: library change `add-registry-module-loader`; the A/B spike `experiments/moduleacquire-spike/`; `library/opm/helper/synth/release.go` (the related self-reference workaround on the release path); `modernize-test-fixtures` design D5/D6.
