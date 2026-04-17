## Why

Two specs already require behavior that the current code violates:
`drift-detection/spec.md` mandates `Drifted=True` is set when drift is observed
on a no-op reconcile, and `failure-counters/spec.md` requires the drift counter
to increment on dry-run failures. But `reconcile-loop-assembly/spec.md`
contradicts both by stating "the reconciler MUST NOT patch status when the
outcome is `NoOp` and no state has changed." Code follows the assembly spec —
drift state and counter deltas are computed during NoOp reconciles then
discarded. Drift visibility is delayed until the next apply-triggering
reconcile, and dry-run failures silently pile up unrecorded.

Storm prevention (the reason for the no-patch rule) is now structurally handled
by `GenerationChangedPredicate` on the controller's event filter — the
NoOp-skip-patch is redundant defense that breaks two existing requirements.

## What Changes

- Update `internal/reconcile/modulerelease.go` deferred status commit so the
  NoOp branch:
  - Calls `updateFailureCounters(...)` (drift counter requires this).
  - Always clears `NextRetryAt` (currently conditional).
  - Always issues a `patcher.Patch(...)` with the existing `WithOwnedConditions`
    set (which already includes `DriftedCondition`).
- Keep NoOp out of `lastAttempted*`, `inventory`, and history updates — those
  describe meaningful reconcile outcomes; NoOp is unchanged-state.
- Update memory entry `project_noop_reconcile_storm.md` to record that the
  storm-prevention strategy now relies solely on `GenerationChangedPredicate`,
  not on skipping NoOp patches.

## Capabilities

### New Capabilities

_(none)_

### Modified Capabilities

- `reconcile-loop-assembly`: NoOp branch now patches drift condition, failure
  counters, and clears `nextRetryAt`. Replaces "MUST NOT patch on NoOp" rule
  with bounded patch contract.

## Impact

- **Go code**: ~10-line change in `internal/reconcile/modulerelease.go` deferred
  status commit, NoOp branch only.
- **Tests**: Unblocks the 3 drift-on-NoOp tests in `fix-reconcile-test-coverage`
  (`drift_test.go` cases that detect drift via no-op reconcile or fail dry-run
  detection). No new tests required here — `fix-reconcile-test-coverage`
  un-pends them.
- **API**: None.
- **Dependencies**: None.
- **Storm safety**: Relies on existing `WithEventFilter(predicate.GenerationChangedPredicate{})`
  at `internal/controller/modulerelease_controller.go:77`. Status subresource
  patches do not bump generation, so the predicate filters them.
- **API call cost**: Every NoOp now issues one extra status patch (~1 round-trip
  per reconcile). Acceptable trade-off for drift visibility.
- **SemVer**: PATCH — internal correctness fix; no API surface changes; aligns
  code with already-stated spec requirements.

## Scope Boundary

**In scope:**

- NoOp branch in deferred status commit
- `reconcile-loop-assembly` spec MODIFIED requirement
- Memory entry update for storm-prevention strategy

**Out of scope:**

- Any other reconcile flow changes
- New metrics, events, or logging
- Refactoring NoOp detection logic itself
- Changes to `drift-detection` or `failure-counters` specs (already correctly
  state the desired behavior)
- Empirical e2e validation of storm absence (could be a follow-up)
