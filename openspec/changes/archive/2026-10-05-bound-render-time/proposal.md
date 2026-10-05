## Why

The ModuleInstance and ModulePackage reconcilers share one pool of render slots, sized by `--max-concurrent-renders` (default 1, opm-operator#192). Nothing on the render path has a timeout. A render that hangs, for example on registry I/O during acquisition or synthesis, keeps its slot until it returns. At the default of one slot that stops every other render of both kinds, and the stuck object shows only `Reconciling=True` with no reason. `docs/RENDERING.md` names this gap today ("Nothing on the render path has a timeout").

Library v1.0.0-beta.6 (library#205) checks the context between the stages of every kernel verb (acquire, synthesis, render), so a cancelled render context now stops a render at its next stage boundary. A deadline is therefore effective, which it was not before. A stage that is already running inside CUE still runs to its end, and a dependency fetch inside CUE's loader takes no context, so a registry that hangs there holds the render until the fetch returns.

The slot is a memory bound. One render of a cert-manager-sized module peaks near 2 GiB of heap. If the slot were given back when the deadline fires while the stage keeps running, a second large render could start beside it and the pod could exceed its 4 GiB limit. So the timeout must stop waiting for the render. It must not free the slot before the render's memory is free.

## What Changes

- **A new manager flag `--render-timeout`** (duration, default `10m`). It is the longest a single render may run once it holds its slot: the renderer call (platform lease, acquisition, synthesis, render build) and the export of the result for apply. The wait for a slot is not counted. `0` disables the deadline, and the render then runs exactly as today. A negative value is refused at startup. The value is one constant in `cmd/main.go` and one flag, so the owner can tune it later without other changes.
- **`render.Slots.Run` gives its body a context with that deadline.** Only the body is bounded. The reconcile's own context, which the deferred status patch, apply, prune and events use, keeps no deadline.
- **When the deadline passes, the reconcile stops waiting and the render keeps its slot.** The render body runs in its own goroutine. When the deadline passes before it returns, `Run` returns `render.ErrRenderTimedOut` at once. The goroutine keeps the slot until the render really returns (at the next stage boundary, or when the running stage ends), and only then gives it back. So `--max-concurrent-renders` still bounds the builds in memory. A render that returns after its deadline is treated as timed out too, even if it succeeded, and its result is discarded.
- **A timeout is a transient failure with a new reason, `RenderTimedOut`.** The object reports `Ready=False` with reason `RenderTimedOut` and no `Stalled` condition. A Warning event carries the message. The outcome is `FailedTransient`, so the object retries on the bounded exponential backoff (5s doubling to 5 minutes, driven by `failureCounters.reconcile`), never on the 30-minute stalled recheck. Inventory and last-applied digests keep the last success.
- **The render goroutine owns what it reads until it returns.** The ModuleInstance render reads a copy of the spec inputs made before the goroutine starts, so the status patch cannot race with it. The ModulePackage artifact directory is handed to the render, which removes it when it returns, so a render that runs past its deadline does not lose its files.
- **One render per object.** While an object's timed-out render is still running, its retries do not start another render or take a slot. They report `RenderTimedOut` (the previous render is still running) on the backoff. One hung object holds at most one slot, as it does today.
- **A panic in the render body is still recorded.** A panic before the deadline is logged with the stack of the panicking frame and raised again on the reconcile goroutine after the slot is free, so the existing `ReconcilePanic` path records it as before. A panic after the reconcile has stopped waiting cannot reach the reconcile any more. It is logged at error level with the value and the stack, and the slot is freed. The process keeps running.
- **Logs.** The reconcile logs `Render timed out` with the timeout. When an abandoned render returns, the goroutine logs how long it ran, so a slot that is held a long time can be seen in the log.
- **Docs.** `docs/RENDERING.md` gains a `--render-timeout` section in place of the paragraph that says there is no timeout, and a `RenderTimedOut` row in its Error reasons table. `docs/site/diagnostics/operator-conditions.md` gains a `RenderTimedOut` row.

## Classification

**MINOR** after GA (a new flag with a default and a new `Ready` reason, no API type or CRD schema change). During beta it ships as the next `1.0.0-beta.N` under a `feat` PR title. Nothing is removed. With the default of `10m` a render that finishes today still finishes. Only a render that runs longer than 10 minutes changes behaviour: it now reports `RenderTimedOut` and retries, where before it held the slot with no signal.

Complexity (Principle VII): one goroutine per bounded render, one channel, and a set of the objects whose render is running replace the plain call inside `Slots.Run`. The set is what keeps one hung object from taking more than one slot across retries. The goroutine is needed so that the object reports the timeout even when the running stage never returns, while the slot stays held. A plain synchronous deadline was rejected: the reconcile would wait for the stage too, so a stuck stage would still leave the object with no reason. Giving the slot back at the deadline was rejected because it breaks the memory bound.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `library-kernel-runtime`: a new requirement says that a render is bounded by `--render-timeout`, and that its slot is held until the render returns.
- `reconcile-backoff`: a new requirement says that a render timeout is a transient failure with reason `RenderTimedOut` on the bounded backoff.
- `status-conditions`: "Reason constants" adds `RenderTimedOut`.

## Impact

- `internal/render/slots.go`: `Run` takes the timeout and gives its body a context; `ErrRenderTimedOut`.
- `internal/reconcile/moduleinstance.go` and `internal/reconcile/modulepackage.go`: the render calls, classification of a timeout, `RenderTimeout` on both params, the copied instance inputs, and the hand-off of the extract directory to the render.
- `internal/controller/moduleinstance_controller.go`, `internal/controller/modulepackage_controller.go`: a `RenderTimeout` field passed through.
- `cmd/main.go`: the flag, its validation and the startup log.
- `internal/status/conditions.go`: `RenderTimedOutReason` and `MarkRenderTimedOut`.
- Tests in `internal/render`, `internal/reconcile` and `internal/controller` (envtest), with stub renderers that block on `ctx.Done()`.
- Docs: `docs/RENDERING.md`, `docs/site/diagnostics/operator-conditions.md`. `config/manager/manager.yaml` is left alone: a change under `config/` holds the operator module's release gate until an operator release carries it, which a comment does not justify.
- Out of scope: the operator module (`modules/opm_operator`) does not expose the flag; the pod uses the default. The artifact fetch of a ModulePackage and the Platform build hold no slot and get no deadline here. A stage that never returns still holds its slot until the pod restarts. The timeout makes that visible but cannot reclaim the memory.
- Downstream: none. No library pin change (beta.6 already has the stage checks) and no cli change.
- No enhancement decision backs this change, so there is no `enhancement.yaml`.
