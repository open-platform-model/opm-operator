## Context

Slice **O3** of enhancement `0002` (D4 label domain, D5 API group). After O1 (`ModuleRelease`→`ModuleInstance`) and O2 (`Release`→`ModulePackage`) rename the kinds, O3 moves all three CRDs (`ModuleInstance`, `ModulePackage`, `Platform`) from group `releases.opmodel.dev` to `opmodel.dev`, migrates the finalizer key, and migrates the resource-label domain `module-release.opmodel.dev/* → module-instance.opmodel.dev/*`. It lands in the same atomic operator PR, last of the three.

`library@v1` already stamps `module-instance.opmodel.dev/{name,uuid}` on rendered resources. O3 is what realigns the operator's read side (prune ownership guard, inventory selectors, status UUID) with that emitted label domain — without O3, the operator looks for a label key the kernel no longer emits.

## Goals / Non-Goals

**Goals:**
- Move the API group of all three CRDs to `opmodel.dev` (markers, `groupversion_info.go`, `PROJECT`, kustomize, regenerated CRDs/RBAC/installer).
- Migrate the finalizer key and the label domain (constants + values + all consumers).
- Keep CRD kinds and the served `apiVersion` version (`v1alpha1`) unchanged.

**Non-Goals:**
- The kind renames themselves (O1, O2).
- Any behavioral change beyond the group/label/finalizer string moves.
- Renaming the `Platform` kind (it only changes group).

## Decisions

### O3 owns the full vocabulary update of the two capabilities it touches
**Context**: `finalizer-and-deletion` and `prune-stale-resources` reference both O3's migrated tokens (finalizer key, label domain) *and* `ModuleRelease` prose. Neither capability is claimed by O1 or O2.
**Decision**: O3's deltas to these two capabilities carry the complete vocabulary update — finalizer key + label domain + `ModuleRelease`/`ReleaseUUID`/`ModuleReleaseStatus` → `ModuleInstance`/`InstanceUUID`/`ModuleInstanceStatus`.
**Rationale**: Avoids leaving stale `ModuleRelease` prose in the merged specs; the slice that modifies a capability is responsible for its consistency. Alternative (split the kind-rename prose into O1) was rejected — O1's capability list deliberately excludes these two, and splitting one requirement block across two changes is not representable in OpenSpec deltas.

### No deltas for `inventory-bridge`, `ssa-apply`, `platform-crd`
**Context**: Planned-changes lists these as touched by O3, but their `openspec/specs/` requirement text names none of the migrated tokens (verified by grep).
**Decision**: No spec deltas for them. The group move (incl. `Platform`'s group), SSA label stamping, and inventory label selectors are implementation/manifest-level and captured in tasks + regenerated manifests.
**Rationale**: OpenSpec deltas track observable spec-level behavior; a delta that only re-states unchanged requirement text is noise (same rule applied to `history-tracking` in O1).

### CRD-base filenames settle in one regeneration
**Context**: O1/O2 change the plural; O3 changes the group; both are embedded in the CRD-base filename (`releases.opmodel.dev_modulereleases.yaml` → `opmodel.dev_moduleinstances.yaml`).
**Decision**: Let O1/O2 do their renames, then a single `task dev:manifests dev:generate` at the end of the bulk PR produces the final `opmodel.dev_*.yaml` bases; do not hand-rename intermediate generated files.
**Rationale**: Generated files are not hand-edited (repo rule); one regeneration avoids churn and stale intermediates.

## Risks / Trade-offs

- **Group change is an install-time break** → Existing CRs in the old group are not migrated; the new group is a new API surface. *Mitigation*: breaking major (`v1.0.0-alpha.N`); reinstall-based rollout per `0002` `06-operational.md`. Pre-1.0, no external consumers.
- **Label-domain split-brain if O3 is omitted** → Without O3, `library@v1`'s `module-instance.opmodel.dev/uuid` stamping and the operator's old-label reads disagree, breaking prune ownership. *Mitigation*: O3 is mandatory in the same PR; this is the slice that closes the gap O1/O2 leave open.
- **RBAC drift** → Group change touches every RBAC marker and aggregated role. *Mitigation*: regenerate `role.yaml` + `dist/install.yaml` via `task dev:manifests dev:generate` + installer task; do not hand-edit.

## Migration Plan

1. Apply last in the atomic operator PR, after O1 and O2 are on the branch.
2. `groupversion_info.go` group; sweep `//+kubebuilder:rbac` markers; hand-edit `PROJECT`; update kustomize bases.
3. Migrate `FinalizerName` and `pkg/core/labels.go` constants/values; update all consumers (prune, inventory, status UUID, SSA stamping, render adapters).
4. `task dev:manifests dev:generate` + installer task → regenerate CRD bases, `role.yaml`, deepcopy, `dist/install.yaml`.
5. Repin fixtures (`core@v1` + `catalogs/opm@v1`) and run the full gate once K1 is published; verify the prune ownership guard against the new label domain.
6. Bulk-archive O1+O2+O3 deltas together (`openspec-bulk-archive-change`).
7. Rollback: reinstall prior CRDs/controller (breaking major, group change).

## Open Questions

- **None blocking authoring.** The K1-publish dependency for CUE-evaluating tests is shared across O1/O2/O3 and tracked in tasks.
