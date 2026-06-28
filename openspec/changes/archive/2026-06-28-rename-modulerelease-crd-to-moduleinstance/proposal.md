## Why

OPM's deployable artifact is being renamed from the `Release` vocabulary family to `Instance` across the whole stack (enhancement `0002`): "Release" is Helm's word and foregrounds a shipping *event*, when the construct's defining property is *multiplicity* — one `#Module` materialized as many concrete deployments. `core@v1` and `library v1.0.0-alpha.1` have already renamed `#ModuleRelease`/`Release` to the `Instance` forms and are published. This change moves the operator's primary CRD onto the new vocabulary so it compiles against `library@v1` and serves the renamed kind.

Scope here is **only** the `ModuleRelease` kind axis (slice O1 of `0002`). The sibling GitOps `Release` CRD → `ModulePackage` rename (O2), the API-group move `releases.opmodel.dev → opmodel.dev`, and the `module-release.opmodel.dev/*` label-domain migration (O3) are separate changes landing in the same atomic per-repo PR.

## What Changes

- **BREAKING** Rename the `ModuleRelease` CRD to `ModuleInstance`: Go types `ModuleRelease{Spec,Status,List}` → `ModuleInstance*`, served `kind` `ModuleRelease` → `ModuleInstance`, plural `modulereleases` → `moduleinstances`, shortName `mr` → `mi`. The served K8s `apiVersion` (`v1alpha1`) is unchanged.
- **BREAKING** Rename the status identity field `ReleaseUUID` → `InstanceUUID` (JSON `releaseUUID` → `instanceUUID`) and its accessor `extractReleaseUUID` → `extractInstanceUUID`.
- Rename the reconcile surface: `ModuleReleaseReconciler`, `ReconcileModuleRelease`, `ModuleReleaseParams`, the controller `Named("modulerelease")` and `mapPlatformToModuleReleases` mapper → instance forms.
- Rename the render kind seam: `const KindModuleRelease = "ModuleRelease"` → `KindModuleInstance = "ModuleInstance"` (consumed by the O2 GitOps-`Release` render path too) and the `KernelModuleRenderer` doc/field passing `ModuleRelease: rel` → `ModuleInstance: rel` into the `library@v1` renderer params.
- **Bump the library dependency** `github.com/open-platform-model/library` `v0.5.2 → v1.0.0-alpha.1`. This is compile-forcing: `library@v1` renamed the renderer-param field the operator passes, so the build does not pass without the rename above.
- `git mv` every `*release*`-named file on the `ModuleRelease` path (D10) and add a `// Was: ModuleRelease…` breadcrumb at each rename site (D11/D12).
- Regenerate `zz_generated.deepcopy.go`, the CRD base, `config/rbac/role.yaml`, and `dist/install.yaml`; rename the aggregated RBAC role files, samples, and `test/fixtures/**/modulerelease.yaml`.

This is a **MAJOR** (breaking) change; the operator artifact ships on the `v1.0.0-alpha.N` line per `0002` D13.

## Capabilities

### New Capabilities

- `module-instance-synthesis`: the renamed form of the existing `module-release-synthesis` capability — the synthesized CUE package and rendered artifact are `#ModuleInstance`, not `#ModuleRelease`, and the CR spec shape belongs to `ModuleInstance`. The `module-release-synthesis/` spec dir is `git mv`'d to `module-instance-synthesis/` (D10).

### Modified Capabilities

- `module-renderer-interface`: the renderer params type is `ModuleInstanceParams`; the field carrying the rendered object is `ModuleInstance`.
- `kernel-module-renderer`: renders a `ModuleInstance` and returns kind `ModuleInstance` via `KindModuleInstance`.
- `reconcile-loop-assembly`: the assembled reconciler is `ModuleInstanceReconciler`/`ReconcileModuleInstance`, controller `Named("moduleinstance")`, watching `ModuleInstance` CRs.
- `platform-gated-rendering`: the platform readiness gate applies to `ModuleInstance` reconciliation.

(The `status.releaseUUID → instanceUUID` field rename is implementation-level — no `openspec/specs/` requirement names the field — and is captured in design/tasks, not a spec delta. `history-tracking` requirements are unchanged.)

## Impact

- **API**: `api/v1alpha1/modulerelease_types.go` → `moduleinstance_types.go`; CRD kind/plural/shortName/status-field renamed; `zz_generated.deepcopy.go` regenerated.
- **Controller / reconcile / render**: `internal/controller/modulerelease_controller.go`, `internal/reconcile/modulerelease.go`, `internal/render/{release.go,kernel_module_renderer.go}`, `cmd/main.go`.
- **Dependencies**: library pin `v0.5.2 → v1.0.0-alpha.1` (compile-forcing).
- **Manifests / fixtures**: `config/crd/bases`, `config/rbac/modulerelease_*_role.yaml`, `config/samples/*modulerelease*`, `dist/install.yaml`, `test/fixtures/**/modulerelease.yaml`, e2e kind strings.
- **Cross-slice seams**: `KindModuleRelease` const is consumed by the O2 GitOps-`Release` render path (`internal/render/kernel_release_renderer.go`, `internal/reconcile/release.go`); O1 renames the symbol, O2 inherits it. The `module-release.opmodel.dev/uuid` label key and `releases.opmodel.dev` group are **not** touched here (O3) — O1 keeps reading `core.LabelModuleReleaseUUID` so the intermediate state compiles.
- **Test gate (downstream dependency)**: the operator's render/integration/e2e fixtures pin `opmodel.dev/core@v0` + `opmodel.dev/catalogs/opm@v0`. Because `library@v1` targets `core@v1`/`#ModuleInstance` only, those fixtures must repin to `core@v1` + `catalogs/opm@v1`, which requires the `catalog_opm@v1` (K1) slice to be **published**. Go unit tests pass independently; CUE-evaluating tests stay red until K1 publishes.
