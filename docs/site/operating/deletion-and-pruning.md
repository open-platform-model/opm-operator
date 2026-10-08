---
title: "Deletion and pruning"
description: "What happens to an instance's resources when it is deleted, and why the default keeps them."
type: explanation
weight: 30
---

<!-- Reads as "About deletion and pruning". This is the page that documents the deletion hazard at its current behaviour, not at any intended behaviour. It covers two events: deleting an instance, and pruning, meaning deleting what a new render no longer produces. Each is covered for both managers. No steps: those are in "Delete an instance safely". Voice: candid, since this is the one place where following a happy path can destroy or strand state.

All three hazard facts still hold in code today:
(1) `spec.prune` has no CRD default, so the operator's finalizer orphans by default.
(2) A CLI-managed ModuleInstance carries no finalizer, so deleting it with kubectl removes the only inventory record and leaves everything running.
(3) The CLI and operator delete paths differ in what gets removed.
Describe what exists only. Enhancement 0012 is a draft: never present its proposals (a shared hold, a changed default) as coming.

Check against: opm-operator/api/v1alpha1/moduleinstance_types.go, opm-operator/internal/reconcile/moduleinstance.go, opm-operator/internal/apply/prune.go, cli/internal/cmd/instance/delete.go, cli/internal/kubernetes/delete.go, cli/internal/inventory/stale.go 
Kubernetes comparison, only if it helps: weave it into the sentence that introduces the concept, or into How it works, never as a section of its own. Researched candidate: Nearest ideas: ownerReferences with the garbage collector (delete the owner and the dependents go), and a finalizer that runs cleanup before its object disappears.

Where the comparison stops: neither the CLI nor the operator sets ownerReferences on anything they apply, so the garbage collector never cascades from a ModuleInstance. The only link from an instance to its resources is the list in the ModuleInstance's `status.inventory`. The labels `module-instance.opmodel.dev/name` and `module-instance.opmodel.dev/uuid` identify resources for display and safety checks, but never decide what gets deleted. The operator's finalizer deletes only when `spec.prune` is true, and leaving the field out means false.

Check against: opm-operator/adr/002-authoritative-inventory-model.md, opm-operator/internal/reconcile/moduleinstance.go, cli/internal/workflow/apply/apply.go, core/src/module_instance.cue -->

## How it works

<!-- Plain words and one diagram. Suggested diagram: two lanes, "CLI-managed" and "operator-managed", from "delete requested" to "what is left running", with the decision points owner, `spec.prune`, and kind is Namespace or CRD.

Check against: cli/internal/cmd/instance/delete.go, opm-operator/internal/reconcile/moduleinstance.go, opm-operator/internal/apply/prune.go -->

### The inventory is the only record

<!-- `status.inventory` on the ModuleInstance holds one entry per applied object: group, kind, namespace, name, `v` (API version) and component, plus revision, digest and count. Both managers write the same shape. The CLI writes it after each successful apply, and moved it there from a per-instance Secret (the first apply after upgrading migrates the Secret). The operator writes it only after a fully successful reconcile. Every delete and prune reads this list and nothing else.

Check against: opm-operator/api/v1alpha1/common_types.go, opm-operator/adr/002-authoritative-inventory-model.md, cli/internal/workflow/apply/apply.go, cli/internal/inventory/legacy.go -->

### Deleting a CLI-managed instance

<!-- `opm instance delete` GETs every inventory entry, deletes the live ones as the user, highest apply weight first, with foreground propagation, and deletes the ModuleInstance last, only when every resource delete succeeded. The operator ignores these instances entirely: no render, apply, prune or finalizer. It only sets Ready=Unknown with reason ManagedExternally. So `kubectl delete moduleinstance` completes at once and leaves every resource running with no record. One exception to "no finalizer": an instance the operator managed before `spec.owner` was set to `cli` still carries `opmodel.dev/cleanup`. The operator removes it when that instance is deleted, and prunes nothing, so this delete completes too. After that, `opm instance delete` answers `instance "<name>" not found`.

Check against: cli/internal/cmd/instance/delete.go, cli/internal/kubernetes/delete.go, cli/internal/workflow/query/status.go, opm-operator/internal/reconcile/moduleinstance.go -->

### Deleting an operator-managed instance

<!-- The operator adds the finalizer `opmodel.dev/cleanup` on the first reconcile. On delete it reads `spec.prune`. If false, or unset, it logs "Prune disabled, orphaning managed resources on deletion" and removes the finalizer, and everything keeps running. If true, it prunes every inventory entry as the impersonated ServiceAccount (`spec.serviceAccountName`, else the manager's `--default-service-account`, else its own identity). It skips Namespaces and CRDs, skips live objects not labelled as OPM-managed, and skips objects whose UUID label names another instance. It keeps every PersistentVolumeClaim unless `spec.dataPolicy` is `Delete`, emits one Normal event with reason ClaimsKept that names them, and still removes the finalizer: a kept claim is not a failure. It keeps the finalizer on any failure. A missing ServiceAccount stalls the delete with reason DeletionSAMissing until the ServiceAccount returns, prune is set to false, or `opm.dev/force-delete-orphan: "true"` is annotated. On a ModuleInstance each of the three takes effect within seconds: the operator watches ServiceAccounts and that annotation. The trigger is the creation of the ServiceAccount, so restore its RBAC first and the ServiceAccount last. In the other order the prune is forbidden, the reason becomes ImpersonationFailed and the next attempt is up to 30 minutes later. A ModulePackage reacts to `spec.prune` at once and to the other two at its next recheck, up to 30 minutes later. Restoring only the ServiceAccount's RBAC (reason ImpersonationFailed) also waits for the recheck on both kinds. `opm instance delete` on such an instance only deletes the ModuleInstance and waits. ModulePackage follows the same path with the same finalizer name.

Check against: opm-operator/internal/reconcile/moduleinstance.go, opm-operator/internal/reconcile/modulepackage.go, opm-operator/internal/apply/prune.go, opm-operator/openspec/specs/finalizer-and-deletion/spec.md, cli/internal/cmd/instance/delete.go -->

### PersistentVolumeClaims are kept

The operator does not delete a PersistentVolumeClaim when it prunes or when an instance is deleted, because deleting a claim deletes the data on its volume. With `spec.prune: true`, a claim that a new render no longer produces stays in the cluster, and deleting the ModuleInstance or ModulePackage leaves its claims in place. Everything else is pruned and deleted as before.

A kept claim is not an error. The object stays `Ready`, a delete completes, and the operator emits one `Normal` event with the reason `ClaimsKept` that names the claims:

```text
Normal  ClaimsKept  Kept 1 PersistentVolumeClaim(s) and the data on them: media/config. They are no longer tracked. Delete one with: kubectl delete pvc <name> -n <namespace>. To let the operator delete claims from now on, set spec.dataPolicy to Delete.
```

After that, nothing tracks the claim. A stale claim leaves `status.inventory` with the other stale entries, and a deleted instance has no inventory. The event names the claim, `kubectl get pvc -n <namespace>` lists it, and `kubectl delete pvc` removes it. The claim keeps its labels. If a later render of the same instance produces a claim of the same name, the operator takes the claim back.

To have the operator delete claims, set `spec.dataPolicy: Delete` on the ModuleInstance or ModulePackage. It applies from then on: it does not reach a claim that was kept earlier. Without `spec.prune` it has no effect, because then the operator deletes nothing.

Four things are not covered:

- **A forced recreate.** With `spec.rollout.forceConflicts: true`, an apply that the API server refuses as a change to an immutable field deletes the object and creates it again. A claim whose `storageClassName` or `accessModes` a new module version changes is deleted this way, and `spec.dataPolicy` does not prevent it. Do not combine `forceConflicts` with claims whose data you need.
- **Claims that a StatefulSet creates.** A StatefulSet creates one claim per replica from its `volumeClaimTemplates`. They are in no inventory, so the operator never deletes them, with or without `spec.dataPolicy: Delete`. Kubernetes keeps them when the StatefulSet is deleted, unless the StatefulSet sets `persistentVolumeClaimRetentionPolicy`.
- **Other storage kinds.** Only a `PersistentVolumeClaim` of the core API group is kept.
- **CLI-managed instances.** The operator does not touch them. The CLI keeps claims too, and its switch is the flag `--delete-data`. One difference remains: after a prune that kept a claim, the CLI still lists the claim in the inventory and the operator does not.

> [!WARNING]
> **The default changed**
>
> Earlier operator releases deleted a tracked PersistentVolumeClaim under `spec.prune: true`, on prune and on delete. An instance that relies on that must now set `spec.dataPolicy: Delete`.

<!-- Check against: opm-operator/internal/apply/prune.go, opm-operator/internal/status/claims.go, opm-operator/api/v1alpha1/common_types.go, opm-operator/adr/020-data-claims-kept-by-default.md, cli/docs/site/diagnostics/kept-volume-claims.md -->

### Pruning when a render drops a resource

<!-- The stale set is the previous inventory minus the new render. The CLI prunes it on every `opm instance apply` unless `--no-prune` is passed. It skips a resource that only moved to a renamed component, skips Namespaces, and deletes everything else, CRDs included, with no label check. It also refuses an empty render that would prune everything unless `--force` is passed. The operator prunes the stale set only when `spec.prune` is true, with the same exclusions and guards as its delete path, kept PersistentVolumeClaims included; a kept stale claim leaves the inventory. So `spec.prune` is one switch for two things: pruning on update and deleting on delete.

Check against: cli/internal/workflow/apply/apply.go, cli/internal/inventory/stale.go, cli/internal/cmd/instance/apply.go, opm-operator/internal/reconcile/moduleinstance.go, opm-operator/openspec/specs/prune-stale-resources/spec.md -->

### Where the two paths differ

<!-- Prose, not a field table. Six differences decide whether a resource is actually removed:
- Default: the CLI always deletes on delete and prunes on apply unless `--no-prune` is passed; the operator does neither unless `spec.prune` is true.
- Namespaces and CRDs: the operator never deletes them. The CLI deletes both on `opm instance delete`, and CRDs (not Namespaces) when pruning on apply.
- Ownership check: the operator re-reads each live object and skips it when its managed-by or UUID label disagrees. The CLI deletes whatever the inventory names.
- Identity: the CLI acts with the user's credentials; the operator with the impersonated ServiceAccount.
- Record: deleting the ModuleInstance ends a CLI-managed instance's record with no cleanup. For an operator-managed instance it runs the finalizer.
- PersistentVolumeClaims: both keep them by default. The CLI's switch is the flag `--delete-data`, the operator's is `spec.dataPolicy: Delete`. After a prune the CLI keeps the claim in the inventory; the operator drops it.

Check against: cli/internal/kubernetes/delete.go, cli/internal/inventory/stale.go, opm-operator/internal/apply/prune.go, opm-operator/internal/reconcile/moduleinstance.go -->

## Why it is built this way

### Why an inventory and not ownerReferences

<!-- The inventory is authoritative because labels can be edited or adopted by other tools, and because a deterministic render lets the operator recompute desired state instead of storing manifests. ownerReferences were not used. Modules render cluster-scoped objects (ClusterRoles, CRDs) that a namespaced ModuleInstance cannot legally own. And an ownerReference garbage-collects its dependent whatever `spec.prune` says; Kubernetes has no reference that does not collect. Rewrite without decision numbers.

Check against: opm-operator/adr/002-authoritative-inventory-model.md, enhancements/0012/01-problem.md -->

### Why the operator keeps resources by default

<!-- Verify: no written rationale for `spec.prune` defaulting to false was found in opm-operator/adr, opm-operator/openspec/specs or cli. The code and the CLI README call it deliberate. Enhancement 0012's open question on the default is a draft and must not be cited as a direction. The author supplies the reason, or the page states the behaviour without one. Candidate framing to confirm with the maintainers: an orphaned Deployment can be deleted later. PersistentVolumeClaims are no longer part of this argument: the operator keeps them even with `spec.prune` (see "PersistentVolumeClaims are kept").

Check against: opm-operator/api/v1alpha1/moduleinstance_types.go, cli/README.md, cli/internal/cmd/instance/delete.go -->

### Why a CLI-managed instance has no finalizer

<!-- No controller runs for a CLI-managed instance, so nothing would ever remove a finalizer. A finalizer with no controller leaves the ModuleInstance stuck in Terminating. The operator's own finalizer would prune resources the CLI owns. So the operator checks `spec.owner` before registering the finalizer. The cost is the kubectl hazard above; the CLI's delete, which removes resources first and the record last, is the safe path.

Check against: opm-operator/internal/reconcile/moduleinstance.go, opm-operator/openspec/specs/module-instance-ownership/spec.md, cli/internal/inventory/cr.go -->

### Why Namespaces and CRDs are never pruned by the operator

<!-- Deleting a Namespace deletes everything inside it, including objects the instance never owned. Deleting a CRD deletes every object of that type in the whole cluster. Both are unrecoverable and reach far beyond the instance, so the operator refuses them unconditionally and leaves them to an administrator. The CLI applies this rule to Namespaces only, and only when pruning on apply.

Check against: opm-operator/adr/011-safety-exclusions-from-pruning.md, opm-operator/internal/apply/prune.go, cli/internal/inventory/stale.go -->

### Why the CLI refuses some deletes and applies

<!-- `opm instance delete` refuses an operator-managed instance when the operator is not ready, because deleting a finalizer-armed ModuleInstance with no controller wedges it in Terminating with its workloads orphaned. `opm instance apply` refuses to start when it cannot patch `moduleinstances/status`, so it never deploys resources it cannot record. `opm operator uninstall` refuses while any ModuleInstance still carries `opmodel.dev/cleanup`.

Check against: cli/internal/cmd/instance/delete.go, cli/internal/inventory/gates.go, cli/internal/operator/uninstall.go -->

## Common mistakes

### Deleting an operator-managed instance keeps its resources unless spec.prune is true

<!-- Readers expect deleting the resource to delete everything it deployed. The finalizer runs, sees no prune, and removes only the ModuleInstance. `opm instance delete` says so ("left running (spec.prune is not set)"); `kubectl delete` says nothing.

Check against: opm-operator/internal/reconcile/moduleinstance.go, cli/internal/cmd/instance/delete.go -->

### Deleting a CLI-managed ModuleInstance with kubectl strands its resources

<!-- The record is deleted at once because there is no finalizer. The resources keep running with no inventory, and the CLI can no longer find them by name. Recovery is re-applying the same instance file, or deleting by label, both covered in "Delete an instance safely".

Check against: opm-operator/internal/reconcile/moduleinstance.go, cli/internal/workflow/query/status.go -->

### Setting spec.prune also turns on pruning on every update

<!-- Readers treat it as a delete-time switch. Setting it to true also makes the operator delete whatever later renders stop producing.

Check against: opm-operator/internal/reconcile/moduleinstance.go -->

### The CLI prunes on every apply unless told not to

<!-- Readers carry the operator's default over to the CLI. `opm instance apply` deletes stale resources unless `--no-prune` is passed, CRDs included.

Check against: cli/internal/cmd/instance/apply.go, cli/internal/workflow/apply/apply.go, cli/internal/inventory/stale.go -->

### A namespace created with --create-namespace belongs to no instance

<!-- `opm instance apply --create-namespace` creates the namespace outside the inventory, so no delete path removes it.

Check against: cli/internal/workflow/apply/apply.go, cli/internal/kubernetes/client.go -->

### Instance labels are not ownership

<!-- `module-instance.opmodel.dev/name` and `module-instance.opmodel.dev/uuid` are on the resources, but only `status.inventory` decides what is deleted. The operator uses the labels only to skip deletes, never to find what to delete.

Check against: opm-operator/adr/002-authoritative-inventory-model.md, opm-operator/internal/apply/prune.go, core/src/module_instance.cue -->

### Uninstalling the operator does not delete instances

<!-- `opm operator uninstall` refuses while any ModuleInstance carries `opmodel.dev/cleanup`, and keeps the CRDs and the operator Namespace. `--remove-finalizers` strips only that finalizer and proceeds, leaving those instances' resources unmanaged. Deleting the operator Deployment by hand has no such check, and later ModuleInstance deletes wedge in Terminating. Verify: the uninstall guard lists ModuleInstances only, while ModulePackages carry the same finalizer.

Check against: cli/internal/cmd/operator/uninstall.go, cli/internal/operator/uninstall.go, opm-operator/internal/reconcile/modulepackage.go -->

## What enforces this

<!-- Each rule with what enforces it. Verify: which badge applies. The template's set is cue, kernel, publish and convention, and none of them names the operator's reconciler, the CRD schema or a CLI command, which is where every rule below lives.
- `spec.prune` defaults to false: CRD schema, a boolean with no default.
- `spec.owner` is `cli` or `operator` only: CRD enum validation.
- A CLI-managed ModuleInstance never gets `opmodel.dev/cleanup`: the operator's reconciler, which checks the owner before registering the finalizer.
- The operator's own instance, the one that deploys the operator, never keeps `opmodel.dev/cleanup` and is never pruned by the operator, when its owner is absent or `operator`: the operator's reconciler, which refuses that instance before registering the finalizer and releases a leftover finalizer without pruning.
- The operator deletes on delete only with `spec.prune`: the operator's reconciler.
- The operator never deletes Namespaces or CRDs, or objects whose labels disagree: the operator's prune.
- The operator deletes a PersistentVolumeClaim only with `spec.dataPolicy: Delete`: the operator's prune. `spec.dataPolicy` is `Keep` or `Delete` only: CRD enum validation.
- An operator-managed delete needs a ready operator: the `opm instance delete` command. Nothing guards `kubectl delete`.
- Apply needs permission to record inventory: `opm instance apply`'s status RBAC check.
- The ModuleInstance goes last on a CLI delete: the `opm instance delete` command.
- Uninstall refuses while ModuleInstances are finalizer-armed: `opm operator uninstall`.
- Never delete a CLI-managed ModuleInstance with kubectl: convention only; nothing enforces it.

Check against: opm-operator/api/v1alpha1/moduleinstance_types.go, opm-operator/internal/reconcile/moduleinstance.go, opm-operator/internal/apply/prune.go, cli/internal/cmd/instance/delete.go, cli/internal/inventory/gates.go, cli/internal/operator/uninstall.go -->
