## Context

This is slice **O1** of enhancement `0002` (Release → Instance vocabulary rename). `core@v1` (`v1.0.0-alpha.1`) and `library v1.0.0-alpha.1` are already published with the renamed `#ModuleInstance` family and Go `Instance` surface. The operator currently pins `github.com/open-platform-model/library v0.5.2` and serves a `ModuleRelease` CRD in API group `releases.opmodel.dev`.

O1 covers **only the `ModuleRelease` kind axis**. Two sibling slices land in the same atomic operator PR and are authored separately:
- **O2** — GitOps `Release` CRD → `ModulePackage`.
- **O3** — API group `releases.opmodel.dev → opmodel.dev`, finalizer key, and the `module-release.opmodel.dev/* → module-instance.opmodel.dev/*` label domain.

Per `0002`'s split-by-concern rule, a pure rename cannot be split into separately *mergeable* PRs (intermediate states don't compile), so O1/O2/O3 are spec-tracked as distinct units but implemented in one PR and bulk-archived.

## Goals / Non-Goals

**Goals:**
- Rename the `ModuleRelease` CRD to `ModuleInstance` (Go types, served kind, plural, shortName `mr`→`mi`) and the reconcile/render/synthesis surface that names it.
- Bump the library dependency to `v1.0.0-alpha.1` so the operator compiles against the renamed kernel.
- Rename the status identity field `ReleaseUUID → InstanceUUID`.
- `git mv` every `*release*`-named file on the `ModuleRelease` path; add `// Was:` breadcrumbs at rename sites (D10/D11/D12).
- Keep the intermediate (post-O1, pre-O3) tree compilable.

**Non-Goals:**
- The API-group move, finalizer-key change, and label-domain migration (O3).
- The GitOps `Release` → `ModulePackage` rename (O2).
- Any behavioral, field-shape, or evaluation-semantic change. Served `apiVersion` stays `v1alpha1`.
- Re-pinning the operator's CUE test fixtures' catalog dependency beyond what the test gate requires (see Risks — K1 publish).

## Decisions

### The library-pin bump belongs in O1, not a separate step
**Context**: O1 must compile against `library@v1`.
**Decision**: Bump `github.com/open-platform-model/library v0.5.2 → v1.0.0-alpha.1` as part of O1.
**Rationale**: `library@v1` renamed the renderer-param field the operator passes at `internal/render/kernel_module_renderer.go` (`ModuleRelease: rel` → `ModuleInstance: rel`) and the kernel entry points (`SynthesizeRelease → SynthesizeInstance`, `ProcessModuleRelease → ProcessModuleInstance`). The build does not pass until both the pin and the call sites move together — they are inseparable.
**Alternative considered**: Bump the pin in O3 (alongside other dependency/manifest churn). Rejected — the operator would not compile between O1 and O3.

### O1 owns the `KindModuleRelease` constant rename; O2 inherits the new symbol
**Context**: `internal/render/release.go` defines `const KindModuleRelease = "ModuleRelease"`, consumed by the O1 module renderer (`kernel_module_renderer.go`) **and** the O2 GitOps-`Release` render path (`internal/render/kernel_release_renderer.go`, `internal/reconcile/release.go:381`).
**Decision**: O1 renames the constant to `KindModuleInstance = "ModuleInstance"`. O2's tasks note they consume the renamed symbol.
**Rationale**: The constant is the rendered-kind string of the `ModuleInstance` artifact; its value must equal what `library@v1` expects (`ReleaseSpec.ExpectedKind = "ModuleInstance"`). It is conceptually O1's. In the single atomic PR, the seam is invisible; the separation is only a spec-authoring convenience.

### The `module-release-synthesis` capability dir is renamed, not duplicated
**Context**: The capability spec dir name carries `release`.
**Decision**: Author the delta under the new name `module-instance-synthesis` as `ADDED Requirements`, and `git mv openspec/specs/module-release-synthesis → module-instance-synthesis` at archive (mirrors L1's `release-synthesis → instance-synthesis`).
**Rationale**: Matches the established repo precedent; bulk-archive performs the dir move.

### The `status.releaseUUID → instanceUUID` rename is not a spec delta
**Context**: No `openspec/specs/` requirement names the UUID status field.
**Decision**: Capture the field rename (`ReleaseUUID`/`releaseUUID` → `InstanceUUID`/`instanceUUID`, `extractReleaseUUID → extractInstanceUUID`) in tasks only; `history-tracking` requirements are unchanged so it gets no delta.
**Rationale**: OpenSpec deltas track observable spec-level behavior. The field is implementation detail; adding a no-op `history-tracking` delta would be noise.

### O1 keeps reading `core.LabelModuleReleaseUUID` (O3 owns the label rename)
**Context**: `extractInstanceUUID` reads the UUID off rendered resources via `core.LabelModuleReleaseUUID = "module-release.opmodel.dev/uuid"`.
**Decision**: O1 does **not** touch `pkg/core/labels.go`. The renamed `extractInstanceUUID`/`mi.Status.InstanceUUID` keep reading the existing label constant; O3 renames the constant and its value.
**Rationale**: Keeps the kind axis and the label-domain axis independent, and the post-O1/pre-O3 tree compilable.

## Risks / Trade-offs

- **CUE-evaluating tests stay red until K1 publishes** → The operator's render/integration/e2e fixtures pin `opmodel.dev/core@v0` + `opmodel.dev/catalogs/opm@v0`. `library@v1` targets `core@v1`/`#ModuleInstance` only, so those fixtures must repin to `core@v1` + `catalogs/opm@v1`, which needs `catalog_opm@v1` (K1) **published** (it is implemented and committed locally but not yet tagged). *Mitigation*: O1's Go unit tests pass independently; the fixture repin + the integration/e2e gate are sequenced after K1 publishes. Tasks mark the fixture repin and full-gate verification as K1-gated.
- **Shared `KindModuleRelease` const seam with O2** → Renaming the const ripples into O2-owned files. *Mitigation*: O1/O2/O3 land in one PR; the const rename and all consumers move together.
- **Generated-file regeneration drift** → CRD base, RBAC role, deepcopy, and `dist/install.yaml` must be regenerated, not hand-edited. *Mitigation*: run `task dev:manifests dev:generate` + the installer task; the CRD base filename's group prefix settles after O3 (single regeneration at the end of the bulk PR).

## Migration Plan

1. Implemented in one atomic operator PR with O2 and O3 (no separately-mergeable intermediate).
2. Order within the PR: O1 (this) → O2 → O3, then a single `task dev:manifests dev:generate` + installer regeneration.
3. Repin CUE fixtures to `core@v1` + `catalogs/opm@v1` once K1 is published; then run the full gate (`task dev:fmt dev:vet dev:lint dev:test`, and `task dev:e2e` where Kind is available).
4. Bulk-archive the spec deltas (`openspec-bulk-archive-change`), including the `module-release-synthesis → module-instance-synthesis` capability dir move.
5. Rollback: the operator ships on the `v1.0.0-alpha.N` line (a breaking major); rollback is reinstall of the prior CRD/controller, not in-place downgrade.

## Open Questions

- **None blocking authoring.** The only sequencing dependency (K1 publish before the CUE-evaluating test gate) is tracked in tasks; it does not change O1's code surface.
