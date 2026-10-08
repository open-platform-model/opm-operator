## Context

See proposal.md for the motivation. The facts that shape the approach:

- `Reconcile` acquires the catalog first (`transformerregistration_controller.go`, the `AcquireCatalogFromRegistry` call). Every error except `ErrWrongKind` goes to `refuse`, which sets `status.accepted = false`, marks `Stalled` and requeues after 30 minutes.
- The Platform reconciler builds the package from claims with `Accepted && Active`, so a cleared `accepted` removes the provider's catalog from the next generation.
- The library (v1.0.0-beta.6) types a registry fetch failure as `*errors.FetchError`. `errors.Is(err, ErrTransient)` holds when the registry gave no HTTP response or answered 5xx. A not-found, a refused credential and a 429 are not `ErrTransient`.
- The library serves a fetched version from the on-disk module cache with no registry call, so the failure needs a cold cache.
- The claim's status has no failure counter, and the CRD is out of scope.

## Goals / Non-Goals

**Goals:**

- An accepted claim keeps its whole verdict through a transient acquisition failure.
- The failure is visible on the claim and retried on a short, growing delay.
- Tests fail before the fix, and one of them goes through the real library with a cold cache.

**Non-Goals:**

- No change for a claim that is not accepted. It is refused with `CatalogUnresolved` and rechecked after 30 minutes, as today.
- No change to the rules between competing claims, to activation, or to the Platform reconciler.
- No change to the provider-identity check. The audit also named a possible cache lag there (a provider `ModuleInstance` read as absent); that scenario is not verified and is not part of this change.
- No new condition reason and no new status field.

## Research & Decisions

### Which failures keep the verdict

**Context**: The operator has two candidate classifications. `opmreconcile.IsTransientFailure` is true for every typed fetch failure, not-found and unauthorized included; the workload reconcilers use it to pick the backoff. The library's `ErrTransient` is narrower: network-level only.
**Explored**: `internal/reconcile/resolution.go`, library `opm/errors/fetch.go`.
**Decision**: Keep the verdict only when `opmreconcile.IsTransientFailure(err) && errors.Is(err, oerrors.ErrTransient)`.
**Rationale**: A catalog the registry no longer holds must still un-accept, and a not-found is a `FetchError` that is not `ErrTransient`. The first operand adds the operator's existing rule that a typed terminal cause in the chain wins over a fetch failure joined to it. Both read types only.

### What the claim reports while it is held

**Context**: `deferVerdict` writes `Ready=Unknown`, which would replace the `Ready=True/Accepted` condition the task says must stay.
**Explored**: `internal/status/conditions.go`, the fluxcd `conditions` setter.
**Decision**: A new helper, `holdVerdict`, sets only `Reconciling=True` with reason `CatalogUnresolved` and a message that does not change between attempts. It does not touch `accepted`, `active`, `Ready`, `Active` or `observedGeneration`. It emits one Warning event, with the registry error, when the claim enters the state. The error text goes in the event and the log, not in the condition.
**Rationale**: The verdict that stands is the last one taken, so every field that records it stays byte-identical. `observedGeneration` stays too: after a spec edit the held verdict belongs to the earlier generation, and the field says so. A fixed message makes a repeated attempt an empty status patch, and it keeps the condition's `lastTransitionTime` at the start of the outage, which the backoff reads. `CatalogUnresolved` is reused because adding a reason changes `internal/status`, outside this change's scope.

Alternative considered: keep the claim untouched and only emit an event. Rejected: events expire, and status is this repo's operational ledger (Principle IV).

### Where the backoff comes from

**Context**: The other reconcilers feed a persisted failure counter to `ComputeBackoff`. The claim has no counter and the CRD does not change.
**Explored**: an in-memory counter per claim; returning the error to the controller runtime's rate limiter; deriving the delay from status.
**Decision**: The delay is the time the claim has spent in the held state, clamped to `[BackoffBaseDelay, BackoffMaxDelay]` (5 seconds to 5 minutes), plus up to 10 percent jitter. The time comes from the `Reconciling` condition's `lastTransitionTime`.

```go
func holdBackoff(since, now time.Time) time.Duration {
	delay := now.Sub(since)
	if delay < opmreconcile.BackoffBaseDelay {
		delay = opmreconcile.BackoffBaseDelay
	}
	if delay > opmreconcile.BackoffMaxDelay {
		delay = opmreconcile.BackoffMaxDelay
	}
	return delay
}
```

**Rationale**: Waiting as long as the outage has lasted doubles the delay at each attempt (5s, 5s, 10s, 20s, ... 5m), with no state outside the object. It survives a restart, and an extra reconcile from a watch event does not inflate it. The controller runtime's limiter starts at 5 milliseconds, which would send about ten fetches at a failing registry in the first five seconds. An in-memory counter is lost on restart and needs cleanup on deletion.

### How the test forces a cold cache

**Context**: The defect needs a cold module cache; a stub acquirer does not prove the library returns a transient failure at this call site.
**Decision**: One group of specs passes the real library Kernel as the acquirer, with `CUE_CACHE_DIR` pointed at a new empty directory for the spec and the registry mapped to `127.0.0.1:1` (nothing listens) or to an `httptest` server that answers 404 or 503. The other specs use the stub with typed `*FetchError` values.
**Rationale**: With an empty cache the fetch must go to the registry, so the error the reconciler classifies is the library's own. The suite runs no manager, so the process-wide variable is read only by the spec that set it, and it is restored afterwards.

## Reconcile phase impact

- Source: the catalog acquisition gains one branch. No other phase of the claim reconcile changes.
- Render, Apply, Prune: none (the claim reconciler has none).
- Status: `Reconciling=True/CatalogUnresolved` can stand beside an accepted verdict; the next attempt that gets an answer removes it through the existing accept, refuse or defer path.

## Risks / Trade-offs

- [A held claim keeps a verdict taken for an earlier spec after an edit] → The Platform already folds an edited claim before it is re-judged (a known, separate defect). `observedGeneration` is left at the judged generation so a later fix can tell.
- [A long outage keeps a claim whose catalog was withdrawn] → The first attempt that gets an answer un-accepts it. During the outage nothing can tell the two cases apart.
- [A 429 or a refused credential still un-accepts] → That is the library's definition of transient. It is put to the owner as a question, not widened here.
- [`Ready=True` with `Reconciling=True` reads as "in progress" to kstatus tools] → Intended: the claim serves, and the operator is retrying a check.
