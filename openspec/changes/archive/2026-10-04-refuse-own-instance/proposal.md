## Why

`opm operator install` is about to deploy the operator as a ModuleInstance of the operator's own module, recorded as CLI-owned and named `opm-operator` in `opm-operator-system`. That instance stays CLI-owned for its whole life: the operator never reconciles the instance that deploys it. The reason is recovery. The operator is the one workload whose failure stops every other instance, so the way to repair it, re-running install, must need nothing from the running operator, whether that operator is healthy, failing or gone.

Today the only thing between the operator and its own instance is the owner-skip gate (0006:D3; `internal/reconcile/moduleinstance.go`, `ReconcileModuleInstance`, the `mi.Spec.Owner == OwnerCLI` check before finalizer registration). `spec.owner` is a plain field: `kubectl patch` can flip it and no frontend refuses that. A prototype of the CLI install on a kind cluster flipped the operator's own instance to `owner: operator` by hand and measured what follows (numbers in design.md): with the shipped RBAC the operator failed its apply but had already added its cleanup finalizer to its own instance; with cluster-admin it adopted all 19 of its own objects and re-running the CLI could no longer take the instance back; deleting the instance with `spec.prune: true` made the operator prune its own Deployment, ServiceAccount, Service and manager ClusterRole and die holding a finalizer only it could clear. Re-running install repaired none of these; only removing the finalizer by hand did.

This change makes the operator itself refuse, so that no write to `spec.owner` can turn the operator against its own install. It must ship before the operator module and the CLI's module install exist, so that no operator release that can see its own instance ever reconciles it. This change cannot enforce that ordering by itself, because the module pins whichever operator release it deploys: the change that adds the operator module and the change that publishes it each carry a gate requiring the pinned operator version to be at least the first operator release that contains this refusal (see Dependencies / gates).

## What Changes

- A new gate in `ReconcileModuleInstance`, after the owner-skip gate and before finalizer registration: an instance that deploys the operator is refused when its `spec.owner` is absent or `operator`. A CLI-owned one is left to the owner-skip gate, unchanged.
- The operator recognises its own instance by three signals, any one of which suffices, all read from the stored object without rendering: the fixed coordinates `opm-operator` in `opm-operator-system`, a module path of `opmodel.dev/modules/opm_operator` in any major, and a recorded `status.inventory` that holds a CustomResourceDefinition of the operator's own API group. design.md justifies each.
- A refused instance gets `Ready=False`, `Stalled=True` with the new reason `SelfManagementRefused`, its `status.observedGeneration`, and one Warning event; `ModuleResolved` and `Drifted` left by an earlier adoption are removed. Nothing is rendered, applied or pruned; `status.inventory`, the `lastApplied*` digests and `instanceUUID` are not written.
- When a refused own instance (owner absent or `operator`) already carries `opmodel.dev/cleanup` (an older operator release added it), the operator removes it without pruning, during deletion or not, so install and uninstall are never blocked on a finalizer only a running operator could clear. A CLI-owned own instance is not touched; a leftover finalizer there is cleared by `opm operator uninstall --remove-finalizers`, as for any other instance.
- The `ModuleInstanceSpec.Owner` doc comment, the generated CRD description, the operator-conditions page, the deletion-and-pruning page and the install page say that the operator's own instance is never reconciled.

Release class: MINOR after GA (a new refusal and status reason, no API schema change); during beta it ships as the next `1.0.0-beta.N`. Not breaking: no supported install path creates an operator-owned instance of the operator today.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `module-instance-ownership`: adds the requirements that the operator recognises its own instance, never reconciles it whatever its owner, and releases a leftover cleanup finalizer on it without pruning; narrows the empty-owner, operator-managed and handoff scenarios so they exclude the operator's own instance.

## Impact

- `internal/reconcile/moduleinstance.go`: the new gate in `ReconcileModuleInstance`, with its handler `handleOwnInstance`; `handleCLIOwned` is unchanged; a new small file `internal/reconcile/owninstance.go` for the detector.
- `internal/status/conditions.go`: `SelfManagementRefusedReason` and a `MarkSelfManagementRefused` helper.
- `api/v1alpha1/moduleinstance_types.go`: doc comment only, so `config/crd/bases/opmodel.dev_moduleinstances.yaml` and the generated resource reference change; no schema change.
- `docs/site/diagnostics/operator-conditions.md`, `docs/site/operating/deletion-and-pruning.md`, `docs/site/start/install-the-operator.md`.
- No RBAC, manifest or dependency change. The CLI is unaffected: it never writes an operator-owned instance of the operator.
- Testing: unit and envtest integration tests only. The local kind cluster has no internet egress, so the e2e suite is not part of this change's bar.

## Dependencies / gates

Gate: none. This change depends on no other change in any repo. It needs neither the operator module nor the CLI's module install: the refusal keys on fixed names and a fixed module path, not on a published module, and it is inert on a cluster that has no such instance.

Downstream gate this change imposes: the opm-operator changes `add-operator-module` (when it pins the operator version the module deploys) and `release-operator-module` (before the first module release is published) MUST require that pinned operator version to be at least the first operator release containing this change. Without that, the first module release could deploy an operator that adopts its own instance after a hand-flipped `spec.owner`.

Out of scope:

- Ownership transfer between the CLI and the operator, in either direction (designed elsewhere). Whatever design adds it inherits this refusal, because it holds whatever the owner field says.
- The CLI side: refusing to write `owner: operator` on the operator's own instance, and install reclaiming an instance whose owner was flipped by hand. This change only guarantees the operator is not in the way.
- The operator module itself, its release train, and install from a registry.
