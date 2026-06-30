## Why

Enhancement [0006](../../../../enhancements/0006/) lets the OPM CLI deploy releases by writing the operator's `ModuleInstance` CR directly, then graduate them to operator management via a zero-downtime handoff. For that to be safe, a cluster running the operator must be able to hold CLI-deployed `ModuleInstance` objects **without the operator fighting the CLI for their resources**. Today the operator reconciles every `ModuleInstance` it sees — render, apply, prune — so a CLI-created CR would be immediately taken over. This change adds an explicit ownership marker (`spec.owner`) the operator respects: when an instance is CLI-owned, the operator stays entirely hands-off and only records that it is deferring. It is slice **A4** of 0006 (decision D3, refined by D18/D25), and it produces the CRD the CLI later embeds (B2) and reads/writes (C1).

## What Changes

- **New `spec.owner` field on `ModuleInstance`** — an enum `cli | operator`, optional, `omitempty`, **no CRD default** (matching the repo's existing enum idiom, e.g. `RolloutSpec.Strategy`). A typed `OwnerType` with `OwnerCLI` / `OwnerOperator` constants. Semantically the default is "operator": the reconciler treats absent / empty / `operator` as operator-managed, and only an explicit `owner: cli` triggers the skip.
- **Owner-skip gate at the top of `Reconcile`, before finalizer registration.** When `spec.owner == cli`, the operator performs no render, apply, prune, or deletion cleanup, and — crucially — adds **no** `opmodel.dev/cleanup` finalizer. Placing the gate before finalizer registration is load-bearing: the deletion path prunes `status.inventory`, so a finalizer on a CLI-owned CR would let the operator delete resources the CLI owns. This scopes the existing finalizer-registration behavior to operator-owned instances.
- **Single acknowledgement condition.** On a non-deleting CLI-owned instance the operator sets `Ready: Unknown` with reason `ManagedExternally` (a new `status` reason + a `MarkManagedExternally` helper) and nothing else. It does **not** write `observedGeneration` (claiming an observed generation would assert reconciliation that did not happen), and never touches `status.inventory`, the `lastApplied*` digests, or any other CLI-written status field. The condition write is idempotent: repeated wake-ups (further spec edits, Platform-watch re-enqueues) produce an empty patch diff.
- **Handoff fall-through.** When `spec.owner` flips `cli → operator` (the 0006 handoff), the next reconcile passes the gate and proceeds normally — finalizer added, full reconcile, the `ManagedExternally` condition overwritten by the real `Ready`. Finalizer presence thus tracks operator ownership exactly.
- **Regenerated CRD + DeepCopy + installer.** `config/crd/bases/opmodel.dev_moduleinstances.yaml`, `zz_generated.deepcopy.go`, and `dist/install.yaml`.

The behavior is **additive and backward-compatible**: existing `ModuleInstance` objects carry no `owner`, are treated as operator-managed, and reconcile exactly as before.

## Capabilities

### New Capabilities

- `module-instance-ownership`: the `spec.owner` marker and the operator's behavior for externally (CLI) owned `ModuleInstance` objects — the field shape and default semantics, the owner-skip gate before finalizer registration, the single `ManagedExternally` acknowledgement (and what status it must not write), CLI-owned deletion being a no-op, and the operator's adopt-on-handoff fall-through. The `ManagedExternally` reason constant and `MarkManagedExternally` helper, and the finalizer-registration scoping (operator-owned only), are specified within this capability.

### Modified Capabilities

<!-- None as formal delta files. The owner-skip's interactions with finalizer-and-deletion (finalizer now operator-owned only) and status-conditions (new ManagedExternally reason + helper) are specified inside the new module-instance-ownership capability, in current ModuleInstance terms, rather than as deltas against specs still written for the pre-0002 ModuleRelease type. See design.md § Relationship to existing capabilities. -->

## Impact

- **API:** `api/v1alpha1/moduleinstance_types.go` — new `Owner OwnerType` field + `OwnerType`/`OwnerCLI`/`OwnerOperator`. Regenerated `zz_generated.deepcopy.go` (`task dev:generate`).
- **Reconcile:** `internal/reconcile/moduleinstance.go` — owner-skip gate inserted after the `Get`, before finalizer registration.
- **Status:** `internal/status/conditions.go` — `ManagedExternallyReason` constant + `MarkManagedExternally` helper.
- **Generated manifests:** `config/crd/bases/opmodel.dev_moduleinstances.yaml` + `dist/install.yaml` (`task dev:manifests` + `task operator:installer`). No hand-edits to generated files.
- **RBAC:** none added — `spec.owner` is spec, and `moduleinstances/status` patch is already granted.
- **Tests:** new reconcile unit/integration coverage for the skip path (no finalizer, no apply, `ManagedExternally`, no `observedGeneration`, delete is a no-op) and the handoff fall-through.
- **Downstream (out of scope here, tracked by 0006):** the CLI embeds this CRD for `opm install crds` (B2) and writes `spec.owner: cli` + reads it for its dual-mode behavior (C1). The operator self-publishing its version into `Platform.status.operatorVersion` (0006 D24) is a separate concern, **not** in this slice.
