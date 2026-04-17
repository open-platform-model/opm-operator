## Why

The controller cannot render real OPM modules. Every reconcile of the hello test module
fails with `"no components field in release spec"` (2,656+ failures observed).

Root cause: the controller loads a raw `#Module` from OCI and passes it directly to the
render pipeline. A `#Module` defines `#components` (a CUE definition). The render pipeline
expects `components` (a concrete field). The catalog's `#ModuleRelease` schema materializes
`#components` into `components` via a for-comprehension — but the controller never builds a
`#ModuleRelease`. The CLI works because users write a `release.cue` that imports and
satisfies `#ModuleRelease`.

Separately, the current architecture couples the controller to Flux source-controller for
artifact delivery. CUE's native module system already speaks OCI and can resolve module
dependencies from a registry. The Flux source layer is redundant for CUE modules.

## What Changes

- Remove the `sourceRef` field from the ModuleRelease CR spec.
- Repurpose the `module` field to carry a CUE import path and pinned version.
- The controller synthesizes a short-lived `#ModuleRelease` CUE package at reconcile time,
  importing the target module via CUE's native module system.
- CUE resolves the module from the OCI registry — no Flux source-controller needed.
- Add `--registry` flag and `OPM_REGISTRY` env var for registry configuration.
  The controller sets `CUE_REGISTRY` from these at evaluation time.
- The catalog version used in the synthesized package is hardcoded in the controller binary.
- Version management is explicit: the CR pins a specific module version.
- Reconciliation triggers on CR change only (no source polling).
- Constitution Principle I ("Flux for Transport") is superseded for CUE module delivery.
  The `internal/source/` package is retained for potential future use.

## Capabilities

### New Capabilities

- `release-synthesis`: Controller synthesizes a `#ModuleRelease` CUE package from CR
  fields (name, namespace, module path, version, values), evaluates it via CUE's native
  module system, and feeds the result into the existing render pipeline.

- `registry-configuration`: Controller accepts `--registry` flag / `OPM_REGISTRY` env var
  to configure CUE module registry resolution.

### Modified Capabilities

- `module-reference`: The `module` field on ModuleRelease CR changes from a sub-path
  selector within a Flux source artifact to a CUE import path with pinned version.

### Removed Capabilities

- `source-reference`: The `sourceRef` field is removed. The controller no longer depends
  on Flux OCIRepository for CUE module acquisition.

## Impact

- **API change**: `sourceRef` removed, `module` field restructured. Breaking change for
  existing ModuleRelease CRs.
- **Go code changes**: New synthesis package, modified reconcile loop, registry config
  plumbing, updated CR types.
- **Dependencies**: No new Go dependencies. CUE's `load` and `cue/cuecontext` packages
  (already used) handle registry resolution.
- **Operational**: Controller requires OCI registry access. Flux source-controller is
  no longer required for CUE modules.
- **SemVer**: MINOR (v1alpha1 API — breaking changes are expected at this stability level).

## Scope Boundary

**In scope:**

- CR schema change (remove sourceRef, update module)
- Release package synthesis
- Registry configuration (flag + env var; ships with built-in default routing
  `opmodel.dev/*` and `testing.opmodel.dev/*` to `ghcr.io/open-platform-model`)
- Update render pipeline entry point
- Update reconcile loop
- Fix existing tests to use real module patterns
- Update e2e tests and samples
- Remove dead Flux source stub from `bundlerelease_controller` (import, RBAC
  markers, `sourcev1.OCIRepository{}` import-keeper). BundleRelease will not
  use Flux OCIRepository when implemented.

**Out of scope:**

- Private registry authentication (future)
- Semver range resolution / auto-upgrade (future)
- Removing `internal/source/` package (retained for future use)
- BundleRelease changes (separate change)
- Auto-secrets, policies in synthesized release (incremental addition)
