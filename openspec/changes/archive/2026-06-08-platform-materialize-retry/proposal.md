## Why

The `PlatformReconciler` classifies every `Materialize` (and `SynthesizePlatform`) failure as `Stalled` and returns `ctrl.Result{}` with no requeue — recovery depends on a `Platform` spec change re-triggering the generation predicate. That is wrong for two reasons. First, materialize resolves against a **mutable external OCI registry**: a failure today (registry unreachable, or no version yet matching a subscription range) can clear tomorrow with no CR edit, so the reconciler must retry on its own. Second, now that `ModuleRelease` and `Release` gate on the materialized platform, a platform stuck `Stalled` from a *transient* failure keeps the store empty and blocks **every** release in the cluster indefinitely — a real availability bug, not an edge case. This change makes materialize failures self-heal: the reconciler requeues on a bounded interval, retrying faster for clearly-transient causes.

## What Changes

- **Requeue on failure.** On `SynthesizePlatform`/`Materialize` failure, return `ctrl.Result{RequeueAfter: <interval>}` instead of `ctrl.Result{}`, so the reconciler periodically retries against the registry without waiting for a spec change. The existing `Stalled`/`MaterializeFailed` status and the "do not clobber the last-good store slot" behavior are unchanged.
- **Cadence by best-effort classification.** Clearly-transient causes (network/timeout — detected via `errors.As` on `net.Error`/`*url.Error`/`context.DeadlineExceeded`, reached through `MaterializeError.Cause`) requeue on a short interval (fast self-heal for the cluster-blocking case). Everything else — semantic failures (bad subscription path, divergent FQN) and unclassifiable causes — requeues on the long `StalledRecheckInterval` (still self-heals when the registry changes, just slowly). The fallback is always the long interval, so misclassification is safe.
- **Record observed generation on failure.** Set `status.observedGeneration` on the failure paths too (today it is set only on success), so a stalled `Platform` reports the generation it actually observed rather than appearing un-reconciled.
- **Avoid event spam.** Emit the warning event only on a transition into (or change of) the failed state, not on every periodic recheck — the status patcher already dedups unchanged conditions; the event should match.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `platform-reconciler`: extends the materialize-outcome behavior — materialize/synth failures now requeue on a bounded interval (short for transient causes, long otherwise), record `observedGeneration` on the failure path, and emit the failure event only on transition. The last-good store slot is still preserved on failure.

## Impact

- **Code**: `internal/controller/platform_controller.go` (failure branches: requeue interval, `observedGeneration`, transition-gated event; a best-effort `isTransientMaterialize` helper); interval constants (reuse `reconcile.StalledRecheckInterval` for the default; add a short transient interval). No API/CRD change.
- **Dependencies**: none new — independent of the render-swap cut-overs (this is a platform-reconciler fix and can land in parallel). It directly improves the availability of the gated `ModuleRelease`/`Release` paths once those cut-overs land.
- **Behavioral change**: a `Platform` whose materialize fails now retries automatically (fast for transient, slow otherwise) instead of staying stalled until edited; releases blocked on `PlatformNotReady` recover automatically once the platform does.
- **Enhancement**: hardens 0001 §8.3 (materialize lifecycle) and aligns the platform reconciler with Principle V (transient → requeue) and the requeue cadence the `ModuleRelease`/`Release` reconcilers already use.
- **SemVer**: PATCH/MINOR — a reliability fix to an alpha controller; no API or signature change.
- **Complexity justification (Principle VII)**: the core fix is a bounded requeue (one-line per branch). The transient classification is a small best-effort helper justified by the availability impact (a 30-minute stall blocking all releases on a network blip); it degrades safely to the long interval.
