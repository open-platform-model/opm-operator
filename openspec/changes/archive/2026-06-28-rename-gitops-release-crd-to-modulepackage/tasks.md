## 1. API types

- [x] 1.1 `git mv api/v1alpha1/release_types.go api/v1alpha1/modulepackage_types.go`.
- [x] 1.2 Rename `ReleaseSpec`/`ReleaseStatus`/`Release`/`ReleaseList` → `ModulePackage*`; receivers `GetConditions`/`SetConditions` on `*ModulePackage`; `init()` `SchemeBuilder.Register(&ModulePackage{}, &ModulePackageList{})`; add `// Was: Release…` breadcrumbs.
- [x] 1.3 kubebuilder markers: `shortName=rel` → `shortName=mpkg`; doc comments "Schema for the releases API" → "modulepackages"; the spec doc comment "loads release.cue … #ModuleRelease" → "loads instance.cue … #ModuleInstance". Leave `+groupName`/RBAC group for O3.
- [x] 1.4 `api/v1alpha1/common_types.go`: rename any `Release`-named shared identifiers used by this CRD (keep finalizer key string for O3).

## 2. Reconcile, controller, render

- [x] 2.1 `git mv internal/reconcile/release.go internal/reconcile/modulepackage.go` (+ `release_dependson_test.go`); rename `ReleaseReconciler`-driven functions/params, `Release`/`ReleaseList` references, `DependsOn` same-namespace logic; runtime kind literal `"ModuleRelease"` → `"ModuleInstance"` and `render.KindModuleInstance` (from O1). `// Was:` breadcrumbs.
- [x] 2.2 `git mv internal/controller/release_controller.go internal/controller/modulepackage_controller.go` (+ tests `release_controller_test.go`, `release_platform_gate_test.go`); `ReleaseReconciler` → `ModulePackageReconciler`, `Named("release")` → `Named("modulepackage")`, `For(&…Release{})`/`ReleaseList` → ModulePackage forms; RBAC marker resource `releases` → `modulepackages` (group stays for O3).
- [x] 2.3 `git mv internal/render/kernel_release_renderer.go internal/render/kernel_package_renderer.go` (+ test); `KernelReleaseRenderer` → `KernelPackageRenderer`; migrate kernel calls `Kernel.LoadReleasePackage` → `LoadInstancePackage`, `Kernel.NewReleaseFromValue` → `NewInstanceFromValue`; doc "renders a ModuleRelease/release package" → "ModuleInstance package".
- [x] 2.4 `cmd/main.go`: controller registration string/type `Release` → `ModulePackage`.
- [x] 2.5 `internal/{source,status,apply}/*.go`: `Release`-typed references (`fetch.go`, `status/{conditions,counters,history}.go`, `apply/prune.go`) → `ModulePackage` where they name this CRD; keep label/finalizer literals for O3.

## 3. Generated manifests, RBAC, samples, fixtures

- [x] 3.1 `git mv config/rbac/release_*_role.yaml` (if present) → `modulepackage_*`; update `config/rbac/kustomization.yaml`; role rule resources `releases` → `modulepackages`.
- [x] 3.2 `git mv config/samples/releases_v1alpha1_release*.yaml` → `…_modulepackage*`; set `kind: ModulePackage`; update `config/samples/kustomization.yaml`.
- [x] 3.3 `git mv test/fixtures/releases/ test/fixtures/modulepackages/`; rename in-fixture `release.cue` → `instance.cue` and `kind` references; update test references.
- [x] 3.4 `task dev:manifests dev:generate` — regenerate CRD base, `config/rbac/role.yaml`, `zz_generated.deepcopy.go`. No hand-edits.

## 4. Spec dir moves

- [x] 4.1 `git mv` the five capability dirs at bulk-archive: `release-reconcile-loop`→`modulepackage-reconcile-loop`, `release-artifact-loading`→`modulepackage-artifact-loading`, `release-depends-on`→`modulepackage-depends-on`, `release-kind-detection`→`modulepackage-kind-detection`, `release-kernel-rendering`→`modulepackage-kernel-rendering`.

## 5. Validation gates

- [x] 5.1 `task dev:fmt dev:vet` — green (requires O1's library-pin bump on the same branch).
- [x] 5.2 `task dev:lint` — 0 issues.
- [x] 5.3 `task dev:test` — unit tests green. (CUE-evaluating integration tests require fixture repin to `core@v1` + `catalogs/opm@v1` / K1 published.)
- [x] 5.4 `task dev:e2e:local` — **GREEN** (4 Passed | 0 Failed | 13 Skipped, 162s). Required two fixes: purged bogus empty `core@v1.0.x` registry tags (unblocked unpinned `core@v1` resolution → controller starts clean) and a rollout-settle wait in `e2e_test.go` after the `--registry` patch (single stable leader materializes the Platform before the ModuleInstance specs).
- [x] 5.5 `openspec validate rename-gitops-release-crd-to-modulepackage --strict` → `openspec-verify-change` before archive.
