# ADR-011: Safety Exclusions from Automatic Pruning

## Status

Accepted

## Context

The controller supports automatic pruning of stale owned resources when `spec.prune=true`. Pruning deletes resources that were previously in the ownership inventory but are no longer part of the current desired state.

Two resource kinds pose catastrophic risk if automatically deleted:

- **Namespaces**: deleting a Namespace cascades to all resources inside it, including resources the release does not own. A module that previously rendered a Namespace but no longer does would trigger deletion of the entire Namespace and everything in it.

- **CustomResourceDefinitions**: deleting a CRD deletes all instances of that CRD globally across the cluster. A module that previously rendered a CRD would trigger cluster-wide destruction of all custom resources of that type.

Both of these are highly destructive, irreversible at the resource level, and affect far more state than the release itself owns.

## Decision

The controller never automatically prunes Namespaces or CustomResourceDefinitions, regardless of the `spec.prune` setting.

Even if a resource of kind `Namespace` or `CustomResourceDefinition` is present in the current `status.inventory` and absent from the newly rendered desired state, the controller skips it during the prune phase.

These resources require explicit manual deletion by an administrator when they are no longer needed.

## Consequences

**Positive:** Prevents catastrophic cascading deletes. An operator cannot accidentally destroy an entire Namespace or all instances of a CRD by removing a component from a module.

**Positive:** The safety exclusion is simple and unconditional in v1alpha1 — no configuration knobs or edge cases to reason about.

**Negative:** Stale Namespaces and CRDs accumulate in the cluster until an administrator manually deletes them. The controller's inventory will continue to list them as owned even though they are no longer in the desired state, until the next successful reconcile after manual deletion.

**Trade-off:** A future version could introduce opt-in force-prune for these kinds, but the v1alpha1 position is that the risk of accidental destruction outweighs the inconvenience of manual cleanup.

Related: [ssa-ownership-and-drift-policy.md](../docs/design/ssa-ownership-and-drift-policy.md)
