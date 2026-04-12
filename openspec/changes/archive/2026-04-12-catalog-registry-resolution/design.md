## Context

Change 04 implemented catalog provider loading from a filesystem directory shipped in the container image. It loads the `opm/v1alpha1` module's `providers` package and extracts `#Registry` — giving the base kubernetes provider (17 transformers). The CLI achieves a richer provider (54 transformers) by importing from 5 catalog modules and unifying their transformers via CUE's `&` operator in `config.cue`.

The catalog modules are now published as public OCI artifacts on GHCR (`ghcr.io/open-platform-model`). CUE's `load.Instances()` natively resolves registry imports when `CUE_REGISTRY` is set — no special Go API needed. This is the same mechanism the CLI uses.

The existing `internal/catalog` package, `--catalog-path` / `--provider-name` flags, and `Provider` field on `ModuleReleaseReconciler` (all from change 04) remain and are adapted.

## Goals / Non-Goals

**Goals:**

- Compose a full provider (54 transformers across 5 modules) using the same CUE unification pattern as the CLI.
- Resolve catalog module dependencies from GHCR at controller startup via `CUE_REGISTRY`.
- Ship a small CUE composition module in the repo (2 files) instead of the full catalog tree.
- Expose `--registry` and `--cue-cache-dir` flags for configuration.

**Non-Goals:**

- Hot-reloading the catalog without restarting the controller.
- Per-namespace or per-ModuleRelease provider overrides.
- Private registry authentication (packages are public).
- Caching modules across pod restarts (ephemeral `/tmp` cache is sufficient).

## Decisions

### 1. CUE composition module at `catalog/` repo root

A new `catalog/` directory at the repo root contains two files:
- `cue.mod/module.cue` — declares the composition module with pinned dependency versions.
- `config.cue` — imports all 5 provider modules and unifies their `#transformers` into a single `providers.kubernetes` value.

This mirrors the CLI's `config.cue` pattern. The composition happens in CUE (where `&` unification is native) rather than in Go.

**Alternative considered:** Go-side composition (load each module, merge transformer maps in Go). Rejected because it reimplements CUE unification semantics and is fragile.

### 2. Registry resolution via `CUE_REGISTRY` environment variable

`load.Instances()` reads `CUE_REGISTRY` to resolve module imports. The controller sets this at startup (before any goroutines) from the `--registry` flag. Both `CUE_REGISTRY` and `OPM_REGISTRY` are set for consistency with the CLI.

The `--registry` flag defaults to `opmodel.dev=ghcr.io/open-platform-model,registry.cue.works`. This routes `opmodel.dev/*` modules to GHCR and everything else (e.g., `cue.dev/x/k8s.io`) to the CUE central registry.

**Alternative considered:** `load.Config` field for registry. Not available — CUE's Go API only supports registry via env var.

### 3. `CUE_CACHE_DIR` set to `/tmp/cue-cache`

CUE caches downloaded modules in `$CUE_CACHE_DIR`. The distroless runtime image runs as user 65532 with no writable home directory. Setting `CUE_CACHE_DIR=/tmp/cue-cache` ensures a writable cache location.

The cache is ephemeral — lost on pod restart. This is acceptable because module fetch is startup-only (~2-5 seconds) and modules are small OCI artifacts.

### 4. `loadRegistry` rewritten to load composition package

The existing `loadRegistry` function changes:
- **Before:** `load.Instances([]string{"./providers"}, cfg)` → `LookupPath("#Registry")`
- **After:** `load.Instances([]string{"."}, cfg)` → `LookupPath("providers")`

The extraction of `map[string]cue.Value` from the result and the downstream call to `loader.LoadProvider()` remain unchanged.

### 5. `--catalog-path` default changes to `/catalog`

The in-repo `catalog/` directory is copied into the container at `/catalog`. For local dev, the default can be overridden to `./catalog`. The Dockerfile COPY stage becomes a single line for the small composition directory.

### 6. Dockerfile simplified and `.dockerignore` updated

The Dockerfile replaces the full catalog COPY with `COPY catalog/ /catalog/`. The `.dockerignore` adds `!catalog/**` to allow CUE files through the existing allowlist.

## Risks / Trade-offs

- **[Risk] Network dependency at startup** — The controller requires GHCR access to start. If GHCR is unreachable, startup fails. Mitigation: this is acceptable for v1alpha1; the controller already requires network for the K8s API. Packages are on GHCR (highly available).
- **[Risk] `os.Setenv` is process-global** — Setting `CUE_REGISTRY` via `os.Setenv` is not goroutine-safe. Mitigation: env vars are set in `main()` before `mgr.Start()`, before any goroutines are spawned. Same pattern the CLI uses.
- **[Trade-off] Cold start latency** — First startup fetches ~8 modules from GHCR/registry.cue.works. Estimated 2-5 seconds. Acceptable for a controller that runs continuously.
- **[Trade-off] Catalog version upgrade requires code change** — Pinned versions in `cue.mod/module.cue` require a commit + rebuild to update. This is intentional: the controller version pins the catalog version for reproducibility.
