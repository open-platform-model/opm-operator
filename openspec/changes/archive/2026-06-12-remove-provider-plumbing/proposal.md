## Why

Stage 5a deleted the dead render fork; the only fork residue left is the legacy **provider plumbing** — the `*provider.Provider` that the kernel renderers already ignore, the `Provider` fields the reconcilers still carry, and the startup `catalog.LoadProvider` that loads a CUE composition module from `--catalog-path`. None of it affects rendering anymore (the materialized platform drives everything), so it is pure dead weight threaded through ~9 files plus `main.go` and the image build. This slice (5b) removes it: the `prov` parameter and `Provider` fields, the provider load and its flags, the `catalog/` composition module, and the `pkg/provider`/`pkg/loader`/`internal/catalog` packages. After it, the operator carries no trace of the pre-0001 fork.

## What Changes

- **Drop the `prov` parameter** from the `ModuleRenderer.RenderModule` and `ReleaseRenderer.Render` interfaces (`internal/render/renderer.go`) and from the kernel renderers that ignore it (`kernel_module_renderer.go`, `kernel_release_renderer.go`).
- **Remove the `Provider` field** from `ModuleReleaseReconciler`/`ReleaseReconciler` (`internal/controller`), from `ModuleReleaseParams`/`ReleaseParams` (`internal/reconcile`), and the call sites that pass `params.Provider` into `RenderModule`/`Render`.
- **Remove the provider load from `cmd/main.go`**: the `catalog.LoadProvider` call, the `opmProvider` value, the two `Provider: opmProvider` reconciler fields, and the `--catalog-path` / `--provider-name` flags. **Keep** `--registry`/`OPM_REGISTRY` and `--cue-cache-dir`/`CUE_CACHE_DIR` — the kernel still needs both for OCI module/catalog resolution.
- **Delete the packages** `pkg/provider`, `pkg/loader`, and `internal/catalog` (now imported only by each other and the provider load).
- **Delete the `catalog/` composition module** at the repo root and its image plumbing: the `COPY catalog/ /catalog/` in `Dockerfile` and the `!catalog/**` allow-rule in `.dockerignore`.

**Out of scope:** BundleRelease (stage 6); the optional upstream `MaterializeError.Transient` library follow-up.

## Capabilities

### Removed Capabilities

- `catalog-provider-loading`: the startup load of a provider from a catalog composition directory (`--catalog-path`/`--provider-name` → `catalog.LoadProvider`) is deleted. Transformers now come from the materialized platform (`platform-reconciler`), not a startup-loaded provider.

### Modified Capabilities

- `catalog-registry-resolution`: the CUE composition module and its Dockerfile copy are removed (no composed provider exists); the startup registry-resolution requirement is reframed to configure the library Kernel (`kernel.WithRegistry`) rather than load a composed provider; the `--cue-cache-dir`/`CUE_CACHE_DIR` requirement is unchanged (the kernel still uses it).
- `module-renderer-interface`: the `ModuleRenderer`/`ReleaseRenderer` method signatures drop the `prov *provider.Provider` parameter; the reconcile-params requirement no longer passes a provider. The interfaces, the `Renderer` fields, and the injected-renderer contract are otherwise unchanged.

## Impact

- **Code (edits)**: `internal/render/{renderer,kernel_module_renderer,kernel_release_renderer}.go`; `internal/controller/{modulerelease,release}_controller.go`; `internal/reconcile/{modulerelease,release}.go`; `cmd/main.go`; `Dockerfile`; `.dockerignore`.
- **Code (deletions)**: `pkg/provider/`, `pkg/loader/`, `internal/catalog/`, and the repo-root `catalog/` composition module.
- **Preserved**: `--registry`/`OPM_REGISTRY`, `--cue-cache-dir`/`CUE_CACHE_DIR`, the renderer interfaces + `Renderer` fields, `pkg/core`, `pkg/resourceorder`.
- **APIs/CRDs**: none.
- **Enhancement**: stage 5b — completes 0001's fork removal; the operator now consumes only the library kernel + the materialized platform.
- **SemVer**: PATCH/MINOR — removal of unused plumbing; no behavioral change to rendering (the kernel renderers already ignore the provider).
- **Complexity justification (Principle VII)**: this is deletion + a mechanical signature narrowing; it removes surface rather than adding it. Kept as one slice because the param removal and the package deletions are coupled (the packages cannot go while the param references them).
