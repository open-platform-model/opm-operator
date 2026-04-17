## Why

Change 04 shipped the catalog as a filesystem directory copied into the container image. This only loads the `opm/v1alpha1` module's `#Registry`, which gives the base kubernetes provider with 17 transformers. The CLI composes a richer provider by unifying transformers from 5 catalog modules (opm, kubernetes, gateway_api, cert_manager, k8up — 54 transformers total) via CUE imports and the `&` operator. The catalog modules are now published to GHCR at `ghcr.io/open-platform-model`, making registry-based resolution viable. The controller needs the same composition capability to produce the full provider.

## What Changes

- Add a `catalog/` CUE composition module at the repo root with `config.cue` (imports + unifies all provider modules) and `cue.mod/module.cue` (pinned dependency versions).
- **BREAKING** (internal): Rewrite `internal/catalog/catalog.go` to load the composition package (`.`) and extract `providers` instead of loading `./providers` and extracting `#Registry`.
- Add `--registry` flag to `cmd/main.go` with default `opmodel.dev=ghcr.io/open-platform-model,registry.cue.works`.
- Add `--cue-cache-dir` flag to `cmd/main.go` with default `/tmp/cue-cache`.
- Set `CUE_REGISTRY`, `OPM_REGISTRY`, and `CUE_CACHE_DIR` environment variables at startup before loading the provider.
- Simplify Dockerfile: replace full catalog COPY with the small in-repo `catalog/` directory (~2 files).
- Update `.dockerignore` to allow `catalog/**`.
- Update tests and test fixtures to match the new composition-based loading.

## Capabilities

### New Capabilities

- `catalog-registry-resolution`: Load composed OPM provider from a CUE composition module that resolves catalog dependencies from an OCI registry (GHCR) at startup.

### Modified Capabilities

- `catalog-provider-loading`: The loading mechanism changes from filesystem-only to registry-based resolution. The `LoadProvider` function now requires registry configuration and loads from a composition package instead of a raw catalog directory.

## Impact

- `catalog/` (new, repo root): CUE composition module with pinned versions.
- `internal/catalog/catalog.go`: Rewrite loading logic for composition + registry.
- `internal/catalog/catalog_test.go` + `testdata/`: New test fixtures for composition loading.
- `cmd/main.go`: New `--registry` and `--cue-cache-dir` flags, env var setup.
- `Dockerfile`: Simplified COPY stage.
- `.dockerignore`: Allow CUE files.
- Depends on: change 04 (existing `internal/catalog` package, `Provider` field on reconciler).
- External dependency: public GHCR packages at `ghcr.io/open-platform-model`.
- SemVer: MINOR — new capability, internal breaking change to catalog loading.
