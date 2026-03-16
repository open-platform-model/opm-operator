## Context

The CLI loads providers from `~/.opm/config.cue` — a user-managed CUE file that imports provider packages from the OPM catalog via the CUE registry. This approach has multiple problems for a Kubernetes controller:

1. **Security** — if providers came from the module artifact, the module author controls the transform logic that produces Kubernetes resources in the cluster.
2. **`CUE_REGISTRY` env mutation** — the CLI sets `os.Setenv("CUE_REGISTRY", ...)` which is not goroutine-safe for concurrent reconciliations.
3. **Filesystem assumptions** — the CLI expects `~/.opm/config.cue` with a `cue.mod/` sibling directory.

The OPM catalog (`opmodel.dev@v1`) contains a `providers/` package with a `#Registry` that maps provider names to provider definitions. The catalog's only external dependency is `cue.dev/x/k8s.io@v0` (Kubernetes API types). Both the catalog and module artifacts are `v1`; compatibility is enforced during catalog publishing.

## Goals / Non-Goals

**Goals:**

- Load the OPM catalog from a directory on the container filesystem.
- Extract the provider `#Registry` from the catalog CUE value.
- Convert the registry into a `map[string]cue.Value` and call `pkg/loader.LoadProvider` to produce a `*provider.Provider`.
- Expose the loaded provider for injection into the rendering bridge (change 05).
- Add a `--catalog-path` flag to `cmd/main.go` with a sensible default.
- Update the Dockerfile to copy the catalog into the container image.

**Non-Goals:**

- Multiple provider versions per cluster (future enhancement).
- Per-namespace provider overrides (future enhancement).
- Loading providers from ConfigMaps or Secrets.
- Hot-reloading the catalog without restarting the controller.

## Decisions

### 1. Catalog shipped in the container image

The full `opmodel.dev@v1` catalog is copied into the container image at `/etc/opm/catalog/v1alpha1/`. This includes `providers/`, `core/`, `schemas/`, `resources/`, `traits/`, and `cue.mod/` (with vendored `cue.dev/x/k8s.io` in `cue.mod/pkg/`). The catalog directory is a complete, self-contained CUE module — no registry access or network calls needed at runtime.

### 2. Provider loaded once at startup

The catalog is loaded via `cue/load.Instances()` during controller startup (in `cmd/main.go`), before the manager starts. This produces a single `*provider.Provider` that is injected into the reconciler struct. Loading at startup avoids per-reconcile CUE evaluation overhead and sidesteps `cue.Context` goroutine-safety concerns for provider loading.

**Alternative considered:** Loading per-reconcile. Rejected because CUE evaluation is expensive and the provider is static for the lifetime of the controller process.

### 3. Load via the providers `#Registry`

The catalog's `providers/registry.cue` defines `#Registry: {"kubernetes": k8s.#Provider}`. The loader evaluates the catalog's `providers` package, extracts `#Registry` fields as a `map[string]cue.Value`, and passes them to the existing `pkg/loader.LoadProvider("kubernetes", registry)`. This reuses the CLI's provider loading code without modification.

### 4. New `internal/catalog` package

A new `internal/catalog` package encapsulates catalog loading. The public API is:

```go
func LoadProvider(catalogDir string, providerName string) (*provider.Provider, error)
```

This hides CUE loading details from `cmd/main.go` and the reconciler. The package creates its own `cue.Context` internally.

### 5. `--catalog-path` flag with default

A new flag `--catalog-path` (default: `/etc/opm/catalog/v1alpha1`) is added to `cmd/main.go`. This allows overriding the catalog location for development (e.g., pointing to a local catalog checkout) or testing.

## Risks / Trade-offs

- **[Risk] CUE load needs vendored dependencies** — `cue/load.Instances()` on the catalog directory will fail if `cue.dev/x/k8s.io` is not vendored in `cue.mod/pkg/`. Mitigation: the Dockerfile must ensure the vendored dependency is present. Add a build-time check.
- **[Risk] Catalog version mismatch** — If the catalog version in the image doesn't match the module artifact's expected `opmodel.dev@v1` schemas, rendering may produce unexpected results. Mitigation: both are `v1`; compatibility is enforced during catalog publishing. The controller version pins the catalog version.
- **[Trade-off] Provider reload requires pod restart** — Loading at startup means catalog updates require a controller restart. This is acceptable for v1alpha1; hot-reload can be added later if needed.
- **[Trade-off] Full catalog in image** — The entire catalog (~100+ CUE files) is shipped in the image even though only the provider package is loaded. This is simpler than cherry-picking files and ensures all transitive dependencies are present.
