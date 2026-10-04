## Context

See proposal.md, Why. The rule this change enforces: for the instance that deploys it, the operator never applies, never prunes and never adds `opmodel.dev/cleanup`, whatever that instance's `spec.owner` says, so that re-running install can always repair the operator without the running operator's help.

The relevant code today:

- `ReconcileModuleInstance` (`internal/reconcile/moduleinstance.go`, about lines 105-140) loads the instance, runs the owner-skip gate of 0006:D3 (`mi.Spec.Owner == releasesv1alpha1.OwnerCLI` → `handleCLIOwned`), then registers `opmodel.dev/cleanup` with `addFinalizer` and requeues, then branches to `handleDeletion`, then to `handleSuspend`, then renders (Phase 1, about line 274). Render is where `status.instanceUUID` is first written (Phase 4, `extractInstanceUUID`), and the deferred status commit writes `lastAttempted*`, history and counters on every non-skipped path.
- `handleCLIOwned` (about line 648) returns early for a deleting instance and otherwise patches only the owned conditions to `Ready=Unknown/ManagedExternally`. It never looks at finalizers, so an instance that carried `opmodel.dev/cleanup` while operator-owned keeps it after a flip back to `cli` (in the prototype below, recovery needed the finalizer stripped by hand). This change leaves it so: see "Release the finalizer, prune nothing".
- `handleDeletion` (about line 719) prunes the inventory when `spec.prune` is true. That is the path that killed the operator in the prototype's step 8c.
- The platform-watch index `moduleInstancePlatform` (`internal/controller/moduleinstance_controller.go`, about line 236) excludes CLI-owned and suspended instances only, so a refused own instance is still re-enqueued when the Platform regenerates.
- The CLI never writes conditions or `status.observedGeneration` (`cli/internal/inventory/store.go`, `StatusInput`), and its wait for an operator-owned instance polls until `observedGeneration` reaches the awaited generation (`cli/internal/inventory/reconcile.go`).
- The operator's CRDs are `moduleinstances`, `modulepackages`, `platforms` and `transformerregistrations`, all in group `opmodel.dev` (`config/crd/bases`), the group of `releasesv1alpha1.GroupVersion`.

### Measured: what an operator does to its own instance today

On 2026-10-04 a prototype of the CLI module install ran on a kind cluster against operator `v1.0.0-beta.5`: it rendered the operator as a 19-object module instance, recorded it CLI-owned as `opm-operator` in `opm-operator-system`, and confirmed the operator acknowledged it with `ManagedExternally`. Step 8 then flipped that instance to `spec.owner: operator` with `kubectl patch` (setting `spec.prune: true` in the same patch), which nothing refuses today.

- **8a, shipped RBAC plus the development cluster's workload grant.** The operator set `Ready=False ApplyFailed: CustomResourceDefinition/moduleinstances.opmodel.dev dry-run failed (Forbidden): … cannot patch resource "customresourcedefinitions"`. The dry-run runs before any apply, so nothing changed (the Deployment stayed at generation 1 with the same uid). But the operator had already added `opmodel.dev/cleanup` to its own instance: it fails closed on apply and still arms the finalizer.
- **8b, cluster-admin on the operator's ServiceAccount** (the rights self-management needs: CRDs, ClusterRoles, bind and escalate). `ReconciliationSucceeded` about 40 s later. All 19 objects were re-applied in place (same uids, new resourceVersions; Deployment still generation 1, so no self-restart). `opm-controller` became a co-manager of every object and `app.kubernetes.io/managed-by` flipped to `opm-controller`. Re-running the CLI apply no longer took the instance back: it reported `instance is operator-managed — editing its spec and waiting for the operator` and left `owner: operator`, and a server-side apply of the CRDs without force now conflicted with `opm-controller` on the `managed-by` label.
- **8c, deleting the self-owned instance.** `opm instance delete` waited on the operator's cleanup until a 150 s timeout killed it (exit 124). The operator pruned its own Deployment, ServiceAccount, metrics Service and `opm-operator-manager-role` ClusterRole, then died mid-cleanup. The record stayed in deletion with finalizer `opmodel.dev/cleanup` and `owner: operator`. Left behind: the leader-election Role and RoleBinding, 5 ClusterRoles, and two ClusterRoleBindings pointing at a deleted ClusterRole or a deleted ServiceAccount. An unrelated operator-managed instance was orphaned with its own finalizer armed.
- **Recovery from 8c.** Re-running the CLI apply failed with `timed out waiting for the operator to reconcile generation 3`, because it only edits the spec of an operator-owned record. The only way back was `kubectl patch … remove /metadata/finalizers`, then the CLI apply (`4 created, 15 configured`, healthy, `owner: cli`, `ManagedExternally` again). `opm-controller` stayed a field manager on the CRDs and ClusterRoles as stale co-ownership.
- **Control, instance kept CLI-owned (step 7).** Deleting the CLI-owned operator instance removed 14 objects (the Namespace and 4 CRDs were left by design), after which an operator-managed instance deleted with `--wait=false` wedged in Terminating, as expected with no operator running. Re-running install brought the operator back (`14 created, 5 unchanged`), and the operator then finished the pending cleanup; the wedged instance was gone 18 s after install returned. Re-running install works as the recovery path exactly as long as the operator never owns its own instance.

## Goals / Non-Goals

**Goals:**

- For every instance that deploys this operator, whatever `spec.owner` says, the operator never applies, never prunes and never adds `opmodel.dev/cleanup`, decided before the finalizer would be registered.
- Release a leftover cleanup finalizer on a refused own instance without pruning, so install and uninstall are never blocked by the operator.
- A status a human and the CLI's wait can read: a final `Stalled` verdict at the current generation, not a timeout.

**Non-Goals:**

- Ownership transfer between the CLI and the operator, and any reverse handoff (designed elsewhere).
- The CLI side: refusing to write `owner: operator` on the operator's own instance, and install reclaiming an instance whose owner was flipped. Recovery needs that too; this change only guarantees the operator is not in the way.
- An admission webhook refusing the flip itself. The operator has no webhook today (issue 144 discusses one for another refusal), and a reconcile-time refusal is enough: the rule constrains what the operator does, not what the API accepts.

## Decisions

### Detect from stored fields, before the finalizer

The check is a pure function over the stored object, `isOwnInstance(mi) (signal string, ok bool)`, in a new file `internal/reconcile/owninstance.go`. It runs in `ReconcileModuleInstance` immediately after the owner-skip gate and before `addFinalizer`:

```go
if mi.Spec.Owner == releasesv1alpha1.OwnerCLI {
    return ctrl.Result{}, handleCLIOwned(ctx, params, &mi)
}

// The operator never reconciles the instance that deploys it, whatever its
// owner says: install must be able to repair the operator without it.
if signal, own := isOwnInstance(&mi); own {
    return ctrl.Result{}, handleOwnInstance(ctx, params, &mi, signal)
}

reconcileStart := time.Now()
if !controllerutil.ContainsFinalizer(&mi, FinalizerName) { ... }
```

`handleCLIOwned` is unchanged: a CLI-owned instance, own or not, is left alone exactly as today. As implemented, both gates sit in one helper, `handleNotReconciled`, called at this point in the same order, so that `ReconcileModuleInstance` stays under the linter's cyclomatic-complexity limit of 30.

`handleOwnInstance`:

```go
func handleOwnInstance(ctx, params, mi, signal) error {
    if controllerutil.ContainsFinalizer(mi, FinalizerName) {
        // Released without pruning: an earlier release may have added it.
        if err := removeFinalizer(ctx, params.Client, mi); err != nil {
            return fmt.Errorf("removing finalizer: %w", err)
        }
    }
    if !mi.DeletionTimestamp.IsZero() {
        params.Warnings.Forget(keyOf(mi)) // as the normal deletion path does
        return nil                        // no status on an object being deleted
    }
    already := isSelfManagementRefused(mi) // Ready reason == SelfManagementRefused at this generation
    patcher := patch.NewSerialPatcher(mi, params.Client)
    status.MarkSelfManagementRefused(mi, msg)
    mi.Status.ObservedGeneration = mi.Generation
    if !already {
        params.EventRecorder.Eventf(mi, nil, corev1.EventTypeWarning,
            status.SelfManagementRefusedReason, "Reconcile", "%s", msg)
    }
    return patcher.Patch(ctx, mi,
        patch.WithOwnedConditions{Conditions: []string{
            status.ReadyCondition,
            status.ReconcilingCondition,
            status.StalledCondition,
            status.ModuleResolvedCondition,
            status.DriftedCondition,
        }},
        patch.WithStatusObservedGeneration{})
}
```

`removeFinalizer` returns a not-found error when the object vanished after the finalizer went; the handler ignores not-found as the deletion path does.

The owned-condition list is the one `handleCLIOwned` and `handleSuspend` use. `MarkSelfManagementRefused` deletes `Reconciling`, `ModuleResolved` and `Drifted`, then calls `MarkStalled`: after an earlier adoption (8b) the instance carries `ModuleResolved=True` and possibly `Drifted`, which describe a reconcile the operator now refuses and would read as live next to `Stalled`. Removing them also keeps the re-refusal patch empty, since nothing sets them again.

Reconcile phase impact: Source, Render, Apply and Prune do not run for an own instance. Status writes only `Ready`, `Stalled`, the removal of `Reconciling`, `ModuleResolved` and `Drifted`, and `observedGeneration`. The refusal runs before the suspend check, because that check comes after finalizer registration: an own instance with `spec.suspend: true` is refused, not marked `Suspended`. No metrics are recorded, as for the CLI-owned skip.

### Signals: coordinates, module path, recorded CRDs

**Context**: the refusal must be decided before the finalizer is registered, so detection can read only the stored object: no render, no registry pull. It must catch every instance that can deploy the operator, and it must not catch an unrelated instance.

**Explored**: five signals.

1. Name `opm-operator` in `opm-operator-system`. The CLI's install always records the operator's instance under exactly these coordinates on every cluster. The operator module (opm-operator change `add-operator-module`) states its object names as constants that reproduce the release manifest's (`opm-operator-controller-manager` and friends in `opm-operator-system`) and refuses to render for any other instance name or namespace, because the operator is a cluster singleton: its CRDs and cluster roles do not vary with the instance. The namespace is the one the operator documents as its own (`docs/site/start/install-the-operator.md`). This signal alone covers every instance the published module can render.
2. Module path `opmodel.dev/modules/opm_operator`, any major, compared after stripping `@<major>`. This is the path the operator module will be published under (first-party modules publish as `opmodel.dev/modules/<leaf>`). Against the published module it adds nothing to signal 1: an instance of it under other coordinates fails at render and applies nothing. What it adds is an earlier refusal for that instance (before the finalizer, instead of a render failure after it) and protection against an edited copy republished under the same path with the coordinate constraint removed.
3. Recorded inventory: a CRD of group `opmodel.dev` in `status.inventory`. Catches an instance whose module has another path and no coordinate constraint (a fork, a mirror rewrite, a rename) once anything, the CLI included, has recorded what it deployed. Like signal 2, it protects only against a forked or edited copy of the module. It needs no knowledge of where the operator runs: the operator's API group is a compile-time constant. It holds because only the operator module may ship CRDs of that group (see Decision).
4. The operator's own ServiceAccount or Deployment in the inventory, found through the downward API (`POD_NAMESPACE`, `spec.serviceAccountName`) or a `SelfSubjectReview` at startup.
5. A render-time check: refuse when the rendered set holds the operator's CRDs or its own ServiceAccount.

**Decision** (decided 2026-10-04): signals 1, 2 and 3; any one is enough. Signal 1 is what the owner's rule needs; signals 2 and 3 go beyond it, guarding against forked or edited copies of the module, and all three are kept. Signal 3 rests on a rule: only the operator module may ship CustomResourceDefinitions of the `opmodel.dev` group, so any instance that records one deploys the operator. The CLI change `locate-operator-and-guard-its-instance` recognises the operator's instance by the same three signals.

**Rationale**: 1 covers every instance the CLI install path or a GitOps user can produce from the published module, 2 refuses an edited copy under the published path, and 3 covers a renamed copy with a recorded inventory, all from stored fields before the finalizer, which is the only placement that satisfies "never adds the finalizer" (8a shows a finalizer is armed before any apply is attempted). Signal 4 adds no case 3 misses: the operator module renders its CRDs and its ServiceAccount together, and an inventory that holds the ServiceAccount holds the CRDs. It would also need either a Deployment change in `config/manager/manager.yaml` and in the not-yet-written operator module (coupling this change to that work) or a startup API call. Signal 5 runs after the finalizer is registered and after `instanceUUID` is written, so it cannot meet the rule as written; the one case it alone catches (a fresh instance of a renamed module with no recorded inventory) is a deliberate act equivalent to `kubectl apply` of the operator, and the shipped RBAC already refuses the CRD patch such an apply needs (8a). Principle VII: not added.

**Signal 3 precision**: an entry matches only when its group is `apiextensions.k8s.io`, its kind is `CustomResourceDefinition`, and the part of its name after the first `.` equals `releasesv1alpha1.GroupVersion.Group` exactly. `widgets.example.opmodel.dev` and `widgets.example.opmodel.dev.io` do not match; the first is the case a naive suffix check on `.opmodel.dev` would get wrong.

### Status: Stalled with observedGeneration, unlike ManagedExternally

**Context**: the CLI-owned acknowledgement writes no `observedGeneration`, because the instance is the CLI's.

**Decision**: the refusal sets `Ready=False`, `Stalled=True` (reason `SelfManagementRefused`, via `status.MarkStalled`) and `observedGeneration`.

**Rationale**: an own instance flipped to `operator` is one the CLI's wait treats as operator-owned; with `observedGeneration` at the awaited generation and `Ready=False`, the CLI reports the reason at once instead of `timed out waiting for the operator to reconcile generation N` (the 8c recovery attempt). The CLI writes neither field, so nothing it records is overwritten. kstatus and `kubectl wait` read the same pair.

Message, stable per generation so a re-refusal patches nothing: `this ModuleInstance deploys the operator (<signal>); the operator never applies or prunes its own instance. Set spec.owner to cli`. `<signal>` is one of `name opm-operator in namespace opm-operator-system`, `module opmodel.dev/modules/opm_operator`, `inventory records CustomResourceDefinition <name>`; when several match, the first in that order. No enhancement reference appears in the message (AGENTS.md: none in user-facing strings).

### Release the finalizer, prune nothing

**Decision**: remove `opmodel.dev/cleanup` from a refused own instance (owner absent or `operator`) whatever its prune flag or deletion state; never call `handleDeletion` for it. Leave a CLI-owned own instance alone, finalizer included.

**Rationale**: the finalizer exists so the operator can prune; on its own instance it must never prune, so the finalizer has no job left. Leaving it would wedge deletion on an operator that refuses to act, the 8c failure in another form, and would block `opm operator uninstall` and the reinstall that recovery relies on. Releasing it on a live instance too means a later delete is never blocked.

**Explored**: also releasing it from a CLI-owned own instance, in `handleCLIOwned`. Rejected: it only helps when an older release armed the finalizer on a hand-flipped instance and the instance was flipped back to `cli` before a release with this change reconciled it, `opm operator uninstall --remove-finalizers` already clears that case, and it would break the rule that the operator leaves CLI-owned instances alone.

### Platform watch stays as is

A refused own instance is still in the `moduleInstancePlatform` index, so a Platform regeneration re-enqueues it; the refusal is idempotent and costs one Get and an empty patch. Excluding it from the index would duplicate the detector in the controller package for no behavioural gain.

## Risks / Trade-offs

- [A user's unrelated instance named `opm-operator` in `opm-operator-system` is refused] → that namespace belongs to the operator (`docs/site/start/install-the-operator.md`), and the message names the matched signal, so the cause is visible.
- [A renamed operator module applied fresh, with no recorded inventory, is reconciled until it has an inventory] → after its first apply the inventory names the operator's CRDs and the next reconcile refuses it and releases the finalizer without pruning. The shipped RBAC refuses the CRD patch that first apply needs (8a), so in practice it fails before recording anything. Accepted (see Signals).
- [An older operator release still running during an upgrade may add the finalizer again] → the new release removes it on its first reconcile; nothing in between prunes, since the older release reaches prune only on delete with `spec.prune: true`, which the CLI-owned install never sets.
- [Behaviour change for anyone who made the operator manage itself on purpose] → no supported path produces that today; the refusal message says how to return the instance to the CLI.

## Migration Plan

None. The change ships in the next operator release. Clusters without an own instance see no difference. A cluster where someone flipped an own instance sees it refused, and its leftover finalizer released, on the first reconcile after the upgrade. Rollback: the previous release reconciles such an instance again, as it does today.

## Research & Decisions

### Why the operator, not only the CLI, must refuse

**Context**: a CLI-side guard can stop the CLI from writing `owner: operator` on the operator's instance, but the owner field is writable by anything with RBAC on ModuleInstances.

**Explored**: the prototype's step 8 (8a-8c and the recovery, summarised under Context above); the owner-skip gate, `handleCLIOwned` and `handleDeletion` code paths.

**Decision**: refuse in the reconciler, before the finalizer.

**Rationale**: the flip that caused 8a-8c was a `kubectl patch`, which no CLI refusal sees. Only the reconciler sees every path to an operator-owned own instance.

### Why the operator must never own its own instance at all

**Context**: other GitOps tools hand their own install to themselves after a bootstrap (Flux after `flux bootstrap`, Argo CD managing its own Application).

**Explored**: those patterns; the prototype's step 8.

**Decision**: the operator's own instance is CLI-owned forever (owner decision 2026-10-04), and the operator enforces it.

**Rationale**: an operator that owns its own instance prunes its own Deployment and RBAC when the instance is deleted with `spec.prune: true` and then waits forever on a finalizer only it can clear (8c); it needs applier rights to grant any permission it applies, which is cluster-admin in practice (8a vs 8b); and a broken image cannot render the fix to itself. Flux's own guidance for a broken self-managed install is to re-run the bootstrap command, i.e. an outer layer; keeping the instance CLI-owned makes install that outer layer permanently.
