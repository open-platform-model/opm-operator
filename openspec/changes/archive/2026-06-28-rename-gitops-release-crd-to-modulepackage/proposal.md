## Why

Enhancement `0002` D2 renames the operator's GitOps `Release` CRD to `ModulePackage`. "Release" is being retired across the whole stack (it is Helm's word and foregrounds a shipping *event*); the GitOps CRD's actual role is to point at a Flux-delivered **package** of authored OPM content and reconcile it. `ModulePackage` names that role and frees the `Release` word entirely. This is slice **O2** of `0002`; it lands in the same atomic operator PR as O1 (`ModuleRelease` → `ModuleInstance`) and O3 (API group + label domain).

## What Changes

- **BREAKING** Rename the GitOps `Release` CRD to `ModulePackage`: Go types `Release{Spec,Status,List}` → `ModulePackage*` (incl. `GetConditions`/`SetConditions` receivers and `init()` registration), served `kind` `Release` → `ModulePackage`, plural `releases` → `modulepackages`, shortName `rel` → `mpkg`. Served `apiVersion` (`v1alpha1`) unchanged.
- **BREAKING** Rename the reconcile surface: `ReleaseReconciler` → `ModulePackageReconciler` and the package `internal/reconcile/release.go` → `modulepackage.go`.
- Rename the render surface: `KernelReleaseRenderer` → `KernelPackageRenderer`, `internal/render/kernel_release_renderer.go` → `kernel_package_renderer.go`.
- Migrate the consumed `library@v1` kernel API on the package path: `Kernel.LoadReleasePackage` → `LoadInstancePackage`, `Kernel.NewReleaseFromValue` → `NewInstanceFromValue` (compile-forcing once the O1 library-pin bump lands).
- The package a `ModulePackage` points at evaluates to `kind: ModuleInstance` (O1's renamed kind) — the runtime kind-detection literal `"ModuleRelease"` → `"ModuleInstance"` and the consumed `render.KindModuleInstance` constant (defined by O1).
- The authored package file convention `release.cue` → `instance.cue` (D9) in operator specs and fixtures — the artifact carries an authored `#ModuleInstance`.
- `git mv` every `*release*`-named file on this path (D10); add `// Was: Release…` breadcrumbs at rename sites (D11/D12).
- Regenerate `zz_generated.deepcopy.go`, the CRD base, and `config/rbac/role.yaml`; rename samples/fixtures.

MAJOR (breaking); ships on the `v1.0.0-alpha.N` line (D13).

## Capabilities

### New Capabilities

Each is the **renamed form** of an existing `release-*` capability; the old spec dir is `git mv`'d to the new name at archive (D10).

- `modulepackage-reconcile-loop`: renamed from `release-reconcile-loop` — the end-to-end `ModulePackage` reconcile loop (phase ordering, triggers, suspend/no-op, finalizer, status).
- `modulepackage-artifact-loading`: renamed from `release-artifact-loading` — Flux artifact fetch, `spec.path` navigation, `instance.cue` location, registry-aware CUE evaluation, cleanup.
- `modulepackage-depends-on`: renamed from `release-depends-on` — `spec.dependsOn` ordering and same-namespace scope across `ModulePackage` CRs.
- `modulepackage-kind-detection`: renamed from `release-kind-detection` — runtime detection that the evaluated package is `kind: ModuleInstance`.
- `modulepackage-kernel-rendering`: renamed from `release-kernel-rendering` — rendering the fetched `ModuleInstance` package through `KernelPackageRenderer` against the materialized platform.

### Modified Capabilities

_None._ (All affected capabilities are full renames, authored under their new names.)

## Impact

- **API**: `api/v1alpha1/release_types.go` → `modulepackage_types.go`; `common_types.go` references; `zz_generated.deepcopy.go` regenerated.
- **Controller / reconcile / render**: `internal/controller/release_controller.go` → `modulepackage_controller.go`; `internal/reconcile/release.go` → `modulepackage.go`; `internal/render/kernel_release_renderer.go` → `kernel_package_renderer.go`; `cmd/main.go` registration; `internal/{source,status,apply}/*` `Release` references.
- **Cross-slice seams**: consumes `render.KindModuleInstance` and `ModuleInstance` artifact vocabulary from **O1**; shares the API group + label/finalizer migration with **O3** (this change keeps `releases.opmodel.dev` and the `module-release.opmodel.dev/*` labels untouched — O3 moves them). RBAC marker resource names change `releases` → `modulepackages` here; the group stays for O3.
- **Manifests / fixtures**: `config/crd/bases`, `config/rbac` role files, `config/samples/*release*`, `dist/install.yaml`, `test/fixtures/releases/**` (→ `modulepackages/`), e2e suites.
- **Test gate**: as with O1, CUE-evaluating tests repin fixtures to `core@v1` + `catalogs/opm@v1` and are gated on `catalog_opm@v1` (K1) being published.
