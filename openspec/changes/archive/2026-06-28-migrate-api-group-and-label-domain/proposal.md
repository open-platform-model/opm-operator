## Why

Enhancement `0002` moves two cluster-facing axes off the retired "release" vocabulary: the API group `releases.opmodel.dev → opmodel.dev` (D5) and the resource-label domain `module-release.opmodel.dev/* → module-instance.opmodel.dev/*` (D4), plus the finalizer key `releases.opmodel.dev/cleanup → opmodel.dev/cleanup`. These are the last operator-side pieces that still say "release". This is slice **O3**; it lands in the same atomic operator PR as O1 (`ModuleRelease`→`ModuleInstance`) and O2 (`Release`→`ModulePackage`), and runs after them because it moves the already-renamed kinds to the new group.

`library@v1`'s synth already stamps `module-instance.opmodel.dev/{name,uuid}` on rendered resources; until O3 lands, the operator still reads the old `module-release.opmodel.dev/uuid` label and its prune ownership guard breaks. O3 realigns the operator with the kernel.

## What Changes

- **BREAKING** API group `releases.opmodel.dev → opmodel.dev` across all three CRDs (`ModuleInstance`, `ModulePackage`, `Platform`): `groupversion_info.go` `+groupName` and `GroupVersion`, every `//+kubebuilder:rbac` marker, `PROJECT`, kustomize bases. CRD **kinds** and served `apiVersion` version (`v1alpha1`) are unchanged — only the group moves. The `Platform` CRD moves group with its siblings (kind unchanged).
- **BREAKING** Finalizer key `releases.opmodel.dev/cleanup → opmodel.dev/cleanup` (`FinalizerName` const + all consumers).
- **BREAKING** Label domain `module-release.opmodel.dev/{name,namespace,uuid} → module-instance.opmodel.dev/{name,namespace,uuid}` and the Go constants `LabelModuleRelease{Name,Namespace,UUID} → LabelModuleInstance*` (`pkg/core/labels.go`), with all prune/inventory/apply consumers updated.
- Status field `ReleaseUUID → InstanceUUID` (`ModuleInstanceStatus`) — surfaced here because the prune-stale-resources spec names it (its kind rename is O1's, its field/label coupling lands with O3's label migration).
- Regenerate CRD bases, `config/rbac/role.yaml`, `zz_generated.deepcopy.go`, `dist/install.yaml`; hand-edit `PROJECT`.
- `// Was:` breadcrumbs at rename sites (D11/D12).

MAJOR (breaking); ships on the `v1.0.0-alpha.N` line (D13). The served CRD `apiVersion` group changes — this is an install-time break (reinstall, not in-place upgrade).

## Capabilities

### New Capabilities

_None._

### Modified Capabilities

- `finalizer-and-deletion`: finalizer key `releases.opmodel.dev/cleanup → opmodel.dev/cleanup`; the capability's `ModuleRelease` prose → `ModuleInstance` (this capability is not claimed by O1/O2, so O3 carries its vocabulary update).
- `prune-stale-resources`: ownership-guard label `module-release.opmodel.dev/uuid → module-instance.opmodel.dev/uuid`; status field `ReleaseUUID → InstanceUUID` (header-renamed requirement `Release UUID persisted on ModuleReleaseStatus` → `Instance UUID persisted on ModuleInstanceStatus`); `ModuleRelease`/`MR` → `ModuleInstance`/`MI`.

(The `inventory-bridge`, `ssa-apply`, and `platform-crd` capabilities change at the implementation/manifest level — group move, label stamping, inventory selectors — but their `openspec/specs/` requirement text names none of the migrated tokens, so they get no spec delta. The group migration is captured in tasks and regenerated manifests.)

## Impact

- **API / group**: `api/v1alpha1/groupversion_info.go` (`+groupName`, `GroupVersion`); `PROJECT`; kustomize bases under `config/crd`, `config/rbac`, `config/default`.
- **RBAC markers**: every `//+kubebuilder:rbac:groups=releases.opmodel.dev` in the three controllers → `opmodel.dev`.
- **Finalizer**: `internal/reconcile/moduleinstance.go` (O1-renamed) `FinalizerName`; consumers in the deletion path.
- **Labels**: `pkg/core/labels.go` constants + values; consumers in `internal/apply/prune.go`, `internal/inventory/*`, `internal/reconcile/moduleinstance.go` (`core.LabelModuleInstanceUUID` reads), `internal/render/*` adapters, SSA label stamping.
- **Generated**: CRD bases (filenames become `opmodel.dev_moduleinstances.yaml` / `opmodel.dev_modulepackages.yaml` / `opmodel.dev_platforms.yaml`), `role.yaml`, `zz_generated.deepcopy.go`, `dist/install.yaml`.
- **Fixtures**: `test/fixtures/**` group/label references in applied manifests and assertions.
- **Cross-slice**: depends on O1 (renamed kinds/finalizer file) and O2 (renamed `ModulePackage` kind) being present on the branch before the group move. Realigns the operator with `library@v1`'s `module-instance.opmodel.dev/*` stamping.
