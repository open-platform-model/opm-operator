# Tasks — remove-bundlerelease-scaffolding

## 1. Remove BundleRelease Go surface

- [x] 1.1 Delete `api/v1alpha1/bundlerelease_types.go` and remove `ModuleStatusSummary` from `api/v1alpha1/common_types.go` (only consumer is `BundleReleaseStatus.Modules`)
- [x] 1.2 Delete `internal/controller/bundlerelease_controller.go` and `internal/controller/bundlerelease_controller_test.go`
- [x] 1.3 Delete `internal/reconcile/bundlerelease.go` (placeholder struct)
- [x] 1.4 Remove the `BundleReleaseReconciler` registration block from `cmd/main.go` (~lines 272–278)

## 2. Generalize the wrong-kind rejection gate

- [x] 2.1 In `internal/render/release.go`: remove the `KindBundleRelease` constant and reword the `ErrUnsupportedKind` doc comment to be kind-agnostic
- [x] 2.2 In `internal/render/kernel_release_renderer.go`: replace the bundle-specific `ErrWrongKind` branch (returns `KindBundleRelease` + "BundleRelease rendering is not yet implemented") with a generic unsupported-kind result and message; fix the surrounding comments
- [x] 2.3 Generalize `TestKernelReleaseRenderer_BundleReleaseUnsupported` in `kernel_release_renderer_test.go` to a wrong-kind test (keep coverage; do not delete) and update the stub error message in `internal/controller/release_controller_test.go` (~line 326)
- [x] 2.4 Update doc comments referencing `#BundleRelease`/BundleRelease in `api/v1alpha1/release_types.go` (ReleaseSpec comment) and `api/v1alpha1/common_types.go` (`SourceReference` comment — note it is used by `Release`, not "BundleRelease")

## 3. Config and generated surface

- [x] 3.1 Delete `config/samples/releases_v1alpha1_bundlerelease.yaml` and its entry in `config/samples/kustomization.yaml`
- [x] 3.2 Delete `config/rbac/bundlerelease_{admin,editor,viewer}_role.yaml` and their entries in `config/rbac/kustomization.yaml`
- [x] 3.3 Remove the `bases/releases.opmodel.dev_bundlereleases.yaml` entry from `config/crd/kustomization.yaml`, then delete the generated CRD base file
- [x] 3.4 Hand-edit `PROJECT` to remove the `BundleRelease` resource entry (deliberate exception to the no-hand-edit rule — Kubebuilder has no `delete api` command; isolate in its own commit)
- [x] 3.5 Regenerate: `task dev:manifests dev:generate` (role.yaml, DeepCopy) and `task operator:installer` (`dist/install.yaml`); verify no `bundlerelease` strings remain in generated output

## 4. Docs, ADRs, and repo guides

- [x] 4.1 Mark `adr/007-bundlerelease-as-orchestrator.md` as Superseded with a note: bundle orchestration deferred pending real design (incl. orchestrator-vs-inline-rendering open question); scaffolding removed
- [x] 4.2 Strike BundleRelease mentions from `adr/003` (consequences), `adr/008:25` (API group taxonomy), `adr/015:45` (user-applied resources)
- [x] 4.3 Fix `CONSTITUTION.md` lines 34 and 44: `internal/source/` is used by the `Release` pipeline, not "retained for BundleRelease"
- [x] 4.4 Fix `CLAUDE.md`: line 6 CRD list → `ModuleRelease`, `Release`, `Platform`; replace the `BundleRelease Controller` ginkgo-focus example (line 82) with an existing suite name; check `AGENTS.md` for the same claims
- [x] 4.5 Release note for the BundleRelease CRD removal + orphaned-CRD cleanup. `CHANGELOG.md` is release-please-generated (do not hand-edit), so this lands in the **commit footer** instead — a `feat(api)!:` subject plus `BREAKING CHANGE:` footer noting `kubectl delete crd bundlereleases.releases.opmodel.dev`, which release-please reads to generate the CHANGELOG entry.

## 5. Verify

- [x] 5.1 `grep -ri bundlerelease` across the repo (excluding `openspec/changes/` and git history) returns nothing unexpected — `openspec/specs/` deltas applied at archive time; `inventory-bridge` historical notes intentionally untouched
- [x] 5.2 Run validation gates: `task dev:fmt dev:vet dev:lint dev:test`
- [x] 5.3 Confirm wrong-kind stall behavior still covered: renderer wrong-kind unit test passes and release controller `UnsupportedKind` path test passes
