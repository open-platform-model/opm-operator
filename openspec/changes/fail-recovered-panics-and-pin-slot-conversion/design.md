## Context

Both workload reconcile loops end in one deferred status commit. They declare their outcome with no initialiser, and the zero value of `Outcome` is `NoOp`:

```go
// internal/reconcile/moduleinstance.go, ReconcileModuleInstance
var (
	outcome    Outcome // zero value: NoOp
	...
	skipCommit bool    // set only when the slot wait is cancelled
)
defer func() {
	if skipCommit {
		return
	}
	if outcome == NoOp {
		commitNoOpStatus(ctx, patcher, &mi, phases, reconcileStart) // MarkReady("Reconciliation succeeded")
		return
	}
	...
}()
```

`ReconcileModulePackage` has the same shape, with the NoOp branch inline. A panic anywhere after the `defer` (renderer, `convertRender`, shrink judgment, apply, prune) unwinds through this func with `outcome` still `NoOp`. The object is marked Ready and its reconcile counter is reset. Then controller-runtime recovers the panic (`RecoverPanic` defaults to true in v0.24.1), returns `panic: <v> [recovered]` as the reconcile error and requeues on its rate limiter.

The render slot is held through conversion on purpose: the export of the rendered set is the heap peak (owner decision op-mem, 2026-10-03). The call sites are `renderAndConvertInstance` (ModuleInstance, called inside `params.RenderSlots.Run`) and the `Run` closure in `renderModulePackage`. `render.Slots` exposes no way to observe a held slot, so no test can tell a conversion inside the slot from one just after it.

Reconcile phase impact: Status only (the deferred commit gains a panic branch). Render gains an injectable conversion func whose production value is unchanged. Source, Apply and Prune are untouched.

## Goals / Non-Goals

**Goals:**

- A panic during a reconcile never leaves `Ready=True` or a reset failure counter behind, on either kind. That includes the drift, apply and prune counters of a phase that was in flight when the panic hit.
- The panic still reaches controller-runtime unchanged, so its log, its panic metric and its rate-limited requeue keep working.
- A deterministic unit test fails when the conversion moves outside the render slot, on either kind.

**Non-Goals:**

- A render timeout (follow-up w1-07, the separate change op-render-timeout).
- Recovering panics in the Platform or TransformerRegistration reconcilers, or in the ModuleInstance and ModulePackage paths that return before the deferred commit is installed. A panic there writes no new status, so a previous Ready persists unchanged; out of scope for this change.
- Changing controller-runtime's `RecoverPanic` setting, or swallowing the panic.
- A Warning event for the panic. controller-runtime already logs it and counts it in `controller_runtime_reconcile_panics_total`, and the condition carries the message.
- Guarding against a panic inside the deferred commit itself (the status patch).

## Research & Decisions

### 1. Detect the panic with `recover()` in the deferred func, not with a completion flag

**Context**: The plan entry offered two shapes: a `completed bool` set on every normal return, or an `outcomeUnset` value 0 so that `NoOp` must be assigned explicitly.

**Explored**: Both detect a missing outcome, but neither gives the panic value, and both need every return path edited (about ten per loop). A path that forgets the flag would then be reported as a panic. `recover()` called directly in the deferred func is non-nil exactly when the reconcile is panicking. Since Go 1.21 this holds for `panic(nil)` too, which arrives as a `*runtime.PanicNilError`.

**Decision**: The deferred func starts with `if r := recover(); r != nil { commitPanic...(r); panic(r) }`. `skipCommit` and the NoOp branch are unchanged and run only when there is no panic.

**Rationale**: One branch per loop. It carries the value for the status message and touches no return path. The zero `Outcome` stays `NoOp`, which keeps `MetricLabel`, `String` and the counter switch as they are. Wave 2's other operator changes (op-j5, op-g3, op-d1, op-render-timeout, op-f5) rebase onto a defer block that changes at one point only.

```go
defer func() {
	if r := recover(); r != nil {
		commitPanicStatus(ctx, patcher, &mi, r, digests, phases, reconcileStart)
		panic(r)
	}
	if skipCommit {
		return
	}
	// unchanged
}()
```

### 2. A panic is a transient failure on controller-runtime's backoff

**Context**: The plan entry left the classification open ("MarkStalled-or-transient"). The deferred commit must pick an outcome for counters, history and metrics, and a condition shape.

**Explored**: (a) `FailedStalled` with `Stalled=True`: says retrying is pointless, which is false while controller-runtime keeps retrying, and the owner cannot fix an operator bug by editing the spec. Rejected. (b) `FailedTransient` with `Reconciling=True` and no `Stalled`: matches what actually happens next (a rate-limited retry). Chosen. (c) Passing the in-flight `phases` to the counter update: the loops set `driftRan`, `applyRan` and `pruneRan` before the call and the matching `*Failed` only when an error returns, so a panic inside drift detection, apply or prune would reset that phase's counter as if it had succeeded. Rejected in favour of a zero `phaseOutcomes{}`.

**Decision**: `commitPanicStatus` (ModuleInstance) and its ModulePackage twin:

1. log the panic value at error level with the object's name and namespace (controller-runtime's panic handler logs the stack; the original frames are still on the goroutine stack when the deferred func re-panics);
2. `status.MarkReconcilePanic(obj, "reconcile panicked: %v", r)`: `conditions.MarkReconciling(obj, ReconcilePanicReason, msg)` (sets Reconciling=True and removes Stalled), then `Ready=False` with the same reason and message;
3. set `observedGeneration` and `lastAttempted*` (action `reconcile`, time, duration, and whichever digests were computed), as the failed branch does today;
4. record a failure history entry with the same message, using the kind's existing recorder;
5. update the failure counters with outcome `FailedTransient` and a zero `phaseOutcomes{}`, so only the reconcile counter moves. The drift, apply and prune counters are left as they were: a phase that was in flight did not succeed, and its `*Failed` flag was never set;
6. clear `nextRetryAt`. The retry is controller-runtime's rate limiter, not the operator's backoff, so the operator schedules no time it does not own;
7. ModuleInstance only: `recordReconcileMetrics(..., FailedTransient, ...)` and `RecordDuration`, as the failed branch does (the ModulePackage commit records no metrics today);
8. patch status with the same owned conditions as the normal commit, and log a patch error without failing.

`lastApplied*` and inventory are not touched: nothing was applied by this attempt in a way the operator can vouch for. Fields the attempt already wrote on the in-memory object before the panic are committed as on any failed attempt: `requiredContracts` (a fact about the render, per the comment where it is written), `instanceUUID`, the `Drifted` condition, and a ModulePackage's `status.source`.

The panicking attempt returns no `ctrl.Result`. It is counted as `FailedTransient` for counters, history and metrics only: the Outcome-classification requeue scenario and the reconcile-backoff `nextRetryAt` rule do not apply to it, and the spec says so.

**Rationale**: Transient, not stalled. A panic is an operator bug or a corrupt input. The object owner cannot fix it by editing the spec, and the next reconcile may succeed, for example after an operator upgrade. `Stalled=True` would also tell tools such as `kstatus` that retrying is pointless, while controller-runtime keeps retrying anyway.

### 3. Re-panic with the original value

**Context**: After the commit the deferred func holds the recovered value and must decide how the reconcile ends.

**Explored**: (a) Swallow the panic and return an error: hides it from `controller_runtime_reconcile_panics_total` and from the stack log, and changes controller-runtime's `RecoverPanic` contract. Rejected. (b) Re-panic with a wrapped value carrying context: changes the value tests and logs see. Rejected. (c) Re-panic with `r` unchanged. Chosen.

**Decision**: `panic(r)` after the commit, with no wrapping.

**Rationale**: controller-runtime's handler formats `r` into the reconcile error and runs its panic handlers, which log the stack. The re-panic runs inside the deferred func before the original frames unwind, so that stack still names the original panic site.

### 4. The slot seam: `Slots.Held` and an unexported `convert` field

**Context**: Supervisor note for w1-02: the test needs a hook in production code. Keep it minimal.

**Explored**: (a) An exported `Convert func(*render.RenderResult) (*convertedRender, error)` field, as the plan entry suggested. Its type names the unexported `convertedRender`, so no caller outside the package can set it, and exporting it adds surface for nothing. (b) A package-level `var convert = convertRender` swapped by tests: global mutable state that races with parallel tests. (c) A `go/ast` test that the `Run` closure contains the call: it breaks on harmless refactors and passes on a wrapper that defers the call. (d) An unexported field on each params struct, nil in production.

**Decision**: (d), plus an accessor on the pool:

```go
// internal/render/slots.go

// Held reports how many slots are taken. A nil pool holds none.
func (s *Slots) Held() int {
	if s == nil {
		return 0
	}
	return len(s.ch)
}

// internal/reconcile
type ModuleInstanceParams struct {
	...
	// convert exports a render result for apply. Nil means convertRender;
	// tests in this package set it to observe the conversion.
	convert func(*render.RenderResult) (*convertedRender, error)
}

func (p *ModuleInstanceParams) convertFn() func(*render.RenderResult) (*convertedRender, error) {
	if p.convert != nil {
		return p.convert
	}
	return convertRender
}
```

The same pair goes on `ModulePackageParams`. `renderAndConvertInstance` takes the convert func as a parameter, and the `renderModulePackage` closure calls `params.convertFn()`.

**Rationale**: No new exported identifier in `internal/reconcile`, no global state, and the production path is unchanged (nil → `convertRender`). `Held` is the smallest observation that makes the test deterministic: on a `NewSlots(1)` pool, a conversion inside the slot sees 1 and one outside sees 0. No timing is involved.

### 5. Where the tests live

- `internal/render`: `Held` is 0 on a nil pool and on a fresh pool, 1 inside `Run` on `NewSlots(2)` with one slot taken, and 0 again after `Run` returns.
- `internal/reconcile`, ModuleInstance: drive `ReconcileModuleInstance` with a fake client (as `default_sa_test.go` and `deletion_test.go` do: the finalizer already present, status subresource enabled), a stub `ModuleRenderer` returning a one-resource result, `RenderSlots: render.NewSlots(1)`, and a `convert` that records `params.RenderSlots.Held()` and then returns a `*conversionError`, so the reconcile stops at Stalled before apply and needs no SSA. Assert that the recorded value is 1, and that `Held()` is 0 after the reconcile returns.
- `internal/reconcile`, ModulePackage: call `renderModulePackage` directly (unexported, same package) with a stub `PackageRenderer` returning `KindModuleInstance`, `NewSlots(1)` and the same recording `convert`. Assert 1 inside and 0 after. This avoids the Flux source, fetch and path phases, which add nothing to the slot question.
- `internal/reconcile`, panic in conversion: the same ModuleInstance harness with a `convert` that panics. Assert that the panic propagates with its value, that `Held()` is 0 afterwards (the slot is freed), and that the stored object has `Ready=False`/`ReconcilePanic`.
- `internal/reconcile`, phase counters, one test per kind. Both start from non-zero drift (ModuleInstance only), apply and prune counters and leave `ResourceManager` nil, so the first use of it panics on a nil receiver after its phase flag is set. ModuleInstance: the panic hits in drift detection, after `driftRan` is set. ModulePackage (driven through `ReconcileModulePackage` with a ready OCIRepository in the fake client, a stub `Fetcher` that writes `instance.cue`, a stub `PackageRenderer` and the recording `convert`): the panic hits in `apply.Apply`, after `applyRan` is set. Assert that the phase counters are unchanged and the reconcile counter went up by one. With the in-flight `phases` passed instead, the drift (or apply) counter would reset to 0, so these tests pin decision 2 step 5.
- `internal/controller` (envtest), a panicking renderer on both kinds, beside `render_memory_test.go`: first reconcile to Ready with the normal stub, then swap the reconciler's `Renderer` to the existing `panickingModuleRenderer` (or `panickingPackageRenderer`) and call `Reconcile` directly. Expect `PanicWith("render blew up")`, and a message containing `reconcile panicked: render blew up`. Re-read the object and expect `Ready=False` reason `ReconcilePanic`, a message containing the value, `Reconciling=True`, no `Stalled`, `failureCounters.reconcile` = 1, a failure history entry, `nextRetryAt` nil, and the inventory and `lastAppliedRenderDigest` unchanged from the successful reconcile.
- Mutation check, by hand, once per kind: move the convert call after `Run` returns. The `internal/reconcile` slot tests must go red, recording `Held()==0`. Revert. Record the result in the section's commit body.

## Risks / Trade-offs

- [Risk] The status patch in the panic branch fails (for example, the API server is unreachable) → it is logged and the panic still propagates, so the worst case is today's behaviour minus the false Ready.
- [Risk] A panic value whose `%v` text is very long makes a long condition message → accepted. Panics carry short values in practice, and the apiserver caps condition messages at 32768 bytes. Truncating would hide the value in the one place a user sees it.
- [Trade-off] The panic branch leaves `nextRetryAt` empty while controller-runtime is in fact retrying → documented in the condition reference. Inventing a time the operator does not control would be worse.
- [Trade-off] `convert` is a production field that exists only for tests → it is unexported and nil in production, and the doc comment says so.

## Sections

1. Report a recovered panic as a failure (status helper, both deferred commits, envtest for both kinds, the phase-counter unit test, the `docs/RENDERING.md` Error reasons row). `fix(controller)`.
2. Pin the conversion inside the render slot (`Held`, the `convert` seam, unit tests, the panic-in-conversion test, the mutation check). `test(reconcile)`: production behaviour is unchanged.
3. Condition reference row. `docs(site)`.
