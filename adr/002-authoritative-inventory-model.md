# ADR-002: Authoritative Inventory Model

## Status

Accepted

## Context

The controller needs to track which Kubernetes resources each release currently owns. This is essential for pruning stale resources when the desired state changes and for understanding what the controller believes it manages.

Two primary models exist for tracking ownership:

1. **Label-based ownership**: resources are owned if they carry specific labels (e.g., `app.kubernetes.io/managed-by`). This is simple but fragile — labels can be manually edited, mutated by other tools, or absent on partially migrated objects.

2. **Inventory-based ownership**: the controller maintains an explicit inventory of owned resources in the CR status. Prune eligibility is determined by comparing previous inventory against current desired state.

Helm uses a manifest-storage approach where full rendered manifests are persisted as release state. Because OPM's CUE rendering is deterministic for the same source and config inputs, the controller can recompute desired state from inputs rather than storing full manifests.

## Decision

`status.inventory` is the sole authoritative source of truth for resource ownership and prune decisions. Labels are supportive metadata for observability, not the prune authority.

The inventory stores ownership entries only:

- `group`, `kind`, `namespace`, `name` — Kubernetes identity
- `v` — API version (informative, excluded from ownership equality)
- `component` — OPM component identity

The inventory also carries lightweight metadata:

- `revision` — monotonic inventory revision counter
- `digest` — digest of the serialized inventory entries
- `count` — number of owned resources

The inventory does not store raw values, source metadata, remediation counters, rendered manifests, or reconcile history. Those live in separate top-level status fields.

`status.inventory` is only replaced after a fully successful reconcile (render + apply + prune if enabled). Failed attempts preserve the previous successful inventory.

## Consequences

**Positive:** Pruning is explicit, safe, and explainable. The controller can determine exactly what it owns without querying live cluster labels. The inventory is resilient to label mutation by other tools or manual edits.

**Positive:** Deterministic CUE rendering means the controller can recompute desired state cheaply, so inventory only needs to track ownership — not full manifests. This keeps status compact compared to Helm's approach.

**Positive:** The same inventory shape is shared between the CLI and the controller, enabling future CLI introspection of controller-managed releases.

**Negative:** Inventory must be kept in sync with actual cluster state. If the controller crashes between applying resources and updating inventory, the next reconcile must handle the gap. The deterministic render property mitigates this because the controller recomputes desired state on each reconcile.

**Trade-off:** Labels remain valuable for human observability and `kubectl` filtering, but they cannot be relied upon as the sole input for automated prune decisions.

Related: [module-release-api.md](../docs/design/module-release-api.md), [ssa-ownership-and-drift-policy.md](../docs/design/ssa-ownership-and-drift-policy.md), [controller-architecture.md](../docs/design/controller-architecture.md)
