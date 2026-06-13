## Why

The render-core swap replaces the operator's pre-0001 fork with the library kernel. The kernel's render entry points (`SynthesizeRelease`, `Compile`) all take an already-loaded `*module.Module`, and the only module loader, `Kernel.LoadModulePackage`, requires a **local directory** — but a `ModuleRelease` references a module by **registry path + version** (`spec.module = {path, version}`). So before any kernel render call, the operator must acquire the target module from the registry as a `*module.Module`. This slice lands that acquisition step — the foundation the kernel-backed renderer (next slice) builds on — and does it without the legacy hardcoded catalog pin.

## What Changes

- New kernel-backed module acquisition (new `internal/moduleacquire` package): given `(path, version, registry)`, write a **minimal shim package** to a temp dir whose `cue.mod/module.cue` declares a single dependency on `<path>@<version>` and whose one `.cue` file imports and embeds that module at the package root, then `Kernel.LoadModulePackage(dir, {Registry}) → Kernel.NewModuleFromValue` → `*module.Module`. The temp dir is removed before return.
- **No catalog version pin.** Unlike `internal/synthesis` (which pins `CatalogVersion = "v1.3.4"` and imports the catalog into a synthesized release), the shim depends only on the target module. The `#Module`/`#ModuleRelease` schema resolves via the kernel's OCI schema cache (`opmodel.dev/core@v0`); catalog transformers arrive later via the materialized platform — neither belongs in module acquisition.
- The acquisition helper is **additive and not yet wired**: the existing render path (`internal/render` → `internal/synthesis` → `pkg/render`) is untouched and still drives every reconcile. The kernel-backed renderer that calls this helper, and the deletion of `internal/synthesis` (with its catalog pin), land in subsequent slices.

**Out of scope (later render-swap slices):** `SynthesizeRelease`/`Compile` calls; reading the materialized-platform store; the `core.Compiled → core.Resource` output adapter; gating releases on platform readiness; wiring any reconciler onto the kernel; deleting the fork (`pkg/render`, `pkg/module`, `pkg/loader`, `pkg/validate`, `internal/synthesis`, `internal/catalog`) and the legacy provider load.

## Capabilities

### New Capabilities

- `module-acquisition`: load a `ModuleRelease`'s target module from the OCI registry into a library `*module.Module` via a kernel-loaded shim package (single module dependency, no catalog pin), suitable as the `synth.ReleaseInput.Module` for a subsequent kernel render.

### Modified Capabilities

None — additive. The legacy `module-release-synthesis` capability (the temp-module-with-catalog-pin path) is unchanged here and is retired when its last caller is rewired in the renderer slice.

## Impact

- **Code**: new `internal/moduleacquire/` (helper + temp-dir shim writer); consumes the shared `*kernel.Kernel` from `cmd/main.go`. No change to existing render/reconcile code.
- **Dependencies**: uses `github.com/open-platform-model/library` (already required) — `kernel.LoadModulePackage`, `kernel.NewModuleFromValue`, `helper/loader/file.LoadOptions`, `module.Module`.
- **APIs/CRDs**: none.
- **Tests**: integration test loading a real published module from the registry (reuse `test-registry-lifecycle`) and asserting decoded `module.Module` metadata; unit test of the shim writer's emitted `cue.mod`/`.cue` contents.
- **Enhancement**: first slice of 0001 §5.2's render-core rewrite; provides the module-side input the kernel renderer consumes.
- **SemVer**: MINOR — additive internal package; no behavior change.
- **Complexity justification (Principle VII)**: a temp-dir shim is the minimum that bridges "registry module path" to the kernel's directory-based `LoadModulePackage`; it is strictly simpler than the replaced `internal/synthesis` (no catalog dep, no release scaffolding). A future library `LoadModuleFromRegistry(path, version)` could remove the shim entirely — noted as an upstream follow-up, not built here.
