## Why

`BundleRelease` is speculative scaffolding: the controller is an empty stub (logs and returns), `internal/reconcile/bundlerelease.go` is a placeholder struct, and no `#BundleRelease` schema exists in `core/` or `library/` — the kernel accepts only `Module`, `ModuleRelease`, and `Platform`. No `kind: BundleRelease` resource exists anywhere in the workspace. The CRD, its config surface, and its doc/spec references describe a contract with no source of truth, confusing readers about what the operator actually does. Bundle orchestration needs real design work before any implementation; until then the scaffolding is noise.

## What Changes

- **BREAKING** Remove the `BundleRelease` CRD (`api/v1alpha1/bundlerelease_types.go`), including `ModuleStatusSummary` (only used by `BundleReleaseStatus`).
- Remove the stub `BundleReleaseReconciler` controller, its empty test, the placeholder `internal/reconcile/bundlerelease.go`, and the `cmd/main.go` registration.
- Generalize the Release pipeline's wrong-kind rejection: drop the `KindBundleRelease` constant and bundle-specific error message; any non-`#ModuleRelease` package keeps stalling with `Ready=False` / `UnsupportedKind` — the safety gate's semantics are unchanged, only the speculative "BundleRelease" labeling goes.
- Remove BundleRelease config surface: CRD base, sample, three RBAC role files, kustomization entries, and the `PROJECT` entry; regenerate `role.yaml`, DeepCopy, and `dist/install.yaml`.
- Supersede ADR-007 (BundleRelease as orchestrator) with a note that bundle orchestration is deferred pending design; strike BundleRelease mentions from ADR-003/008/015.
- Fix stale doc claims in the same pass: `CONSTITUTION.md` says `internal/source/` is "retained for BundleRelease" (it is used by `Release`); `CLAUDE.md` lists the CRDs as "ModuleRelease + BundleRelease" (omits `Release`, `Platform`).

Out of scope: any bundle redesign, `ModuleRelease`/`Release` consolidation, behavior changes to apply/prune/status.

## Capabilities

### New Capabilities

(none — this change removes surface)

### Modified Capabilities

- `release-kind-detection`: remove the BundleRelease dispatch branch; detection becomes `ModuleRelease` or generic `UnsupportedKind` stall.
- `release-kernel-rendering`: replace the "BundleRelease remains unsupported" requirement with a generic wrong-kind rejection requirement (same `ErrUnsupportedKind` behavior).
- `module-release-synthesis`: remove the "BundleRelease does not depend on Flux source types" requirement and the deferred `spec.sourceRef` removal note — the controller no longer exists.

## Impact

- **API**: `releases.opmodel.dev/v1alpha1 BundleRelease` deleted. Alpha API, zero in-repo or workspace consumers (`releases/`, `modules/`, `opm-kind-demo/` checked). No migration needed; clusters that applied an older `dist/install.yaml` keep an orphaned CRD until `kubectl delete crd bundlereleases.releases.opmodel.dev`.
- **SemVer**: MINOR (0.x line; removes an unused alpha API — would be MAJOR post-1.0).
- **Code**: ~310 lines of Go deleted; comment/message edits in `internal/render/release.go`, `kernel_release_renderer.go`, `release_types.go`, `common_types.go`; tests generalized, not deleted (wrong-kind coverage stays).
- **Config**: `config/crd/`, `config/rbac/`, `config/samples/`, `PROJECT`, `dist/install.yaml`. `PROJECT` requires a deliberate hand-edit — Kubebuilder has no `delete api` command.
- **Docs/specs**: ADR-003/007/008/015, `CONSTITUTION.md`, `CLAUDE.md`, three openspec specs (above); `inventory-bridge` historical notes left as-is.
