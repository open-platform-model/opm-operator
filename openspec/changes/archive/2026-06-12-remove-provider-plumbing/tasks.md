## 1. Narrow the renderer interfaces

- [x] 1.1 `internal/render/renderer.go`: drop `prov *provider.Provider` from `ModuleRenderer.RenderModule` and `ReleaseRenderer.Render`; remove the `pkg/provider` import
- [x] 1.2 `internal/render/kernel_module_renderer.go`: drop the `_ *provider.Provider` param; remove the import; update the doc comment
- [x] 1.3 `internal/render/kernel_release_renderer.go`: drop the `_ *provider.Provider` param; remove the import; update the doc comment

## 2. Remove Provider fields + call sites

- [x] 2.1 `internal/reconcile/modulerelease.go`: remove `Provider` from `ModuleReleaseParams`; drop the `params.Provider` arg in the `RenderModule` call; remove the `pkg/provider` import
- [x] 2.2 `internal/reconcile/release.go`: remove `Provider` from `ReleaseParams`; drop the `params.Provider` arg in the `Render` call; remove the import
- [x] 2.3 `internal/controller/modulerelease_controller.go`: remove the `Provider` field and the `Provider: r.Provider` params wiring; remove the import
- [x] 2.4 `internal/controller/release_controller.go`: remove the `Provider` field and `Provider: r.Provider`; remove the import

## 3. main.go

- [x] 3.1 Remove the `--catalog-path` and `--provider-name` flags and their vars
- [x] 3.2 Remove the `catalog.LoadProvider` block, `opmProvider`, the two `Provider: opmProvider` reconciler fields, and the `internal/catalog` import
- [x] 3.3 Keep `--registry`/`OPM_REGISTRY` and the `CUE_CACHE_DIR` setup (`--cue-cache-dir`); confirm `CUE_CACHE_DIR` is still set before the first kernel call

## 4. Delete packages + composition module

- [x] 4.1 Delete `pkg/provider/`, `pkg/loader/`, `internal/catalog/`
- [x] 4.2 Delete the repo-root `catalog/` composition module
- [x] 4.3 `Dockerfile`: remove `COPY catalog/ /catalog/` (and its comment); `.dockerignore`: remove the `!catalog/**` allow-rule

## 5. Verify + validation gates

- [x] 5.1 `grep -rn "opm-operator/pkg/provider\|opm-operator/pkg/loader\|opm-operator/internal/catalog\|prov \*provider\|catalog-path\|provider-name" --include=*.go .` returns no hits
- [x] 5.2 Confirm no manifest/sample/e2e reference to `/catalog` or the removed flags remains (per design open question)
- [x] 5.3 `task dev:fmt dev:vet`
- [x] 5.4 `task dev:lint`
- [x] 5.5 `task dev:test`
- [x] 5.6 Build the container image to confirm the `catalog/` removal is clean
