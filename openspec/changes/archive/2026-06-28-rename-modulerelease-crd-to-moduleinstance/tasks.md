## 1. Dependency pin

- [x] 1.1 Bump `github.com/open-platform-model/library` `v0.5.2 → v1.0.0-alpha.1` in `go.mod`; `go mod tidy`. (Compile-forcing — the build stays broken until §3 lands.) — pin bumped + `go.sum` populated via `go mod download`; final `go mod tidy` deferred to the post-O2/O3 compile gate.

## 2. API types

- [x] 2.1 `git mv api/v1alpha1/modulerelease_types.go api/v1alpha1/moduleinstance_types.go`.
- [x] 2.2 Rename types `ModuleReleaseSpec`/`ModuleReleaseStatus`/`ModuleRelease`/`ModuleReleaseList` → `ModuleInstance*`; update the `init()` `SchemeBuilder.Register`; add `// Was: ModuleRelease…` breadcrumbs (D11/D12).
- [x] 2.3 Status field `ReleaseUUID string \`json:"releaseUUID,…"\`` → `InstanceUUID \`json:"instanceUUID,…"\``; update the field doc comment to "rendered ModuleInstance" but keep the `module-release.opmodel.dev/uuid` label-key reference (O3 renames the key).
- [x] 2.4 kubebuilder markers: `shortName=mr` → `shortName=mi`; doc comments "Schema for the modulereleases API" → "moduleinstances". Leave `+groupName` / RBAC group `releases.opmodel.dev` for O3.

## 3. Render, reconcile, controller

- [x] 3.1 `internal/render/release.go`: `const KindModuleRelease = "ModuleRelease"` → `KindModuleInstance = "ModuleInstance"`; update `ErrUnsupportedKind` doc ("Only #ModuleInstance is renderable"); `// Was:` breadcrumb. (Consumed by the O2 GitOps-`Release` path — O2 picks up the renamed symbol.)
- [x] 3.2 `internal/render/kernel_module_renderer.go`: doc "renders a ModuleRelease" → "ModuleInstance"; the renderer-param field `ModuleRelease: rel` → `ModuleInstance: rel`; `Kernel.SynthesizeRelease` → `Kernel.SynthesizeInstance`; return `KindModuleInstance`.
- [x] 3.3 `git mv internal/reconcile/modulerelease.go internal/reconcile/moduleinstance.go` (+ `_test.go`); rename `ModuleReleaseParams` → `ModuleInstanceParams`, `ReconcileModuleRelease` → `ReconcileModuleInstance`, `extractReleaseUUID` → `extractInstanceUUID`; `mr.Status.ReleaseUUID` → `mi.Status.InstanceUUID`; keep `core.LabelModuleReleaseUUID` reads (O3 renames the label). `// Was:` breadcrumbs.
- [x] 3.4 `git mv internal/controller/modulerelease_controller.go internal/controller/moduleinstance_controller.go` (+ `_test.go`, `modulerelease_platform_gate_test.go`, `modulerelease_reconcile_test.go`); rename `ModuleReleaseReconciler` → `ModuleInstanceReconciler`, `mapPlatformToModuleReleases` → `mapPlatformToModuleInstances`, `Named("modulerelease")` → `Named("moduleinstance")`, `For(&…ModuleRelease{})`/`ModuleReleaseList` → instance forms; pass `ModuleInstanceParams`. Leave the RBAC marker groups (`releases.opmodel.dev`) for O3 but rename the resource `modulereleases` → `moduleinstances`.
- [x] 3.5 `cmd/main.go`: controller registration string `"ModuleRelease"` → `"ModuleInstance"`; reconciler type reference.
- [x] 3.6 `internal/render/kernel_module_renderer_test.go` and any other `*_test.go` under render/reconcile/controller: kind literals `"ModuleRelease"` → `"ModuleInstance"`, type/method references.

## 4. Generated manifests, RBAC, samples

- [x] 4.1 `git mv config/rbac/modulerelease_{admin,editor,viewer}_role.yaml` → `moduleinstance_*`; update `kustomization.yaml` references and role rule resources `modulereleases` → `moduleinstances`.
- [x] 4.2 `git mv config/samples/releases_v1alpha1_modulerelease.yaml`/`…_jellyfin.yaml` → `…_moduleinstance*`; set `kind: ModuleInstance`; update `config/samples/kustomization.yaml`. (Group prefix in the filename settles with O3's regeneration.)
- [x] 4.3 `task dev:manifests dev:generate` — regenerates the CRD base, `config/rbac/role.yaml`, and `zz_generated.deepcopy.go` for the renamed kind. Do not hand-edit generated files.

## 5. Test fixtures

- [x] 5.1 `git mv test/fixtures/modules/{hello,redis,podinfo}/modulerelease.yaml` → `moduleinstance.yaml`; set `kind: ModuleInstance`.
- [x] 5.2 e2e suites (`test/e2e/{finalizer,lifecycle,prune,podinfo,concurrent}_test.go`): kind strings / typed references `ModuleRelease` → `ModuleInstance`.
- [x] 5.3 **K1-gated** — repin the CUE fixtures (`test/fixtures/**/cue.mod/module.cue`) `opmodel.dev/core@v0` → `@v1` (`v1.0.0-alpha.1`) and `opmodel.dev/catalogs/opm@v0` → `@v1`; the fixture `.cue` content's `#ModuleRelease`/`#moduleReleaseMetadata` → instance forms. Requires `catalog_opm@v1` (K1) published.

## 6. Spec dir move

- [x] 6.1 `git mv openspec/specs/module-release-synthesis openspec/specs/module-instance-synthesis` (done at bulk-archive of this change's `module-instance-synthesis` delta).

## 7. Validation gates

- [x] 7.1 `task dev:fmt dev:vet` — green.
- [x] 7.2 `task dev:lint` — 0 issues.
- [x] 7.3 `task dev:test` — unit tests green. (Integration tests that evaluate CUE require §5.3 / K1 published.)
- [x] 7.4 `task dev:e2e:local` — **GREEN** (4 Passed | 0 Failed | 13 Skipped, 162s). Required two fixes: purged bogus empty `core@v1.0.x` registry tags (unblocked unpinned `core@v1` resolution → controller starts clean) and a rollout-settle wait in `e2e_test.go` after the `--registry` patch (single stable leader materializes the Platform before the ModuleInstance specs).
- [x] 7.5 `openspec validate rename-modulerelease-crd-to-moduleinstance --strict` → `openspec-verify-change` before archive.
