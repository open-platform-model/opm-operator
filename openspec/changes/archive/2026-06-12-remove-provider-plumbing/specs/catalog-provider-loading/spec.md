## REMOVED Requirements

### Requirement: Load provider from catalog directory

**Reason**: The controller no longer loads a transformer provider at startup. Transformers are resolved from the materialized platform's registry subscriptions (`Kernel.Materialize`), held in the platform store. `catalog.LoadProvider` and the `pkg/provider`/`pkg/loader`/`internal/catalog` packages are deleted.
**Migration**: See the `platform-reconciler` and `platform-gated-rendering` capabilities — the materialized `Platform` is the sole transformer source; apply a `Platform` CR named `cluster` instead of mounting a catalog directory.

### Requirement: Configurable catalog path

**Reason**: With the provider load deleted, the `--catalog-path` and `--provider-name` flags no longer have any effect and are removed.
**Migration**: Configure transformers via the `Platform` CR's `registry` subscriptions; configure the registry mapping via `--registry`/`OPM_REGISTRY` (unchanged).
