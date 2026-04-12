# ADR-003: ModuleRelease as Primary Reconciliation Unit

## Status

Accepted

## Context

The controller needs a primary CRD that represents a single deployable unit. Two candidate resources exist in the design: `ModuleRelease` (a single CUE module evaluated and applied as one release) and `BundleRelease` (a higher-level grouping of multiple modules).

Building both CRDs as independent apply engines would duplicate SSA logic, inventory tracking, status handling, and history management. The architecture needs a clear primary unit where detailed reconciliation lives, with any higher-level orchestration layered on top.

## Decision

`ModuleRelease` is the primary detailed reconciliation unit. It is the CRD responsible for:

- referencing a Flux `OCIRepository` source
- evaluating a CUE module with release values
- rendering desired Kubernetes resources
- applying resources with server-side apply
- pruning stale owned resources
- owning detailed digests, conditions, inventory, and bounded history

The `ModuleRelease` API lives in `releases.opmodel.dev/v1alpha1` and includes spec fields for `suspend`, `sourceRef`, `module.path`, `values`, `prune`, `serviceAccountName`, and `rollout`.

The status model captures `observedGeneration`, `conditions`, `source`, `lastAttempted*` and `lastApplied*` digests, `failureCounters`, `inventory`, and `history`.

`BundleRelease` is a separate orchestration CRD that creates and manages child `ModuleRelease` objects (see ADR-007) rather than implementing its own apply path.

## Consequences

**Positive:** The reconciliation contract is small and concrete. All detailed resource lifecycle logic (render, apply, prune, inventory, status) lives in one place. This makes the controller easier to implement, test, and debug.

**Positive:** `BundleRelease` can be built as a lightweight orchestrator without duplicating core reconciliation logic. The two CRDs compose naturally.

**Positive:** The CLI and controller share the same `ModuleRelease` status and inventory contract, enabling future CLI introspection of controller-managed releases.

**Negative:** `ModuleRelease` carries significant status complexity (digests, inventory, history, conditions). This is a deliberate choice: the status is the operational ledger, and compactness matters less than operational clarity.

**Trade-off:** `BundleRelease` depends on `ModuleRelease` being proven first. Bundle orchestration is deferred until the primary reconciliation unit is working end to end.

Related: [module-release-api.md](../docs/design/module-release-api.md), [controller-architecture.md](../docs/design/controller-architecture.md), [scope-and-non-goals.md](../docs/design/scope-and-non-goals.md)
