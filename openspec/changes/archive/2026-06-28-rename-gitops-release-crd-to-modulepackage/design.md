## Context

Slice **O2** of enhancement `0002` (D2). The operator currently defines a GitOps `Release` CRD (group `releases.opmodel.dev`, shortName `rel`) that points at a Flux artifact, fetches it, navigates to `spec.path`, loads `release.cue`, and renders it when the evaluated `kind` is `ModuleRelease`. `0002` retires the "Release" word; this CRD's role — referencing a Flux-delivered package of authored content — is named `ModulePackage`.

O2 lands in one atomic operator PR with O1 (`ModuleRelease` → `ModuleInstance`) and O3 (group + label/finalizer). It consumes O1's renamed kind (`ModuleInstance`, `render.KindModuleInstance`) and the `library@v1` kernel API.

## Goals / Non-Goals

**Goals:**
- Rename the GitOps `Release` CRD to `ModulePackage` (types, kind, plural, shortName `rel`→`mpkg`) and its reconcile/render surface.
- Migrate the package-path `library@v1` kernel calls (`LoadReleasePackage`→`LoadInstancePackage`, `NewReleaseFromValue`→`NewInstanceFromValue`).
- Detect/accept `kind: ModuleInstance` packages (O1's renamed kind); reject others unchanged.
- Apply the `release.cue`→`instance.cue` authored-file convention (D9) in operator specs/fixtures.
- `git mv` every `*release*`-named file on this path; `// Was:` breadcrumbs (D10/D11/D12).

**Non-Goals:**
- The API-group move, finalizer-key change, and label-domain migration (O3). RBAC marker *resource* names move here (`releases`→`modulepackages`); the *group* stays for O3.
- The `ModuleRelease`→`ModuleInstance` kind rename itself (O1).
- Any behavioral/field-shape change. Served `apiVersion` stays `v1alpha1`. `spec` fields (`sourceRef`, `path`, `dependsOn`, `prune`, `suspend`, `interval`, `serviceAccountName`, `rollout`) are unchanged.

## Decisions

### Rename target names
**Decision**:
- CRD/type `Release` → `ModulePackage` (`ReleaseSpec/Status/List` → `ModulePackage*`); shortName `rel` → `mpkg`; plural `releases` → `modulepackages`.
- `ReleaseReconciler` → `ModulePackageReconciler`; `internal/reconcile/release.go` → `modulepackage.go`; `internal/controller/release_controller.go` → `modulepackage_controller.go`.
- `KernelReleaseRenderer` → `KernelPackageRenderer`; `internal/render/kernel_release_renderer.go` → `kernel_package_renderer.go`.
**Rationale**: `ModulePackage` describes the construct (a referenced package of module content); `KernelPackageRenderer` is the renderer for that package path, distinct from O1's `KernelModuleRenderer` (synthesis path). The five `release-*` capability dirs rename to `modulepackage-*` per `0002`'s explicit mapping.

### The authored file is `instance.cue`, not `release.cue` or `modulepackage.cue`
**Context**: A `ModulePackage` artifact contains an authored package that evaluates to `kind: ModuleInstance`.
**Decision**: The conventional filename is `instance.cue` (D9) — the authored artifact is a `#ModuleInstance`, so it follows the same instance-file convention as the CLI (X1/D9), not a name derived from the CRD.
**Rationale**: The file names the *content kind* (instance), not the *delivery wrapper* (package). Failure reason `ReleaseFileNotFound` → `InstanceFileNotFound` follows.
**Note**: This ripples into out-of-scope `modules/`/`releases/` fixtures (tracked as the `0002` closing sweep); within the operator only its own specs/fixtures change here.

### Kind detection consumes O1's renamed constant
**Decision**: The runtime kind literal `"ModuleRelease"` → `"ModuleInstance"` and the consumed `render.KindModuleInstance` constant are O1's; O2 references the renamed symbol. `ErrUnsupportedKind` and the generic-rejection contract (`loaderfile.ErrWrongKind`) are unchanged.
**Rationale**: The package's renderable kind is the same artifact O1 renamed; both slides land in one PR.

## Risks / Trade-offs

- **CRD identity break for existing `Release` CRs** → A renamed CRD is a new resource; existing `Release` objects are not migrated in place. *Mitigation*: breaking major (`v1.0.0-alpha.N`); rollout is reinstall, consistent with `0002` D13 and the PRR-lite in `06-operational.md`. No `Release` consumers exist outside the workspace (the operator is pre-1.0).
- **Compile coupling with O1/O3** → The `library@v1` kernel calls on this path won't compile until the O1 pin bump lands; the label/group references won't be final until O3. *Mitigation*: one atomic PR; verify gates after all three.
- **CUE-evaluating tests gated on K1** → Same as O1 — fixtures repin to `core@v1` + `catalogs/opm@v1`, gated on `catalog_opm@v1` published.

## Migration Plan

1. Implemented in the one atomic operator PR with O1 and O3, after O1's type/kind/const renames.
2. `git mv` the API/controller/reconcile/render files; rename types and receivers; migrate the two package-path kernel calls.
3. Rename `config/samples/*release*`, `config/rbac` role files, `test/fixtures/releases/` → `modulepackages/`; regenerate manifests/deepcopy/RBAC via `task dev:manifests dev:generate`.
4. Bulk-archive the five `release-*` → `modulepackage-*` capability dir moves with the ADDED deltas.
5. Rollback: reinstall of the prior CRD/controller (breaking major).

## Open Questions

- **None blocking authoring.** The K1-publish dependency for CUE-evaluating tests is shared with O1 and tracked in tasks.
