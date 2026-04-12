# ADR-007: BundleRelease as Orchestrator over ModuleReleases

## Status

Accepted

## Context

The controller needs a way to manage multiple module releases as one higher-level unit. A media stack, for example, might include Jellyfin, qBittorrent, and Radarr as separate modules deployed together.

Two approaches were considered:

1. **BundleRelease as a second apply engine**: the bundle controller renders all resources from all modules and applies them directly, maintaining its own inventory and SSA logic.

2. **BundleRelease as an orchestrator**: the bundle controller evaluates bundle intent, derives desired child `ModuleRelease` objects, and creates/updates them. Each child `ModuleRelease` handles its own render/apply/prune/inventory lifecycle.

Approach 1 duplicates SSA logic, inventory tracking, detailed status handling, and history management across two CRDs.

## Decision

`BundleRelease` is an orchestration layer over child `ModuleRelease` objects. It does not directly render or apply workload resources.

The bundle reconcile flow:

1. Resolve the referenced source artifact from Flux.
2. Evaluate the bundle intent from CUE.
3. Derive desired child `ModuleRelease` specs.
4. Create or update child `ModuleRelease` objects (with owner references back to the bundle).
5. Prune removed child releases when `spec.prune` is true.
6. Aggregate child readiness into bundle status.

Child `ModuleRelease` objects hold detailed resource inventory. `BundleRelease` status includes aggregate inventory metadata (revision, digest, count) and per-module summaries (name, release ref, ready state, digest, inventory count) but does not duplicate all child resource entries.

Bundle failure and remediation remain per-child through `ModuleRelease`. No transactional rollback across children exists in v1alpha1.

## Consequences

**Positive:** No duplication of SSA apply logic, inventory tracking, or detailed per-resource status handling. All of that lives in `ModuleRelease` and is reused by composition.

**Positive:** Each child `ModuleRelease` reconciles independently, making the system easier to reason about and debug. A failure in one child module does not block others.

**Positive:** The CLI can inspect releases at two levels: bundle-level aggregate health, or child `ModuleRelease` for exact resource ownership.

**Negative:** No transactional semantics across children. If one child module fails while another succeeds, the bundle is in a partially applied state. This is accepted for the POC — bundle-wide rollback is deferred as explicitly out of scope.

**Negative:** Bundle status depends on watching child `ModuleRelease` objects, adding watch complexity. Owner references and standard controller-runtime watching patterns mitigate this.

**Trade-off:** `BundleRelease` is intentionally kept as a sketch/placeholder in the POC while `ModuleRelease` is proven end to end. Bundle orchestration semantics (dependency ordering, aggregate readiness, cross-module coordination) are deferred.

Related: [bundle-release-api.md](../docs/design/bundle-release-api.md), [controller-architecture.md](../docs/design/controller-architecture.md), [scope-and-non-goals.md](../docs/design/scope-and-non-goals.md)
