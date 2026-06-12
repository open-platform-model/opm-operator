## Context

Stage 5b of fork removal, now precisely scopeable because 5a deleted the old impls. The remaining `pkg/provider` surface (9 non-test importers) is entirely legacy provider wiring; the kernel renderers already ignore the `prov` argument. Confirmed sites:

- Interfaces (`internal/render/renderer.go`): `prov *provider.Provider` on `ModuleRenderer.RenderModule` and `ReleaseRenderer.Render`.
- Kernel renderers: `_ *provider.Provider` params they discard.
- `Provider` fields on `ModuleReleaseReconciler`/`ReleaseReconciler` and `ModuleReleaseParams`/`ReleaseParams`; call sites `params.Provider` at `reconcile/modulerelease.go:264` and `reconcile/release.go:366`; controllers thread `Provider: r.Provider` into params.
- `cmd/main.go`: `--catalog-path`/`--provider-name` flags, `catalog.LoadProvider` → `opmProvider`, two `Provider: opmProvider` fields. `--registry`/`OPM_REGISTRY` (line ~215) and `CUE_CACHE_DIR` (set from `--cue-cache-dir` at line 229) are process-wide and still used by the kernel's OCI resolution — they stay.
- Image: `Dockerfile:31 COPY catalog/ /catalog/`, `.dockerignore:14 !catalog/**`, and the repo-root `catalog/` composition module.
- `internal/catalog` (imported only by `main.go`) and `pkg/loader` (imported only by `internal/catalog`) become orphaned once the provider load goes.

## Goals / Non-Goals

**Goals:**

- Narrow the renderer interfaces and kernel renderers to drop the unused `prov` parameter.
- Remove `Provider` fields and the provider load; delete `pkg/provider`, `pkg/loader`, `internal/catalog`, and the `catalog/` composition module + its image plumbing.
- Keep the kernel's registry and CUE-cache configuration intact.

**Non-Goals:**

- BundleRelease; the upstream `MaterializeError.Transient` follow-up.
- Any change to rendering behavior (the provider is already ignored).

## Decisions

### One slice: the param removal and the deletions are coupled

**Decision:** Remove the `prov` param, the `Provider` fields, the provider load, and the three packages in a single change.

**Rationale:** `pkg/provider` cannot be deleted while the interface/renderer signatures and reconciler fields still reference it, and those references are meaningless without the provider load. Splitting would leave an intermediate state that still imports a package slated for deletion. The whole thing is mechanical (narrow a signature, drop a field, delete dead packages), so one cohesive "remove provider plumbing" slice is appropriate.

**Alternatives considered:** split signature-narrowing from package deletion — produces a compile-clean but pointless intermediate that still drags `pkg/provider`; no review benefit.

### Keep `--registry` and `--cue-cache-dir`

**Decision:** Remove only `--catalog-path`/`--provider-name`; keep `--registry`/`OPM_REGISTRY` and `--cue-cache-dir`/`CUE_CACHE_DIR`.

**Rationale:** The provider flags fed only `catalog.LoadProvider`. The registry mapping configures the Kernel (`WithRegistry`) and the catalog/core-schema OCI resolution; `CUE_CACHE_DIR` is the on-disk module cache the library uses for OCI pulls. Both remain load-bearing for the kernel path. The `catalog-registry-resolution` spec's registry requirement is reframed (configure the Kernel, not load a provider) rather than removed; its cue-cache requirement is unchanged.

**Alternatives considered:** drop `--cue-cache-dir` too — would remove the kernel's cache-dir control and risk uncached re-pulls; rejected.

### Spec: remove the provider-loading capability, reframe registry resolution, narrow the interface

**Decision:** REMOVE `catalog-provider-loading` (both requirements) and the composition-module + Dockerfile requirements of `catalog-registry-resolution`; MODIFY `catalog-registry-resolution`'s "Registry resolution at startup" to the kernel framing; MODIFY `module-renderer-interface`'s "ModuleRenderer interface" and "Renderer in reconcile params" to drop the provider.

**Rationale:** Keeps the spec honest — the provider load and composition module no longer exist, while registry/cache resolution and the renderer seam persist (narrowed). Startup reachability is already owned by `library-kernel-runtime`, so the reframed registry requirement cross-refs it rather than duplicating it.

## Risks / Trade-offs

- **Missed `prov`/`Provider` reference breaks the build** → mechanical; `task dev:vet`/`dev:test` is the backstop, plus a final grep for `pkg/provider`/`pkg/loader`/`internal/catalog`.
- **Removing `catalog/` breaks the image build if something still references `/catalog`** → remove the `Dockerfile` COPY and `.dockerignore` rule in the same slice; confirm no runtime reads `/catalog`.
- **`CUE_CACHE_DIR` was set just before the provider load** → ensure the `os.Setenv("CUE_CACHE_DIR", ...)` and `--cue-cache-dir` survive the provider-load removal and still execute before the first kernel call (verify ordering in `main.go`).

## Migration Plan

1. Narrow `ModuleRenderer`/`ReleaseRenderer` signatures (drop `prov`); update the two kernel renderers; drop the `pkg/provider` import from `internal/render`.
2. Remove `Provider` fields from the two controllers and the two params structs; drop the `params.Provider` args at the call sites; drop `pkg/provider` imports.
3. `cmd/main.go`: delete `--catalog-path`/`--provider-name`, the `catalog.LoadProvider` block and `opmProvider`, the two `Provider:` fields, and the `internal/catalog` import; keep `--registry` and the `CUE_CACHE_DIR` setup.
4. Delete `pkg/provider/`, `pkg/loader/`, `internal/catalog/`, and the repo-root `catalog/` module.
5. `Dockerfile`/`.dockerignore`: remove the `catalog/` COPY and allow-rule.
6. Verify: grep shows no `pkg/provider`/`pkg/loader`/`internal/catalog`/`prov *provider` references; `task dev:fmt dev:vet dev:lint dev:test`; build the image to confirm the `catalog/` removal is clean.

**Rollback:** revert the commit; the plumbing returns. No behavioral rollback concern (rendering never used the provider post-cut-over).

## Open Questions

- Confirm nothing at runtime reads the `/catalog` path or expects `--catalog-path`/`--provider-name` (e.g., deployment manifests, samples, e2e harness) — update any such references in this slice.
