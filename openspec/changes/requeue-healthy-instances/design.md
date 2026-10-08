## Context

See proposal.md for the motivation. The facts that shape the design, each read from the code at `59537b7`:

- `healthRequeue` returns 0 for a rolled-out or empty inventory (`internal/reconcile/health.go:216-228`). The three healthy returns of a ModuleInstance reconcile pass that value on: the `NoOp` return (`moduleinstance.go:541`), the apply return (`:653`) and the skipped render (`:694`).
- The controller watches ModuleInstance with `GenerationChangedPredicate` and the Platform; it owns nothing (`internal/controller/moduleinstance_controller.go:167-173`). The manager sets no `SyncPeriod`.
- A reconcile whose render inputs are unchanged, younger than `--drift-render-interval` (default 30m), skips its render: it judges health and patches `Healthy` only when it changed (`moduleinstance.go:263-265`, `inputs.go`). Drift detection runs only on a reconcile that renders.
- A `NoOp` skips the apply (`moduleinstance.go:534-542`). `apply.DetectDrift` sends one SSA dry-run per rendered object and reads `ConfiguredAction` as drift; Flux returns `CreatedAction` for an object that does not exist (`fluxcd/pkg/ssa` v0.77.0 `manager_diff.go`: the dry-run object has no `resourceVersion`), and `DetectDrift` drops that answer today (`internal/apply/drift.go:55`).
- A missing inventory object already makes `Healthy=False` with reason `NotRolledOut` and a health requeue of at most 2 minutes (`health.go:93-96`, `:220-224`).
- The opm catalog renders Jobs with `ttlSecondsAfterFinished` (`catalog_opm/src/transformers/job_transformer.cue`).
- No jitter helper exists in the repo's own code. `k8s.io/apimachinery/pkg/util/wait.Jitter` is in a module the operator already requires.

## Goals / Non-Goals

**Goals:**

- `Healthy` and `Drifted` of an unchanged ModuleInstance are never older than a known bound.
- An object of an instance that was deleted by hand is created again without a spec change.
- An unchanged, healthy instance costs no write and, between two drift render intervals, no render.

**Non-Goals:**

- A `spec.interval` field, a watch on child objects, drift correction of live objects.
- Any change to ModulePackage or Platform requeue behaviour, to RBAC, or to the health verdict of a missing object.
- A faster restore than the drift render interval (see Decisions, "The restore waits for a rendering reconcile").

## Decisions

### A fixed operator-wide interval, as a flag

The three healthy returns go through one function:

```go
// instanceRequeue is a ModuleInstance's requeue after a reconcile that ended
// well: the health requeue when health asks for one, the instance reconcile
// interval otherwise, with up to requeueJitter of it added.
func instanceRequeue(healthAfter, interval time.Duration) time.Duration {
    if healthAfter > 0 || interval <= 0 {
        return healthAfter
    }
    return wait.Jitter(interval, requeueJitter) // [interval, 1.1*interval)
}
```

`--instance-reconcile-interval` (default `10m`) reaches it through `ModuleInstanceReconciler.ReconcileInterval` and `ModuleInstanceParams.ReconcileInterval`. The flag MUST refuse a negative value at start, like `--drift-render-interval`. `0` disables the requeue, so the old behaviour stays one flag away. Tests that build a reconciler without the field keep a zero interval and so keep their assertions.

Alternatives considered:

- *Status quo plus a documented limit.* No code, but `Healthy=True` can be false for days; rejected by the task.
- *`spec.interval` on ModuleInstance* (issue 35, the plan ADR-016 names). A per-object knob, but an API contract that cannot be withdrawn after GA; out of scope by the brief. The flag does not block it: a later field would override the operator default.
- *A watch on child objects* (issue 34). Fast, but it needs dynamic informers per rendered kind, more memory and RBAC, and an event filter for the operator's own writes; a large change with its own failure modes.
- *The shorter of health requeue and interval*, as `packageRequeue` does. It would move the 30-minute recheck of a stalled Deployment to 10 minutes and change a scenario of `instance-health` for no need. Health requeues stay as they are.

**Why 10 minutes.** It is the bound on how stale `Healthy` can be. Between renders the periodic reconcile is a skipped render: one uncached GET per inventory object, at most 8 in flight, no write when nothing changed. 500 instances with 20 objects each give about 17 GET per second on average. Shorter buys little; longer leaves a failed Deployment unreported for longer.

**Why jitter.** After a restart every instance reconciles in one burst, so without jitter they all come due together for ever. Up to 10 percent added per requeue spreads them over a few cycles. It is a one-line call to a helper the module graph already has; no new dependency.

### The restore applies missing objects only

`apply.DriftResult` gains `Missing []*unstructured.Unstructured`: the input objects whose dry-run answered `CreatedAction`. `detectDrift` returns them. In the reconcile:

```go
missing, driftFailed := detectDrift(ctx, params.ResourceManager, &mi, applyList)
restore := restorable(missing)            // drops Jobs with a TTL
if isNoOp && len(restore) == 0 { /* NoOp branch, unchanged */ }
restoring := isNoOp                        // digests match, something is missing
if restoring { applyList = restore }
// impersonation, apply.Apply(applyList), events and metrics as today
if !restoring { status.ClearDrifted(&mi) } // a restore did not touch drifted objects
// prune (the stale set is empty when digests match), MarkReady, health from now
```

The outcome is `Applied`: history gets an entry, `lastAppliedAt` moves, the inventory revision goes up by one with the same digest, and health is checked quickly because something is rolling out again. The existing `Applied` event reports the count of created objects.

Alternatives considered:

- *Apply the full rendered set when something is missing.* Three lines less, but it rewrites every drifted field of the instance as a side effect of one deleted object. That is drift correction, which ADR-012 and the `drift-detection` spec rule out.
- *Report only, do not restore.* Keeps today's behaviour; the task asks for the restore.
- *Find missing objects from the health reads.* It needs no change in `internal/apply`, but it adds a second answer to "does it exist" beside the dry-run, read through another identity, and cannot run before the render.

**The Job exception.** A Job with `ttlSecondsAfterFinished` is deleted by the cluster after it finished. To restore it would run it again on every drift render interval, which for a migration Job is harmful. `restorable` MUST drop a `batch/v1` Job whose rendered spec sets the field. A Job without a TTL that someone deletes is restored like any object.

### The restore waits for a rendering reconcile

A skipped render has no rendered objects, so it cannot restore. A missing object is restored on the first reconcile that renders: when `lastAppliedInputs.renderedAt` is older than `--drift-render-interval`, or on any input change. At the defaults that is at most about 30 minutes after the deletion plus the health requeue of at most 2 minutes that a missing object already causes.

Alternative considered: *let a missing object in the health judgement cancel the skip*. Restores within one requeue, but an object that cannot be restored (a failing dry-run, a Job with a TTL, something another controller deletes at once) would then render every 2 minutes for ever. The drift render interval already is the documented bound for "the cluster no longer holds what was rendered", and `--drift-render-interval` tunes it. The fast path can be added later without a contract change.

### Reconcile phase impact

- Source, Render: unchanged. More reconciles reach them: one per instance per drift render interval.
- Plan: the dry-run result now also yields the missing set; `NoOp` means "digests match and nothing restorable is missing".
- Apply: runs for the missing subset on a restore.
- Prune: unchanged (the stale set is empty when the inventory digest matches).
- Status: unchanged shape. A restore is recorded as an apply.

## Research & Decisions

### What the periodic reconcile costs with one render slot

**Context**: the brief asks what the requeue costs with `--max-concurrent-renders=1`.
**Explored**: the skip path (`inputs.go`, `judgeSkippedInstance`), the `NoOp` commit (`commitNoOpStatus`), `render.Slots`, `docs/RENDERING.md`.
**Decision**: no extra bound; document the limit.
**Rationale**: per instance and per 30 minutes (the drift render interval) there is one render, one dry-run per rendered object and one status patch that moves `renderedAt`; the two other periodic reconciles in that window skip the render and hold no slot. With one slot the renders of N instances run one after another, so they fit while `N x (render time) < 30 minutes`: 360 instances at 5 seconds per render. Past that the queue never drains and renders for spec changes wait behind periodic ones; the remedies are a longer `--drift-render-interval`, more render slots, or `--instance-reconcile-interval=0`. The render time of a real module was not measured in this change. Before this change an unchanged instance rendered once and never again, so this is new load, and it is the price of knowing `Drifted`.

### No write storm

**Context**: a periodic status write would not re-trigger the controller (`GenerationChangedPredicate`), but it would load the API server and wake every watcher of ModuleInstances.
**Explored**: `judgeSkippedInstance` patches `Healthy` alone and the patch is empty when the verdict is the same; `commitNoOpStatus` writes `renderedAt` when the skip is enabled.
**Decision**: no new write. A test counts the writes of a skipped periodic reconcile and expects zero.
**Rationale**: the only periodic write is the existing `renderedAt` move, once per drift render interval per instance. A `NoOp` also emits one `Normal` event, which the API server aggregates.

## Risks / Trade-offs

- [An object that vanishes again after each restore, with `--drift-render-interval=0`, is applied on every health requeue, as fast as every 5 seconds because each apply moves `lastAppliedAt`] → With the default interval the apply happens at most once per 30 minutes. `0` already means "every reconcile renders" and is documented as such; docs/RENDERING.md names this case.
- [A restored object surprises someone who deleted it on purpose] → Documented; `spec.suspend` is the way to stop the operator from acting on an instance.
- [An instance whose rendered Job has a TTL reads `Healthy=False` (`NotRolledOut`, missing) for ever once the Job is gone] → Existing behaviour, not changed here; reported to the supervisor as a follow-up.
- [More renders on a large cluster with one slot] → See "What the periodic reconcile costs"; three flags tune it.
- [The fixed interval is one value for every instance] → Accepted; a `spec.interval` field stays possible.

## Migration Plan

No migration. On upgrade every healthy instance starts to requeue on the interval after its first reconcile. Rollback: `--instance-reconcile-interval=0` stops the periodic reconcile; the restore then only happens on reconciles that other triggers start.
