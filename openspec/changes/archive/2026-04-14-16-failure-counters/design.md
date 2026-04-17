## Context

ADR-005 classifies reconcile outcomes into six categories. The `FailureCounters` struct in `api/v1alpha1/common_types.go` has four fields: `Reconcile`, `Apply`, `Prune`, `Drift`. These need to be incremented on failure and reset on success per counter.

## Goals / Non-Goals

**Goals:**
- Provide helper functions for incrementing and resetting individual counters.
- Map reconcile outcomes to counter updates.
- Initialize counters struct if nil.
- Integrate into Phase 7 status commit.

**Non-Goals:**
- Counter-based alerting or automated responses (operator tooling concern).
- Counter caps or rollover (counters are int64, overflow is not a practical concern).
- Exposing counters as Prometheus metrics (that's the metrics change).

## Decisions

### 1. Counters map to phases, not outcomes

| Counter | Incremented when | Reset when |
|---------|-----------------|------------|
| `reconcile` | Any phase fails with transient or stalled error | Full reconcile succeeds (Applied, AppliedAndPruned, NoOp) |
| `apply` | Phase 5 (Apply) fails | Phase 5 succeeds |
| `prune` | Phase 6 (Prune) fails | Phase 6 succeeds or is skipped |
| `drift` | Phase 4 drift detection API call fails | Phase 4 drift detection succeeds (regardless of drift result) |

### 2. Helpers operate on pointer to FailureCounters

```go
func IncrementCounter(counters *FailureCounters, field string)
func ResetCounter(counters *FailureCounters, field string)
func EnsureCounters(status *ModuleReleaseStatus) *FailureCounters
```

`EnsureCounters` initializes the struct if nil, avoiding nil pointer checks at every call site.

### 3. Reset on success is per-counter, not global

A successful apply resets only the `apply` counter. If prune fails in the same reconcile, the `prune` counter increments independently. This gives operators precise signal about which phase is problematic.

### 4. Counter updates happen in Phase 7

All counter modifications are collected during phases 1-6 and applied in Phase 7 (status commit). This keeps counter logic out of the phase implementations and centralizes status mutations.

## Risks / Trade-offs

- **[Trade-off] No counter cap** — Counters grow unbounded on persistent failures. This is acceptable; operators who care about runaway counters can alert on thresholds externally.
- **[Risk] Counter reset on brief success** — If a resource flaps (fail, succeed, fail), the counter resets to 0 then increments to 1. Operators lose the historical failure count. Mitigation: `status.history` provides a bounded log of outcomes for pattern analysis.
