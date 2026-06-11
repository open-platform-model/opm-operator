## REMOVED Requirements

### Requirement: Release synthesis

**Reason**: The temp-module synthesis mechanism (`internal/synthesis`: a `cue.mod/module.cue` declaring the target module + catalog dependency and a `release.cue` importing `#ModuleRelease`) is deleted, along with its hardcoded `CatalogVersion = "v1.3.4"` pin. Release construction is now performed by the library kernel's `SynthesizeRelease` over a module acquired by the `module-acquisition` step, and the catalog is resolved from the materialized platform rather than a synthesis-time pin.
**Migration**: See the `module-acquisition` capability (registry path → `*module.Module`, no catalog pin) and `kernel-module-renderer` (kernel `SynthesizeRelease` → `Compile`). The remaining `module-release-synthesis` requirements (CR spec shape, registry configuration, reconcile behavior, status reporting, BundleRelease, end-to-end scenarios) are unchanged.
