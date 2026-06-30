## Context

The operator's `ReconcileModuleInstance` (`internal/reconcile/moduleinstance.go`) currently reconciles every `ModuleInstance` it observes: register finalizer → handle deletion → check suspend → render → apply → prune → write status. Enhancement [0006](../../../../enhancements/0006/) introduces a second actor — the CLI — that writes the same `ModuleInstance` CR to record its own deployments (inventory in `status.inventory`). In a cluster running both, the operator would immediately take over a CLI-created CR and the two would fight over its resources. D3 resolves this with an explicit `spec.owner` marker the operator respects; D18 and D25 refine what the operator writes (conditions only, operator-exclusive) and the CLI's dual-mode behavior. This slice (A4) implements the operator side: the field, the skip, and the acknowledgement.

The controller watches `ModuleInstance` with `predicate.GenerationChangedPredicate` on the primary `For()`, plus a `Platform` watch that re-enqueues **all** instances on any Platform change (`mapPlatformToModuleInstances`). The suspend check (`internal/reconcile/moduleinstance.go`) is the closest existing analog to the owner-skip and the template for its condition/patch idiom.

## Goals / Non-Goals

**Goals:**

- Add `spec.owner: cli | operator` and make the operator fully hands-off for CLI-owned instances: no render, apply, prune, deletion cleanup, or finalizer.
- Have the operator record exactly one fact about a CLI-owned instance — `Ready: Unknown / ManagedExternally` — and nothing else.
- Keep behavior for existing/operator-owned instances byte-identical (additive, backward-compatible).
- Make the `cli → operator` handoff a clean fall-through into the normal reconcile.

**Non-Goals:**

- Reverse handoff (`operator → cli`) and any operator-relinquish-finalizer path — out of scope (0006 D16). A manual `operator → cli` edit on an operator-managed CR is undefined here.
- Event-level filtering of CLI-owned instances (a watch predicate that suppresses them). Considered and rejected for this slice (see Decision 4); the operator wakes and skips.
- The operator self-publishing its version into `Platform.status.operatorVersion` (0006 D24) — a separate concern.
- Any CLI-side behavior (writing `spec.owner: cli`, dual-mode apply) — 0006 slices C1.

## Decisions

### D1: `spec.owner` is a typed enum with no CRD default; the reconciler carries the default semantics

`Owner OwnerType` (`type OwnerType string`; `OwnerCLI = "cli"`, `OwnerOperator = "operator"`), markers `+kubebuilder:validation:Enum=cli;operator` + `+optional`, json `owner,omitempty`. **No `+kubebuilder:default`.**

*Why:* the repo uses zero `+kubebuilder:default` markers today — the established enum idiom (`RolloutSpec.Strategy`) is enum + optional + omitempty with no default. The reconciler makes the default load-bearing instead: it skips only on an explicit `owner == OwnerCLI`; absent / empty / `operator` all mean operator-managed. This realizes D3's "default operator" without depending on CRD read-time defaulting semantics for already-stored objects and without introducing a marker pattern the repo otherwise avoids. A typed `OwnerType` keeps the reconciler check readable and self-documenting. *Alternative — `+kubebuilder:default=operator`:* rejected; new pattern for the repo, and read-time defaulting of pre-existing objects is a nuance best not relied on when an explicit reconciler check is unambiguous.

### D2: The owner-skip gate sits before finalizer registration

The `owner == cli` check is the first branch in `ReconcileModuleInstance` after the initial `Get`, **before** finalizer registration and the deletion branch. For a CLI-owned instance with no `DeletionTimestamp`: set `MarkManagedExternally`, emit a `ManagedExternally` event, patch status, return. For a CLI-owned instance being deleted: return immediately (nothing to clean up).

*Why:* `handleDeletion` prunes the resources in `status.inventory`. If the operator registered its `opmodel.dev/cleanup` finalizer on a CLI-owned CR, deleting that CR would drive the operator to prune resources the CLI owns — the exact two-actors-fighting hazard D3 exists to prevent. The suspend check sits *after* finalizer registration precisely because a suspended instance is still operator-owned (the operator must still clean it up on delete); a CLI-owned instance is categorically different — the operator must touch nothing, including the finalizer. A neat consequence: finalizer presence then tracks operator ownership exactly, so the handoff fall-through (D3.5) adds the finalizer at the moment the operator first takes ownership. *Alternative — gate at the suspend location (after finalizer):* rejected; it stamps a finalizer on CLI-owned CRs and makes their deletion prune CLI-owned resources.

### D3: The skip writes only the `ManagedExternally` condition — no `observedGeneration`, no other status

The skip path calls a new `status.MarkManagedExternally(&mi)` (`conditions.MarkUnknown(&mi, ReadyCondition, ManagedExternallyReason, "ModuleInstance is managed externally by the CLI")`, deleting `Reconciling`/`Stalled` — mirroring `MarkSuspended` but `Unknown` not `False`), then patches with `patch.WithOwnedConditions{...all condition types...}` **only** — not `patch.WithStatusObservedGeneration{}`. It writes no `inventory`, `lastApplied*`, `instanceUUID`, or any other status field.

*Why:* `observedGeneration` means "the controller reconciled up to generation N"; the operator is deliberately *not* reconciling a CLI-owned instance, so stamping it would assert work that did not happen. Owning the conditions list (D25: conditions are operator-exclusive) lets the operator cleanly carry the single `ManagedExternally` entry. The patcher diffs against the snapshot taken at `patch.NewSerialPatcher` (after the `Get`); since the skip mutates only conditions, `status.inventory`/`lastApplied*` written by the CLI are identical in snapshot and current → never included in the patch → never clobbered (D25). The write is idempotent: a re-wake on an already-acknowledged instance yields an empty diff and no API call.

### D4: Wake-then-skip, not event-level filtering

The operator is woken for CLI-owned instances (on each generation-bumping spec edit, and on every Platform-watch re-enqueue) and skips early; it does not filter them out at the watch predicate.

*Why:* a predicate filter would have to cover **two** enqueue sources — the `For()` predicate and the `mapPlatformToModuleInstances` map func, which builds requests directly and bypasses predicates — and would need a finalizer-aware guard to avoid stranding the `opmodel.dev/cleanup` finalizer on a CR manually flipped `operator → cli` (it would be stuck `Terminating`). Critically, full filtering also means the operator never writes the `ManagedExternally` acknowledgement (D3), a deviation from D3. The wake-then-skip cost is bounded: the skip is O(1) and the condition write is idempotent (empty diff after the first acknowledgement), so repeated wake-ups — including Platform-triggered ones — are no-ops. Keeping D3's visible "operator is deferring" signal is worth the cheap wake-ups. *Alternative — predicate filter + map-func skip + amend D3 to drop `ManagedExternally`:* considered and rejected for this slice; recorded here so the option is documented if wake volume ever matters. A later, non-load-bearing optimization could skip CLI-owned instances inside `mapPlatformToModuleInstances` without touching D3.

## Relationship to existing capabilities

The new behavior is specified as one new capability (`module-instance-ownership`) rather than as delta edits to adjacent specs, which are still written for the pre-0002 `ModuleRelease` type:

- **finalizer-and-deletion** — its "Finalizer registration" requirement ("the controller MUST add the finalizer … during Phase 0") is now scoped to operator-owned instances. The new capability states the exception directly (CLI-owned instances receive no finalizer) in current `ModuleInstance` terms.
- **status-conditions** — gains the `ManagedExternally` reason constant and a `MarkManagedExternally` helper. Specified inside the new capability.
- **suspend-resume** — unchanged. The owner-skip is a parallel, independent gate (an instance can be CLI-owned regardless of `spec.suspend`); the suspend path is the idiom template, not a dependency.

## Risks / Trade-offs

- **Manual `operator → cli` edit on an operator-managed CR** → the operator has already stamped its finalizer; on the next reconcile the owner-skip returns without removing it, and on delete the CR could stick in `Terminating`. *Mitigation:* this transition is explicitly undefined/out of scope (0006 D16, no reverse handoff). Document it; the operator-relinquish path is a future enhancement. The forward path (CLI creates `owner: cli` → never gets a finalizer) has no such issue.
- **Wake-up volume for many CLI-owned instances on Platform churn** → every Platform change re-enqueues all instances, including CLI-owned ones, each a no-op skip. *Mitigation:* bounded and O(1); a one-line `owner == cli` skip in `mapPlatformToModuleInstances` is available later if needed, without touching D3.
- **Idempotency of the condition write** → if `MarkManagedExternally` produced a non-empty diff every reconcile (e.g. a changing message), repeated wake-ups would churn status. *Mitigation:* a static message ensures the Flux conditions helper leaves an already-set condition untouched, so the patch diff is empty after the first write.

## Migration Plan

Additive and backward-compatible — no data migration. Existing `ModuleInstance` objects have no `owner`, are treated as operator-managed, and reconcile unchanged. Rollback is reverting the field + skip + helper and regenerating manifests; nothing persisted depends on the field until the CLI starts writing `owner: cli` (0006 C1). After API changes: `task dev:manifests dev:generate`; after manifest changes: `task operator:installer`.

## Open Questions

None. The field shape (D1), skip placement (D2), status boundary (D3), and wake-then-skip vs filter (D4) are all decided. The reverse-transition edge is consciously out of scope (0006 D16).
