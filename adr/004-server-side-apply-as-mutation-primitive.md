# ADR-004: Server-Side Apply as Mutation Primitive

## Status

Accepted

## Context

The controller needs a mechanism to create and update Kubernetes resources rendered from CUE modules. The main options are:

1. **Client-side apply / patch**: the controller computes diffs and sends patches. This requires the controller to manage three-way merge logic and does not provide native field ownership semantics.

2. **Server-Side Apply (SSA)**: the controller declares desired state and the Kubernetes API server resolves field ownership and conflicts. SSA provides structured field manager identity, conflict detection, and ownership tracking as first-class API features.

Because OPM rendering is declarative and deterministic, SSA is a natural fit — the controller declares what it wants, and the API server handles the rest.

## Decision

All managed resource mutations use Kubernetes Server-Side Apply. The controller uses `opm-controller` as the field manager name, distinguishing its mutations from those made by the CLI (`opm-cli`), manual users (`kubectl`), or other tools.

Key policy decisions:

- **Force-conflicts is opt-in**: by default, the controller fails safely if another field manager owns a conflicting field (`force: false`). Users may set `spec.rollout.forceConflicts: true` to explicitly allow the controller to take ownership of conflicting fields.

- **Staged apply ordering**: CRDs and Namespaces are applied first (stage 1), followed by all other resources (stage 2). This uses Flux `ssa.ResourceManager` staged apply logic to avoid race conditions where a custom resource is applied before its CRD is registered.

- **Immutable field handling**: if SSA encounters an immutable field change (e.g., StatefulSet `volumeClaimTemplates`), the controller marks the reconcile as stalled. It does not automatically delete and recreate the resource, because that would destroy attached PersistentVolumeClaims.

## Consequences

**Positive:** Reconciliation is declarative, retry-safe, and aligned with Kubernetes field ownership semantics. The controller does not need to implement three-way merge logic. Field manager identity provides clear audit trails for who changed what.

**Positive:** The fail-safe default for conflicts prevents silent overwriting of fields managed by humans or other operators. This avoids a class of outages caused by configuration tools fighting over the same fields.

**Positive:** Staged apply ordering prevents CRD registration race conditions that are common in batch-apply scenarios.

**Negative:** The conservative conflict default means that if another tool manages a field OPM also wants to set, the reconcile fails until the user either resolves the conflict or enables `forceConflicts`. This is intentional — it surfaces real conflicts rather than hiding them.

**Negative:** Immutable field failures require manual intervention (deleting the resource so the controller can recreate it). Automatic delete-and-recreate was rejected because it risks catastrophic data loss (e.g., StatefulSet PVCs).

**Trade-off:** The controller depends on Flux `ssa.ResourceManager` for staged apply and changeset computation. This is acceptable because the Flux SSA package is well-tested and avoids reimplementing resource ordering logic.

Related: [ssa-ownership-and-drift-policy.md](../docs/design/ssa-ownership-and-drift-policy.md), [module-release-reconcile-loop.md](../docs/design/module-release-reconcile-loop.md)
