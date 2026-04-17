## Context

Three specs touch NoOp behavior:

| Spec | Position |
|------|----------|
| `drift-detection` | `Drifted=True` MUST be set on no-op when drift observed (line 35-44) |
| `failure-counters` | `drift` counter MUST increment on dry-run failure (line 53-59) |
| `reconcile-loop-assembly` | `MUST NOT patch status when outcome is NoOp and no state has changed` (line 47-56) |

The first two assume status patches go through. The third forbids them on
NoOp. The contradiction was introduced by commit `7c2814b` (storm-prevention)
which added `GenerationChangedPredicate` AND retained skip-patch-on-NoOp as
belt-and-suspenders. Tests for the first two requirements were `PIt` (pending),
so the conflict was never surfaced.

The primary storm-prevention mechanism is now `GenerationChangedPredicate{}`
applied via `WithEventFilter` on the controller (see
`internal/controller/modulerelease_controller.go:77`). Status subresource
patches do not bump `metadata.generation`, so the predicate filters those
events at the watch boundary — no reconcile loop.

The skip-patch-on-NoOp behavior is therefore redundant defense that breaks two
existing requirements.

## Goals / Non-Goals

**Goals:**

- Align code with already-stated `drift-detection` and `failure-counters`
  spec requirements.
- Update `reconcile-loop-assembly` to reflect the post-predicate reality:
  NoOp patches drift state + counters + nextRetryAt, nothing else.
- Keep storm prevention intact via `GenerationChangedPredicate`.

**Non-Goals:**

- Refactoring the deferred status commit structure beyond the NoOp branch.
- Changing what counts as a NoOp (digest comparison logic untouched).
- Adding new metrics, events, or log lines.
- Persisting `lastAttempted*`, history, or inventory on NoOp (those fields
  describe meaningful outcomes; NoOp is unchanged-state).

## Decisions

### NoOp patch contract: drift + counters + nextRetryAt only

On `outcome == NoOp`, the deferred function:

1. Calls `updateFailureCounters(&mr.Status, outcome, phases)` — drift counter
   needs the increment/reset based on `phases.driftFailed`; reconcile counter
   resets to 0.
2. Sets `mr.Status.NextRetryAt = nil` unconditionally — NoOp means healthy.
3. Issues `patcher.Patch(...)` with the same `WithOwnedConditions` set used
   by the non-NoOp path (which already includes `DriftedCondition`).
4. Does NOT touch `lastAttempted*`, `Inventory`, or `History`.

**Rationale:** Drift state and counters are the only fields that meaningfully
change on NoOp. lastAttempted/history would falsely imply a meaningful event
occurred. Inventory is only updated on successful apply.

**Alternative considered:** Patch only when drift state OR counters changed
(track previous state). Rejected — extra complexity for marginal gain. Patches
are cheap, predicate filters watch events, kube-apiserver handles idempotent
patches efficiently.

### Storm safety: trust GenerationChangedPredicate

The original NoOp storm (memory entry `project_noop_reconcile_storm.md`,
2026-04-15) occurred BEFORE commit `7c2814b` added `GenerationChangedPredicate`.
That commit's message stated the predicate makes `shouldSkipStatusPatch`
"redundant" — yet it also left the skip-patch logic in the defer. This change
removes the redundant defense for NoOp specifically.

**Rationale:** Status subresource patches do not modify `metadata.generation`.
`GenerationChangedPredicate{}` filters watch events where generation didn't
change. Therefore status-only patches cannot trigger reconcile via the
controller's primary watch. This is verifiable by code review at the
controller setup site.

**Alternative considered:** Add a custom predicate that explicitly ignores
status-only updates beyond generation comparison. Rejected — generation check
already covers this; extra predicate is redundant.

**Risk acknowledged:** Verified by code-level reasoning, not empirically
re-tested under the original storm conditions. See Risks section.

### Scope: no spec changes to drift-detection or failure-counters

Both specs already state the correct behavior. This change only modifies
`reconcile-loop-assembly` to remove the contradicting clause.

**Rationale:** Minimum delta surface. Drift-detection and failure-counters
specs need no edits — the violation was code, not their definitions.

## Risks / Trade-offs

- **Empirical storm validation gap** → Code-level reasoning is sound, but the
  fix has not been re-tested under the original storm-producing conditions.
  Mitigation: deploy to dev cluster, monitor reconcile rate after triggering
  NoOps. If a storm recurs, root-cause is not the predicate (it would be a
  separate event source — `Owns(...)` watches, requeue loops, etc.).

- **Extra patch per NoOp** → Every NoOp now issues one status patch
  (~1 round-trip per reconcile). Previously: only when `NextRetryAt` cleared.
  Mitigation: predicate filters the resulting watch event; kube-apiserver
  handles idempotent patches without additional work.

- **NoOp now visible in API audit logs** → Patches are logged. Consumers of
  audit logs (compliance, debugging) will see one more entry per reconcile.
  Acceptable trade-off for drift visibility.

- **Spec audit gap** → drift-detection's "no-op" scenario was un-tested for
  the lifetime of the storm-prevention commit. Other specs may have similar
  un-tested requirements. Out of scope here, but worth a follow-up audit.

## Migration Plan

Single atomic change. No data migration. No feature flag.

- **Deploy:** standard controller upgrade.
- **Rollback:** revert the commit; previous skip-patch behavior returns.
  Drift visibility regresses but no data loss.
- **Verification:** drift integration tests in
  `test/integration/reconcile/drift_test.go` (un-pended by sister change
  `fix-reconcile-test-coverage`) provide automated validation. Manual:
  modify a managed ConfigMap on a dev cluster, observe `Drifted=True`
  appears within one reconcile interval.

## Open Questions

- **Should empirical storm validation be a task in this change, or a
  follow-up?** Lean: follow-up. Sister change ships first to validate
  no-storm in CI; this change can ship if CI-green.
- **Is there a class of NoOp with state changes we want to capture beyond
  drift+counters?** None identified. lastAttempted/history would mis-signal.
  Punt to YAGNI.
