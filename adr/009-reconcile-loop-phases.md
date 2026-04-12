# ADR-009: Reconcile Loop Phases

## Status

Accepted

## Context

The `ModuleRelease` reconcile loop spans multiple concerns: source resolution, artifact fetching, CUE evaluation, resource application, pruning, and status updates. These concerns interact in specific ways — for example, prune must never run after a failed apply, and inventory must only update after full success.

A monolithic reconcile function would make it difficult to:

- attribute failures to specific phases
- test phases in isolation
- enforce ordering invariants (apply before prune, success before inventory update)
- classify failures correctly (soft-blocked, transient, stalled)

The reconcile loop needs explicit structure with clear phase boundaries.

## Decision

The reconcile loop is divided into seven explicit phases:

**Phase 0 — Load and preflight**: fetch the `ModuleRelease`, exit early if suspended or being deleted, create patching helpers, initialize reconcile-local state.

**Phase 1 — Resolve source**: resolve the referenced `OCIRepository`, validate its readiness, read artifact revision/digest/URL. Soft-block if source is not ready. Stall on invalid source reference.

**Phase 2 — Fetch and unpack artifact**: fetch the resolved artifact, recover the CUE module payload, validate module layout (presence of `cue.mod`, expected entrypoints). Stall on invalid content.

**Phase 3 — Render desired state**: normalize input values, evaluate the CUE module, render desired Kubernetes objects, build desired inventory entries, compute source/config/render/inventory digests. Stall on CUE evaluation failure.

**Phase 4 — Plan actions**: compare desired inventory with current `status.inventory`, compute stale set, detect no-op reconciles. If no-op, update observed generation and finalize without apply.

**Phase 5 — Apply desired resources**: apply with SSA using staged ordering (CRDs/Namespaces first, then other resources). If apply fails, record the failure and stop — do not proceed to prune.

**Phase 6 — Prune stale resources**: if `spec.prune=true` and apply succeeded, delete stale previously owned resources. If prune fails, the overall reconcile is considered failed and `lastApplied*` does not advance.

**Phase 7 — Commit status**: update `status.source`, `lastAttempted*`, and (on success) `lastApplied*`. Replace `status.inventory` with the new ownership set. Append a bounded history entry. Finalize conditions.

Key ordering invariants:

- Apply must succeed before prune runs.
- Inventory and `lastApplied*` only update after full success (apply + prune if requested).
- No-op detection happens before apply to avoid unnecessary cluster mutations.

## Consequences

**Positive:** Each phase has explicit success criteria, failure classification, and outputs. Failures are attributable to specific phases, making debugging straightforward.

**Positive:** The phase structure maps directly to testable units. Each phase can be tested with focused inputs and expected outputs.

**Positive:** The ordering invariants (apply before prune, success before inventory update) are structurally enforced by phase sequencing rather than buried in conditional logic.

**Negative:** Seven phases add structural complexity to the reconcile function. This is mitigated by the `internal/` package structure (`source/`, `render/`, `apply/`, `inventory/`, `status/`) that keeps each phase's logic in focused packages.

**Trade-off:** The phase model is designed for extensibility (e.g., drift detection can extend Phase 4, health checks can extend Phase 5 in future versions) but does not implement those extensions yet.

Related: [module-release-reconcile-loop.md](../docs/design/module-release-reconcile-loop.md), [controller-architecture.md](../docs/design/controller-architecture.md)
