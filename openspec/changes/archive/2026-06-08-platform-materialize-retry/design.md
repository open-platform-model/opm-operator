## Context

The `PlatformReconciler` (`internal/controller/platform_controller.go`) currently handles both failure branches as `status.MarkStalled(...)` + `return ctrl.Result{}, r.patchStatus(...)` — no requeue. `observedGeneration` is set only on the success path. Confirmed surface:

- `MaterializeError{Kind, Subscription, Version, Cause}` with kinds `catalog`/`core-schema`. There is **no transient flag** — classification must inspect `Cause` (via `errors.Unwrap`/`errors.As`).
- `reconcile.StalledRecheckInterval = 30 * time.Minute` exists in `internal/reconcile/backoff.go`; `internal/controller` already depends on `internal/reconcile` elsewhere, so it is importable (or define platform-local constants).
- `Store.Set` is called only on success today, so leaving the failure path untouched already preserves the last-good slot — that behavior stays.

This change is independent of the render-swap cut-overs: it is a platform-reconciler fix that lands in parallel and improves availability of the gated release paths.

## Goals / Non-Goals

**Goals:**

- Materialize/synth failures requeue on a bounded interval (self-heal against the mutable registry).
- Transient causes retry fast; semantic/unknown causes retry slowly; misclassification defaults to slow (safe).
- `observedGeneration` recorded on failure paths.
- No event spam under periodic recheck.

**Non-Goals:**

- Changing the `Ready=False`/`MaterializeFailed` reason, the structured-error message, or the store-preservation behavior.
- Adding a transient flag to the library `MaterializeError` (an upstream option, noted but not done here).
- Anything in the render-swap (cut-overs, fork deletion, BundleRelease).

## Decisions

### Always requeue; materialize is inherently retryable

**Decision:** Both failure branches return `ctrl.Result{RequeueAfter: interval}`; no materialize failure is terminal.

**Rationale:** Materialize resolves against an external mutable OCI registry. A failure can clear with no CR change — the registry becomes reachable, or a catalog version satisfying a subscription range is published later. Waiting for a generation change (today's behavior) leaves the platform — and every gated release — stuck on external state that has since recovered. Periodic retry is the correct model and matches the `ModuleRelease`/`Release` reconcilers, which already `MarkStalled` + `RequeueAfter`.

**Alternatives considered:** keep no-requeue and rely on generation predicate (the current bug — misses registry-side recovery); return the error to use controller-runtime's exponential backoff (couples cadence to the workqueue limiter and re-logs errors; an explicit `RequeueAfter` is clearer and is the repo convention).

### Best-effort transient classification, safe long-interval default

**Decision:** A small `isTransientMaterialize(err)` helper unwraps to the cause and reports transient for network/timeout signals (`net.Error` with `Timeout()`, `*url.Error`, `context.DeadlineExceeded`). Transient → short interval; otherwise → `StalledRecheckInterval`. Unknown defaults to the long interval.

**Rationale:** The cluster-blocking case is a transient registry blip; a 30-minute recheck there is poor availability. A short retry for transient causes fixes that, while semantic failures (bad path, divergent FQN) don't benefit from rapid retry and stay on the slow recheck (which still catches registry-side fixes). Defaulting unknown causes to the long interval means a wrong guess never makes things worse than today.

**Alternatives considered:** a single moderate interval for all failures (simpler — Principle VII — but either too slow for transient or too noisy for semantic); rich classification by `MaterializeError.Kind` (Kind is catalog-vs-core-schema, orthogonal to transient-vs-semantic, so unhelpful); add a `Transient bool` to the library error (cleaner long-term, but an upstream change out of scope — noted as a follow-up).

### Record observedGeneration on failure; gate the event on transition

**Decision:** Set `plat.Status.ObservedGeneration = plat.Generation` on the failure paths; emit the warning event only when the failure condition is newly entered or its message changes (compare against the prior condition before patching).

**Rationale:** Without `observedGeneration`, a stalled platform reads as un-reconciled. Without transition-gating, periodic requeue would emit a warning event every interval; the status patcher already suppresses unchanged conditions, and the event should match that.

## Risks / Trade-offs

- **Classification fragility** — `Cause` matching is best-effort and CUE/registry error wrapping may not expose a clean `net.Error`. Mitigation: safe default to the long interval (never worse than today); a test per bucket; the upstream `Transient bool` option remains open if matching proves unreliable.
- **Short-interval retry load** — a transient-classified failure retrying every short interval hits the registry repeatedly. Mitigation: one cluster-singleton platform; short interval on the order of a minute, not seconds; once it succeeds it stops.
- **Interval choice** — too short wastes registry calls, too long hurts availability. Mitigation: reuse `StalledRecheckInterval` for the default; pick a conservative short value (~1 min), tunable later.

## Migration Plan

1. Add a short transient retry interval constant (and reuse `reconcile.StalledRecheckInterval` for the default).
2. Add `isTransientMaterialize(err error) bool` (unwrap → `errors.As` net/url/deadline).
3. In `Reconcile`'s synth and materialize failure branches: set `ObservedGeneration`; choose the interval via classification; emit the event only on transition; `return ctrl.Result{RequeueAfter: interval}, r.patchStatus(...)`.
4. Tests (envtest/unit): transient cause → short `RequeueAfter`; semantic/unknown cause → long; `observedGeneration` set on failure; event not re-emitted across identical rechecks; store still holds last-good on failure.
5. Validation gates.

**Rollback:** revert the commit; failure handling returns to no-requeue stall.

## Open Questions

- Exact transient matchers and the short interval value — confirm against real registry/CUE error shapes during implementation (what `MaterializeError.Cause` actually wraps on an unreachable registry).
- Whether to additionally propose an upstream `MaterializeError.Transient bool` (or a typed transient cause) in the library to replace best-effort matching — out of scope here, worth filing if matching proves unreliable.
