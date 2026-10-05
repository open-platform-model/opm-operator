## Why

The operator keeps no readable record of which module version it last applied. `status.lastAppliedSourceDigest` holds it only as a hash: for a ModuleInstance it is `sha256(path@version)` (`internal/status/digests.go`, `ModuleSourceDigest`), and for a ModulePackage it is the Flux artifact digest, which does not name a module version at all. A reader of `kubectl get mi -o yaml` cannot tell from status what is running, and a later change that needs the applied version (an upgrade or uninstall step, the parked 0009 hook work) has nothing to read it from.

The owner settled this in the kernel-plan walkthrough (task j5, 2026-10-03): "Operator adds an additive status.lastAppliedVersion (plain text) next to the digest." The ADR-008 half of j5 merged in wave 1 (library#167). This change is the operator half.

## What Changes

- **A new status field, `status.lastAppliedVersion`, on ModuleInstance and ModulePackage.** A plain string, optional, beside `lastAppliedSourceDigest`. It holds the version of the module whose render the operator last applied successfully, as that module declares it in `metadata.version` (bare SemVer, for example `0.1.0`).
- **The render reports the version.** `render.RenderResult` gains `ModuleVersion`. Both kernel renderers (`KernelModuleRenderer` for ModuleInstance, `KernelPackageRenderer` for ModulePackage) read it off the rendered instance's source module (`#module.metadata.version`) through the library's public `opm/schema` paths. One reading for both kinds, so the field means the same thing on each.
- **The reconcile loops write it.** On a successful apply (and prune, when enabled), the deferred status commit sets `lastAppliedVersion` together with the other `lastApplied*` fields. A failed attempt, a panicking attempt, a suspended object and a CLI-owned instance leave it as it was, exactly like the digests.
- **A NoOp fills it once.** An object applied by an operator release without this field is a NoOp on every later reconcile until something changes, so the field would stay empty indefinitely. When the outcome is NoOp and `lastAppliedVersion` is empty, the NoOp commit writes the version the render just reported. A NoOp never changes a recorded value. This is sound because a NoOp means the source digest matches the last apply, and the source digest pins the module version on both kinds.
- **Generated files.** `config/crd/bases`, `dist/install.yaml` and the operator module's generated CRD data (`modules/opm_operator/zz_generated_crds.cue`) are regenerated with the repository tasks, never by hand. The field's doc comment becomes the CRD description and the generated resource reference.

## Classification

**MINOR** after GA (an additive optional status field on `v1alpha1`); during beta it ships as the next `1.0.0-beta.N` under a `feat` PR title. No field is removed or renamed, no-op detection is unchanged (the version is not a no-op input; the source digest already covers it), and older clients ignore the field.

The regenerated `modules/opm_operator/zz_generated_crds.cue` is the forced exception in `AGENTS.md`: a CRD change must regenerate the module's data in the same PR, so the squash commit also lands in the module's changelog and opens a module release PR, which the module's release gate holds until an operator release carrying this `config/` reaches the module.

Complexity (Principle VII): one string field per kind, one result field, one read helper shared by both renderers, and one conditional write in each NoOp commit. The NoOp fill is the only behaviour beyond "set on apply"; without it the field is empty on every instance that existed before the upgrade and has not changed since, which defeats its purpose.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `reconcile-loop-assembly`: a new requirement says both kinds record the applied module version in plain text on a successful apply and fill an empty field on NoOp; "Status always patched" names `lastAppliedVersion` in the NoOp patch set.

## Impact

- API: `api/v1alpha1/moduleinstance_types.go`, `api/v1alpha1/modulepackage_types.go` (new field), `api/v1alpha1/zz_generated.deepcopy.go` (regenerated; a string field leaves it unchanged).
- Render: `internal/render/module.go` (`RenderResult.ModuleVersion`), `internal/render/kernel_module_renderer.go`, `internal/render/kernel_package_renderer.go`, a small read helper beside `internal/render/demand.go`.
- Reconcile: `internal/reconcile/moduleinstance.go` (deferred commit and `commitNoOpStatus`), `internal/reconcile/modulepackage.go` (deferred commit and its inline NoOp branch). Builds on the deferred-commit shape of op-fix-panic-seam (#236): `reconcileAction`, `nextInventory` and the recover branch.
- Generated: `config/crd/bases/opmodel.dev_moduleinstances.yaml`, `config/crd/bases/opmodel.dev_modulepackages.yaml`, `dist/install.yaml`, `modules/opm_operator/zz_generated_crds.cue`.
- Tests: `internal/render` (the read helper), `test/integration/reconcile` (both kernel renderers report the fixture's version), `internal/controller` (envtest: set on apply, unchanged on a failed apply, filled on NoOp, never overwritten by NoOp).
- Downstream: none required. The cli writes `lastApplied*` digests for CLI-owned instances and does not learn this field here; whether the cli records it too is not part of the owner's decision and is left open.
- No enhancement decision backs this change (the owner's j5 answer is a walkthrough decision recorded in ADR-008), so there is no `enhancement.yaml`.
