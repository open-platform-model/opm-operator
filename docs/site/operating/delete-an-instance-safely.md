---
title: "Delete an instance safely"
description: "Remove an instance without orphaning the resources it created."
type: how-to
weight: 22
---

<!-- One sentence: this removes an instance so that its resources are actually deleted, or kept on purpose, whichever of the two managers owns it. You need it because the two delete differently, and the operator's default leaves every resource running. Voice: candid, stating the hazard where it applies, not in a warning box at the end.

The three facts this page turns into steps, all still true in code: `spec.prune` has no CRD default, so the operator's `opmodel.dev/cleanup` finalizer orphans by default. A ModuleInstance with `spec.owner: cli` carries no finalizer, so deleting it with kubectl removes the only inventory record and leaves the resources running. And the CLI and operator delete paths differ in what they actually remove.

Check against: opm-operator/api/v1alpha1/moduleinstance_types.go, opm-operator/internal/reconcile/moduleinstance.go, cli/internal/cmd/instance/delete.go, cli/internal/kubernetes/delete.go -->

## Before you begin

<!-- The state the reader must already be in, as links:
- The opm CLI configured, from "Deploy a module with the CLI", with kubectl access to the instance's namespace. For a CLI-managed instance, that access must also cover every resource it created, because the CLI deletes them as you.
- For an operator-managed instance, the operator installed, from "Install the operator".
- "Deletion and pruning" explains why the paths differ. This page only walks them.

Check against: cli/internal/cmd/instance/delete.go, cli/internal/inventory/ownership.go -->

## Steps

1. Find out who manages the instance.
   <!-- Run `opm instance list -n <namespace>` and read the OWNER column (`cli` or `operator`; an empty `spec.owner` displays as `operator`). `opm instance status <name> -n <namespace>` prints the same as its "Owner:" line. With kubectl: `kubectl get moduleinstance <name> -n <namespace> -o jsonpath='{.spec.owner}'`, where empty output means operator-managed. Every later step forks on this answer.
   Check against: cli/internal/workflow/query/list.go, cli/internal/kubernetes/status.go, cli/internal/inventory/ownership.go, opm-operator/api/v1alpha1/moduleinstance_types.go -->
2. Preview the delete.
   <!-- Run `opm instance delete <name> -n <namespace> --dry-run`. The argument may also be an instance.cue path or the instance UUID. If CLI-managed, it lists every live resource from `status.inventory` and ends "dry run complete: <n> resources would be deleted". If operator-managed, it prints either "ModuleInstance <name> would be deleted and the operator would prune its <n> tracked resource(s)" or "...its <n> tracked resource(s) would be left running (spec.prune is not set)". Only what `status.inventory` lists is ever touched.
   Check against: cli/internal/cmd/instance/delete.go, cli/internal/workflow/query/status.go -->
3. Check the preview for Namespaces and CRDs.
   <!-- If CLI-managed and the list includes a Namespace or a CustomResourceDefinition, `opm instance delete` deletes it too. The CLI delete path has no exclusion, so this deletes everything in that Namespace, or every object of that CRD cluster-wide. Verify: whether that is intended; `cli/internal/kubernetes/delete.go` has no exclusion, while the operator's prune skips both kinds. If operator-managed, the operator never deletes Namespace or CRD entries, even with `spec.prune` true. Plan to remove them yourself in step 7. It also keeps every PersistentVolumeClaim unless `spec.dataPolicy` is `Delete`: decide in step 4 whether the data goes with the instance.
   Check against: cli/internal/kubernetes/delete.go, opm-operator/internal/apply/prune.go, opm-operator/adr/011-safety-exclusions-from-pruning.md -->
4. Set spec.prune on an operator-managed instance.
   <!-- If operator-managed and you want its resources removed, run `kubectl patch moduleinstance <name> -n <namespace> --type=merge -p '{"spec":{"prune":true}}'` before deleting. The field has no default and the CLI never writes it, so unset means the finalizer removes only the ModuleInstance. The spec change also triggers a reconcile that prunes any stale resources, because `spec.prune` governs pruning on update too. If you want the resources kept, leave the field unset. PersistentVolumeClaims are kept even with `spec.prune` true. To delete them and the data on them with the instance, also set `spec.dataPolicy` to `Delete`: `kubectl patch moduleinstance <name> -n <namespace> --type=merge -p '{"spec":{"prune":true,"dataPolicy":"Delete"}}'`. Claims that a StatefulSet created from `volumeClaimTemplates` are never deleted by the operator. If CLI-managed, skip this: neither field has an effect there, and the CLI flag `--delete-data` decides.
   Check against: opm-operator/internal/reconcile/moduleinstance.go, cli/internal/inventory/record.go, cli/README.md -->
5. Confirm the operator is running.
   <!-- If operator-managed, run `kubectl -n opm-operator-system rollout status deployment/opm-operator-controller-manager`. `opm instance delete` refuses without a ready operator: "the opm operator is not ready (<pending>) — instance "<name>" is operator-managed, and deleting its ModuleInstance now would wedge it in Terminating on the opmodel.dev/cleanup finalizer with its workloads orphaned; install or repair it with 'opm operator install', then retry". It has no bypass flag. `kubectl delete` has no such check and wedges the ModuleInstance.
   Check against: cli/internal/cmd/instance/delete.go, cli/internal/operator/ready.go -->
6. Delete the instance with opm.
   <!-- Run `opm instance delete <name> -n <namespace>`. It prompts `[y/N]`, and `--force` skips the prompt. If CLI-managed, it deletes each live resource, highest apply weight first with foreground propagation, then the ModuleInstance last, and prints "Instance deleted". On a partial failure it keeps the ModuleInstance so a re-run retries the rest. If operator-managed, it deletes the ModuleInstance, waits up to `--timeout` (default 5m) for the finalizer to finish, then prints "Instance deleted — operator pruned <n> resources" or "ModuleInstance deleted — <n> resource(s) left running".
   Never delete a CLI-managed instance with `kubectl delete moduleinstance`. The record disappears at once, the resources keep running, and `opm instance delete` then reports `instance "<name>" not found in namespace "<namespace>"`.
   Check against: cli/internal/cmd/instance/delete.go, cli/internal/kubernetes/delete.go, cli/internal/kubernetes/errors.go, cli/internal/inventory/reconcile.go -->
7. Remove what the delete leaves behind.
   <!-- If the namespace was created by `opm instance apply --create-namespace`, delete it with `kubectl delete namespace <namespace>`. It was never in the inventory. If operator-managed, delete any Namespace or CRD from step 3 yourself. Unless `spec.dataPolicy` was `Delete`, the PersistentVolumeClaims are still there: the delete left a Normal event with reason ClaimsKept that names them (`kubectl get events -n <namespace> --field-selector reason=ClaimsKept`), and `kubectl get pvc -n <namespace>` lists them (Verify: whether a rendered claim carries `module-instance.opmodel.dev/name`, so that the list can be filtered by instance); remove one with `kubectl delete pvc <claim> -n <namespace>`. If you left `spec.prune` unset, the resources are still running on purpose. If a CLI-managed record was already deleted with kubectl, re-run `opm instance apply` with the same instance file. The first-apply existence check accepts live objects that carry an OPM `app.kubernetes.io/managed-by` label, and apply writes a fresh record, so `opm instance delete` works again. Verify: this recovery end to end. The fallback is `kubectl delete <kinds> -n <namespace> -l module-instance.opmodel.dev/name=<name>`. Verify: that every rendered kind carries that label; cluster-scoped kinds need a command without `-n`.
   Check against: cli/internal/workflow/apply/apply.go, cli/internal/inventory/stale.go, cli/internal/kubernetes/client.go, core/src/transformer.cue, core/src/module_instance.cue -->
8. Release a delete stuck in Terminating.

   This step is for an operator-managed instance. With `spec.prune: true` the operator deletes the instance's resources and keeps the ModuleInstance until they are gone, so a delete takes at least a few seconds and can take as long as the slowest Pod needs to stop. When it takes longer than you expect, read the `Ready` condition:

   ```sh
   kubectl get moduleinstance <name> -n <namespace> -o jsonpath='{.status.conditions[?(@.type=="Ready")]}'
   ```

   `opm instance delete` waits for the same thing. Its timeout message says that the operator's `opmodel.dev/cleanup` finalizer may still be pruning workloads.

   **`DeletionInProgress`.** The operator deleted the instance's resources and waits until they are gone. The message names what is left. There is nothing to do: a Deployment is gone when its Pods have stopped.

   **`DeletionBlocked`.** A resource has been terminating for more than 10 minutes. The message names it and the finalizer that holds it. The operator keeps checking once a minute and never gives up on its own. You have two ways out:

   - Remove what holds the resource. When the message says that the resource waits for its dependents, find the Pod or other dependent that cannot stop, for example a Pod on a node that is gone, and delete it. When the message names another finalizer, fix or remove the controller that owns that finalizer.
   - Let the instance go and leave the resources as they are:

     ```sh
     kubectl patch moduleinstance <name> -n <namespace> --type=merge -p '{"spec":{"prune":false}}'
     ```

     The operator then removes its finalizer within seconds. The resources that were terminating stay terminating, and nothing tracks them any more.

   The annotation `opm.dev/force-delete-orphan` does not release this wait. It is for a missing ServiceAccount only.

   **`DeletionSAMissing` or `ImpersonationFailed`.** The ServiceAccount the operator deletes as, or its rights, went away before the operator had deleted everything. For `DeletionSAMissing`, do one of three things:

   - Restore the ServiceAccount and its RBAC. Create the RBAC first and the ServiceAccount last: the creation of the ServiceAccount is what the operator reacts to. In the other order the reason becomes `ImpersonationFailed`.
   - Set `spec.prune` to false, with the command above. The resources are left running.
   - Run `kubectl annotate moduleinstance <name> -n <namespace> opm.dev/force-delete-orphan=true`. The resources are left running and the operator writes an `OrphanedOnDeletion` event. Only the literal value `true` counts.

   Each of the three releases a ModuleInstance within seconds. A ModulePackage picks up the restored ServiceAccount or the annotation at its next recheck, up to 30 minutes later. For `ImpersonationFailed`, fix the ServiceAccount's RBAC; the operator tries again at its next recheck, up to 30 minutes later.

   To avoid both, delete the instance first and its ServiceAccount after it. The order matters only until the operator has sent every delete. When the ServiceAccount or its rights are removed while the operator only waits (the reason is `DeletionInProgress` or `DeletionBlocked`), it lets the instance go and writes a `Warning` event with the reason `DeletionUnconfirmed`: the operator could not check that the resources are gone, so check the namespace for leftovers yourself.

   An instance with `spec.owner: cli` never waits on the finalizer: the operator releases a leftover `opmodel.dev/cleanup` on delete and prunes nothing.

   <!-- Check against: opm-operator/internal/reconcile/deletion.go (runDeletionCleanup, awaitGone, actOnLostIdentity), opm-operator/internal/status/deletion.go, opm-operator/internal/reconcile/moduleinstance.go, opm-operator/api/v1alpha1/common_types.go, cli/internal/inventory/reconcile.go -->

## Check that it worked

<!-- Run `opm instance list -n <namespace>`: the instance is gone. Then run `kubectl get all -n <namespace> -l module-instance.opmodel.dev/name=<name>`. It prints "No resources found" when the resources were deleted, and lists them when you kept them on purpose. `get all` misses ConfigMaps, Secrets, PVCs and cluster-scoped kinds, so name those kinds explicitly. Verify: the label is on every rendered object.

Check against: cli/internal/workflow/query/list.go, core/src/transformer.cue -->

## Related

<!-- The reference section "Operator Reference" (`spec.owner`, `spec.prune`, `spec.serviceAccountName`, the `opm.dev/force-delete-orphan` annotation), the reference page "Operator conditions" (DeletionSAMissing, ImpersonationFailed, OrphanedOnDeletion), and the concept pages "Deletion and pruning" and "Who owns an instance".

Check against: opm-operator/api/v1alpha1/moduleinstance_types.go, opm-operator/api/v1alpha1/common_types.go, opm-operator/internal/status/conditions.go -->
