# ADR-020: PersistentVolumeClaims Are Kept by Default

## Status

Accepted

## Context

With `spec.prune: true` the controller deletes what an instance no longer renders, and every tracked object when the ModuleInstance or ModulePackage is deleted. ADR-011 exempts Namespaces and CustomResourceDefinitions, because deleting either destroys far more than the release owns.

A PersistentVolumeClaim was pruned like any other object. Deleting a claim deletes the data on its volume under the usual reclaim policy, and nothing in the controller or in Kubernetes brings that data back. A module change that renames a volume, or a deleted ModuleInstance, was enough to lose it.

The CLI stopped deleting claims by default: `opm instance delete` and the prune of apply keep them unless `--delete-data` is passed. The two managers then differed, and a claim that a CLI prune kept stayed in an inventory that the controller could prune after a change of `spec.owner`.

Unlike a Namespace or a CRD, a claim is often meant to go with its instance: a test environment, a cache. An unconditional exclusion as in ADR-011 would leave no way to ask for that.

## Decision

The controller keeps every PersistentVolumeClaim of the core API group that it would otherwise delete, on the prune of stale resources and on deletion cleanup, for ModuleInstance and ModulePackage alike. The claim and its labels are left unchanged.

One optional field opts out: `spec.dataPolicy`, an enum with the values `Keep` and `Delete`. An absent value means `Keep`. `Delete` lets the controller delete claims under `spec.prune`; without `spec.prune` it has no effect, and the API server accepts the pair. The CRD sets no default, so an object stored before the field existed is protected as it is.

The check lives in the one prune function behind every delete the controller makes, and its option protects at the zero value, so a caller cannot forget it.

A kept claim is not a failure. The reconcile ends Ready, deletion cleanup removes the finalizer, and one Normal event with reason `ClaimsKept` names the claims. A stale claim that the prune keeps leaves `status.inventory` with the rest of the stale set, as a skipped Namespace or CRD does: the recorded inventory is the rendered set. From then on nothing tracks the claim.

Two other shapes for the field were considered. A boolean `spec.deleteData` would share its name with the CLI flag, but a boolean cannot grow a third policy. A struct `spec.persistentVolumeClaimRetentionPolicy` with `whenPruned` and `whenDeleted`, after the StatefulSet field of that name, would set prune and deletion apart, a need nobody has stated. The owner chose the enum.

Keeping the kept claim in the inventory, as the CLI does, was also considered. It would make the stored inventory differ from the rendered one on every reconcile, put a claim that no workload mounts into the health judgement, and leave a record that makes the claim deletable by whoever manages the instance next.

## Consequences

**Positive:** Setting `spec.prune` no longer puts data at risk. The destructive behaviour has to be asked for by name, per object.

**Positive:** The controller and the CLI agree on the default, and a claim that a CLI prune kept is not deleted when the controller takes the instance over.

**Negative:** This changes a default. An instance that relied on the controller to delete claims must set `spec.dataPolicy: Delete`.

**Negative:** Kept claims accumulate and hold storage until someone deletes them. The only record is the `ClaimsKept` event, which expires, and the instance labels that stay on the claim. `spec.dataPolicy: Delete` does not reach a claim that was kept earlier, because no inventory lists it.

**Negative:** The controller names the policy `spec.dataPolicy` and the CLI names its flag `--delete-data`. Documentation has to name both.

**Trade-off:** Only `PersistentVolumeClaim` in the core group is kept. A PersistentVolume, a VolumeSnapshot or a claim-like custom resource is pruned as before. Claims that a StatefulSet creates from its `volumeClaimTemplates` are in no inventory, so the controller never deletes them, with or without the field.

Related: [ADR-011](011-safety-exclusions-from-pruning.md), [ADR-002](002-authoritative-inventory-model.md)
