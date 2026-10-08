## Why

opm-operator#267 keeps PersistentVolumeClaims on prune and on deletion unless `spec.dataPolicy` is `Delete`. Its review found a second path that deletes a claim. With `spec.rollout.forceConflicts: true`, the apply deletes and creates again every object whose update the API server refuses, a claim included (`internal/apply/apply.go`, `opts.Force`; `fluxcd/pkg/ssa` v0.77.0 `manager_apply.go`, `shouldForceApply`). A module version that changes `storageClassName` or `accessModes` of a claim, or lowers its storage request, then deletes the claim and its data, whatever `spec.dataPolicy` says. #267 documented this as an exception. The owner decided on 2026-10-08 to close it before v1.0.0.

## What Changes

- **BREAKING** With `spec.dataPolicy` `Keep` or absent, the forced recreate never deletes a PersistentVolumeClaim of the core API group. The apply checks every rendered claim before it changes anything. When the API server refuses the update of a live claim, the apply stops: the claim is left as it is and no object of the render is applied.
- The refusal is reported as a conflict the user resolves: `Ready=False` with the new reason `ClaimConflict`, and one `Warning` event with reason `ClaimConflict` and action `Apply` that names the claim, the refused field and the ways out.
- With `spec.dataPolicy: Delete`, the forced recreate works for a claim as it does today. For this path the field does not depend on `spec.prune`.
- The forced recreate of every other kind is unchanged. An apply without `forceConflicts` is unchanged.
- ModuleInstance and ModulePackage follow the same rule.
- The texts that #267 wrote to name the exception say the new rule: the `dataPolicy` doc comment on both kinds, the CRDs, `dist/install.yaml`, the operator module's generated CRD data, `docs/site/operating/deletion-and-pruning.md`, the conditions page and ADR-020. The `forceConflicts` doc comment says what the field does.

SemVer: after GA this is a MAJOR change of behaviour (an apply that succeeded now reports a conflict under the default). The beta line ships it as the next `-beta.N`, with `!` in the PR title.

Sections: four, each green on its own (the apply guard, the report, the API texts, the docs).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `ssa-apply`: a forced recreate keeps claims unless data deletion is allowed.
- `prune-stale-resources`: the `spec.dataPolicy` description and its dependence on `spec.prune`.
- `status-conditions`: the `ClaimConflict` reason.
- `events-emission`: the `ClaimConflict` event.
- `modulepackage-reconcile-loop`: ModulePackage follows the same rule.

## Impact

- API types: `ModuleInstanceSpec.DataPolicy`, `ModulePackageSpec.DataPolicy` and `RolloutSpec.ForceConflicts` doc comments only. No field, no marker, no schema change.
- Code: `internal/apply` (`Apply` takes an options struct; a claim check before the staged apply; the resource manager's client refuses to delete a claim unless the apply allows it), `internal/reconcile/moduleinstance.go`, `internal/reconcile/modulepackage.go`, `internal/status/conditions.go`.
- Controllers: ModuleInstance and ModulePackage. Platform and TransformerRegistration are not touched.
- Generated: `config/crd/bases/*`, `dist/install.yaml`, `modules/opm_operator/zz_generated_crds.cue`. The CRD description change lands in the operator module's changelog too.
- Users: an instance with `forceConflicts: true`, no `dataPolicy: Delete`, and a claim whose immutable field a new render changes stops with `ClaimConflict` where it used to recreate the claim.
- Not changed: prune and deletion (#267), other kinds' recreate, the cli, RBAC.
