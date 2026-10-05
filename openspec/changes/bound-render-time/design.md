## Context

Both workload reconcilers render inside `params.RenderSlots.Run(ctx, func() {...})`:

- ModuleInstance (`internal/reconcile/moduleinstance.go`, the Phase 1 block of `ReconcileModuleInstance`): `renderAndConvertInstance` calls `Renderer.RenderModule` and the convert function, and writes `renderResult`, `converted` and `err` through the closure.
- ModulePackage (`internal/reconcile/modulepackage.go`, `renderModulePackage`): `Renderer.Render(ctx, packageDir)` and the convert function, writing `kind`, `result`, `converted` and `err` through the closure.

`Slots.Run` (`internal/render/slots.go`) takes a slot with `Acquire(ctx)`, calls `fn` on the caller's goroutine, and releases with a deferred call, so a panic in `fn` frees the slot and reaches the reconcile's deferred `recover()` (the `ReconcilePanic` path). A failed wait returns the context error, and the caller skips its status commit.

The deferred status commit in each loop patches with the reconcile's `ctx`. The ModulePackage loop creates the artifact directory before the render and removes it with `defer os.RemoveAll(extractDir)` in `ReconcileModulePackage`. The renderers take their platform lease inside the render call (`kernel_module_renderer.go`, `kernel_package_renderer.go`), so the lease already lasts as long as the render itself.

Library v1.0.0-beta.6 checks `ctx.Err()` between stages in `kernel/acquire.go`, `kernel/synth.go`, `kernel/render.go` and the registry loader (library#205). A render whose context is done stops at the next check and returns an error that wraps the context error. A stage already inside CUE evaluation does not observe the context until it returns.

## Goals / Non-Goals

**Goals:**

- One flag, `--render-timeout` (default `10m`), bounds how long a render may run once it holds its slot.
- A timed-out render is reported on the object as a transient failure with reason `RenderTimedOut`, on the bounded backoff.
- The slot is held until the render really returns, so `--max-concurrent-renders` still bounds the builds in memory.
- Nothing the render goroutine reads is changed or removed under it, and nothing it writes is read by the reconcile after a timeout.

**Non-Goals:**

- Interrupting a CUE stage that is already running. That needs the library, and the memory bound would not allow freeing the slot early anyway.
- A deadline on the ModulePackage artifact fetch, the Platform build, apply or prune. They hold no slot.
- Exposing the flag in the operator module (`modules/opm_operator`).
- A metric for abandoned renders. The log line is enough until someone asks for more.

## Decisions

### 1. `Slots.Run` owns the deadline and the goroutine

```go
// ErrRenderTimedOut reports that a render did not finish within its
// timeout. The render may still be running; its slot stays held until it
// returns.
var ErrRenderTimedOut = errors.New("render timed out")

// Run takes a slot and calls fn with a context bounded by timeout.
// timeout <= 0 calls fn on the caller's goroutine with ctx unchanged, as before.
func (s *Slots) Run(ctx context.Context, timeout time.Duration, fn func(context.Context)) error {
	release, err := s.Acquire(ctx) // the wait is not counted against the timeout
	if err != nil {
		return err
	}
	if timeout <= 0 {
		defer release()
		fn(ctx)
		return nil
	}
	renderCtx, cancel := context.WithTimeout(ctx, timeout)
	log := logf.FromContext(ctx)
	done := make(chan any, 1) // the recovered panic value, or nil
	start := time.Now()
	go func() {
		defer cancel()
		var p any
		defer func() { release(); done <- p }() // slot freed before anyone hears of the return
		defer func() { p = recover() }()
		fn(renderCtx)
	}()
	select {
	case p := <-done:
		if p != nil {
			panic(p) // before the deadline: the reconcile's recover() records it
		}
		if renderCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
			return ErrRenderTimedOut // returned, but only after its deadline
		}
		return nil
	case <-renderCtx.Done():
		if ctx.Err() != nil {
			// Parent cancelled (shutdown): wait for fn as today.
			if p := <-done; p != nil {
				panic(p)
			}
			return nil
		}
		go logAbandoned(log, start, done) // logs the return, and a late panic with its stack
		return ErrRenderTimedOut
	}
}
```

The sketch shows the order that matters, not the final code. In the real code the panic value and the stack are captured together, so a late panic can be logged with its stack. `RunBounded` as a separate method was considered. It was rejected because the two callers are the only users and `Slots` is internal: one `Run` with a timeout keeps a single entry point, and `timeout <= 0` keeps today's exact path for tests and for an owner who turns the deadline off.

**Contract for callers:** when `Run` returns `ErrRenderTimedOut`, the caller MUST NOT read anything `fn` writes. `fn` may still be writing it. When `Run` returns nil, `fn` has returned. The channel receive orders its writes before the caller's reads.

### 2. A render that returns after its deadline is a timeout

When the deadline passes, the render's error wraps `context.DeadlineExceeded` from a library stage check, and `select` may see `done` and `renderCtx.Done()` at the same moment. Either way `Run` returns `ErrRenderTimedOut` whenever the render context's deadline has passed and the parent context is live. That holds even when `fn` happened to succeed at the boundary. The rule is simple and deterministic: a render that did not finish within the timeout is a timeout. The success is not lost for long, because the next try comes on the backoff.

The caller does not try to detect a timeout from `fn`'s error. A `context.DeadlineExceeded` from somewhere else, such as an HTTP client timeout inside a registry fetch that finished in time, keeps its present classification.

### 3. Classification: transient, `RenderTimedOut`, bounded backoff

Both callers check `errors.Is(waitErr, render.ErrRenderTimedOut)` before any other classification, and ignore the closure's variables in that branch:

- ModuleInstance: `status.MarkNotReady(mi, status.RenderTimedOutReason, msg)`, a Warning event with reason `RenderTimedOut`, `outcome = FailedTransient`, `errMsg = msg`, `retryAfter = retryIntervalFor(FailedTransient, reconcileFailureCount(...))`. Return `ctrl.Result{RequeueAfter: retryAfter}, nil`, so the deferred commit runs: failure history, `failureCounters.reconcile` + 1, `nextRetryAt` set, `lastAttempted*` set, inventory and `lastApplied*` untouched.
- ModulePackage: the same condition and event, and `&phaseFail{FailedTransient, msg, modulePackageBackoff(pkg)}`.

The message is `render did not finish within <timeout>; its render slot stays held until the render returns`. The other `waitErr` path, a cancelled wait for a slot, keeps `skipCommit` as today. `MarkNotReady` already sets `Ready=False`, sets `Reconciling=True` and removes `Stalled`, so no new helper is needed. One is added only if the implementation shows otherwise.

`RenderTimedOut` is a reason of its own and not `ResolutionFailed`. The fix differs: a timeout points at a slow registry, a very large module or a timeout set too low, not at an unresolved module. It is transient because the cause is usually temporary, and nothing else re-enqueues the object.

### 4. The goroutine owns its inputs

- **ModuleInstance.** Before calling `Run`, the reconcile copies what the render reads: `name`, `namespace`, `spec.module.path`, `spec.module.version` and `spec.values.DeepCopy()`. `renderAndConvertInstance` takes those values instead of `*ModuleInstance`. The deferred patch can update `mi` from the server's answer while an abandoned render is still reading. The copy is what removes that race.
- **ModulePackage.** The extract directory gets two owners: the reconcile and the render. A small helper (`sharedDir`, an `atomic.Int32` set to 2 and a `Release()` that removes the directory when the count reaches 0) replaces the plain `defer os.RemoveAll(extractDir)`. The reconcile releases on return. The render closure releases when it returns. On a path where no render starts (the fetch succeeded but `navigateModulePackagePath` failed, or the wait for a slot failed), the reconcile releases both shares. `packageDir` is a string, so nothing else is shared.
- **Lease.** The renderers already take and release the platform lease inside the call, so the Platform reconciler's prune keeps skipping the directory an abandoned render reads. Nothing changes there.

### 5. Flag, wiring and validation

```go
const defaultRenderTimeout = 10 * time.Minute

flag.DurationVar(&renderTimeout, "render-timeout", defaultRenderTimeout,
	"Longest one ModuleInstance or ModulePackage render may run once it holds a render slot "+
		"(platform lease, acquisition, synthesis, the render build and the export for apply); "+
		"the wait for a slot is not counted. A render past it reports Ready=False with reason "+
		"RenderTimedOut and retries on the backoff. Its slot stays held until the render "+
		"returns, at the next stage, so --max-concurrent-renders still bounds memory. "+
		"0 disables the deadline.")
```

`validateRenderTimeout` refuses a negative value, as `validateDriftRenderInterval` does, and the startup log line gains `renderTimeout`. The reconciler structs and the params structs get a `RenderTimeout time.Duration` field. Zero, the value tests get when they leave it unset, keeps today's synchronous path.

The default of 10 minutes is far above any render measured so far: enhancement 0019 measured a 129-component module at about two seconds a render, and a cold module cache at 1.76 s more. It is also far below "never". A render that takes longer is almost certainly waiting on I/O, not computing.

### 6. Tests

- `internal/render`: a stub body that blocks on `ctx.Done()` and then waits on a test channel, which stands for a CUE stage that is still running. `Run` returns `ErrRenderTimedOut` soon after a short timeout. `Held()` is still 1 after `Run` returns, and 0 once the test channel is closed. Also: a body that returns after the deadline gives `ErrRenderTimedOut`; a body that returns in time gives nil, and its writes can be read; a panic before the deadline is raised again on the caller with the slot already free; a panic after the deadline does not crash the test binary, frees the slot and is logged; `timeout = 0` calls the body on the caller's goroutine with the caller's context (no deadline); a cancelled parent context during the body makes `Run` wait for the body and return nil. Run with `-race`.
- `internal/reconcile`: a `ModuleRenderer` stub that blocks until `ctx.Done()` and returns `ctx.Err()`, with `RenderTimeout` set to a few milliseconds and `NewSlots(1)`. The stored instance has `Ready=False` reason `RenderTimedOut`, no `Stalled`, `failureCounters.reconcile` 1, `nextRetryAt` set, and the result is `RequeueAfter: 5s`. That the status was patched shows the patch used the live parent context. The ModulePackage version is the same, and it also checks that the extract directory still exists while the stub blocks after its deadline, and is gone once the stub returns.
- `internal/controller` (envtest): on a shared `NewSlots(1)`, a ModuleInstance whose render times out and then keeps blocking holds the slot. A ModulePackage reconciled next waits for the slot and reaches Ready only after the blocked render is released. This shows the memory bound across both kinds.

## Research & Decisions

### Where the deadline applies

**Context**: The deadline could cover the whole reconcile, the slot wait plus the render, or only the slot body.
**Explored**: `cmd/main.go` flag set; `Slots.Run` and its two callers; the deferred status commits, which patch with the reconcile `ctx`; library v1.0.0-beta.6 stage checks (`ctx.Err()` in `kernel/acquire.go`, `kernel/synth.go`, `kernel/render.go`, `internal/loader/registry.go`).
**Decision**: Only the slot body.
**Rationale**: A deadline on the reconcile context would also cancel the status patch, and a timeout would then never reach the object. Counting the slot wait would time out renders that are only queued behind others, which is a sizing question for `--max-concurrent-renders`, not a stuck render.

### Free the slot at the deadline, or hold it until return

**Context**: The plan allowed either. Freeing at the deadline unblocks other renders sooner.
**Explored**: `docs/RENDERING.md` memory figures (a cert-manager-sized export peaks near 2 GB of heap; the pod limit is 4 GiB, `GOMEMLIMIT` 3276 MiB).
**Decision**: Hold it until the render returns.
**Rationale**: The slot is the memory bound. An abandoned stage still holds its build, so a freed slot would let a second large render start beside it, and two such renders can go over the limit. The reconcile returning early keeps what matters: the object reports the timeout.

### Synchronous deadline or goroutine

**Context**: Calling `fn(renderCtx)` on the reconcile goroutine needs no goroutine, no input copy and no shared directory ownership.
**Explored**: How long a stage can run after the context is done. The library checks only between stages, so a registry call inside a stage that hangs keeps the stage running.
**Decision**: A goroutine per bounded render.
**Rationale**: With a synchronous call, a stage that hangs keeps the reconcile waiting too, and the object never reports why. The goroutine is what makes `RenderTimedOut` visible in exactly the case the flag exists for.

### Default value

**Context**: The flag needs a default that never cuts off a normal render.
**Explored**: The render benchmarks of enhancement 0019 (experiment 08: about two seconds for a 129-component render; experiment 04: 1.76 s for a cold module cache).
**Decision**: `10m`, defined as one constant.
**Rationale**: The default is two orders of magnitude above any measured render, and short enough that a hung render is reported within one cycle of the backoff cap.

## Reconcile phase impact

- **Source/Fetch**: unchanged. The ModulePackage extract directory now has two owners (decision 4).
- **Render**: bounded by `--render-timeout`. A timeout ends the attempt as `FailedTransient` / `RenderTimedOut`.
- **Apply/Prune**: not reached on a timeout. The inventory is untouched.
- **Status**: the deferred commit runs with the parent context. History, counters and `nextRetryAt` follow the transient path.

## Risks / Trade-offs

- [A stage that never returns holds a slot forever] → It did so before this change too. Now the object reports `RenderTimedOut` on every retry and the log shows the abandoned render. A pod restart reclaims the slot. The docs say so.
- [Retries of an object whose earlier render still holds the only slot wait for that slot] → Intended: that is the memory bound. The retry waits in `Acquire` with no deadline, so it is never reported as a timeout while it is only queued.
- [A late panic is only logged] → After the reconcile has returned, nothing can record it on the object. The next attempt renders again, and a panic that repeats arrives before the deadline and is recorded as `ReconcilePanic`.
- [A success just past the deadline is discarded] → One extra render on the backoff. The rule stays deterministic and easy to test.
- [Other open operator changes edit the same render blocks, for example the move of the required-contracts reading to the render diagnostics] → Whichever merges second is brought up to date with the other. The edits are in different statements of the same functions.
