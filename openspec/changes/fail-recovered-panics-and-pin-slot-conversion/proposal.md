## Why

Two follow-ups from the render-memory work (`bound-render-memory`, kernel plan wave 1) touch the same two functions, the deferred status commits of the ModuleInstance and ModulePackage reconcile loops, and the render slot they hold.

**A recovered panic reports success (follow-up w1-08).** Both loops declare `outcome Outcome` with no initialiser, and `Outcome`'s zero value is `NoOp` (`internal/reconcile/outcome.go`). The deferred status commit runs while a panic unwinds. It sees `outcome == NoOp` and takes the NoOp branch:

- ModuleInstance: `commitNoOpStatus` (`moduleinstance.go`) calls `status.MarkReady(mi, "Reconciliation succeeded")`, resets the reconcile failure counter and clears `nextRetryAt`.
- ModulePackage: the deferred func (`modulepackage.go`) does the same inline.

controller-runtime v0.24.1 recovers reconcile panics by default (`RecoverPanic` unset means true; `cmd/main.go` does not override it), turns them into an error and requeues on its rate limiter. No operator code calls `recover()`. So a panic in render, conversion, apply or prune leaves the object `Ready=True` while every reconcile keeps panicking. The `skipCommit` flag added in wave 1 covers only the slot-wait cancel.

**Nothing pins conversion inside the render slot (follow-up w1-02).** The slot is held through conversion (render digest and `toUnstructured`) because the memory baseline puts the heap peak there (owner decision op-mem, 2026-10-03 walkthrough). Today `renderAndConvertInstance` and the ModulePackage `Run` closure call `convertRender` inside `Slots.Run`. No test fails if the call moves out: `render_memory_test.go` covers only the nil-out of the resources. The wave-1 reviewer's two-reconcile probe with `NewSlots(1)` passed 3/3 with conversion moved out, because it races. A deterministic test needs a hook in production code.

## What Changes

- **A recovered panic is reported as a failure.** In both deferred commits, the deferred func calls `recover()` first. On a panic it records the attempt as a failure and then re-panics with the same value, so controller-runtime still logs it, counts it in `controller_runtime_reconcile_panics_total` and requeues on its rate limiter. Recording the failure means: `Ready=False` with the new reason `ReconcilePanic` and the message `reconcile panicked: <value>`, `Reconciling=True`, `Stalled` removed, a failure history entry, the reconcile failure counter incremented (outcome `FailedTransient`), `nextRetryAt` cleared, `lastAttempted*` set, `lastApplied*` and inventory untouched, and the status patched. The ModuleInstance path also records its reconcile metrics as `failed_transient`. The operator logs the panic value and the goroutine stack once before it re-panics.
- **A new reason constant, `status.ReconcilePanicReason` (`"ReconcilePanic"`)**, and a helper `status.MarkReconcilePanic` that sets the condition shape above.
- **A test seam for the slot.** `render.Slots` gains `Held() int`, the number of slots taken (0 for a nil pool). `ModuleInstanceParams` and `ModulePackageParams` gain an unexported `convert` field, nil in production, which falls back to `convertRender`. Unit tests in `internal/reconcile` set a `convert` that records `RenderSlots.Held()` and assert that it is 1 for both kinds. The mutation check is done by hand: moving the conversion out of `Slots.Run` turns those tests red.
- **Docs.** `docs/site/diagnostics/operator-conditions.md` gains a `Ready`: `ReconcilePanic` row.

## Classification

**PATCH** (pre-GA; ships as the next `-beta.N` under a `fix` commit). No API type, field or CRD schema changes. One new user-visible `Ready` reason, `ReconcilePanic`, appears only where the operator wrongly reported `ReconciliationSucceeded` before. The seam is unexported, and `Slots.Held` lives in an `internal` package.

Complexity (Principle VII): one `recover()` branch per deferred commit and one condition helper fix a false success. The seam is one unexported field and one accessor. A structural `go/ast` test that the `Run` closure contains the call was the alternative. It was rejected because a refactor that renames or wraps the call breaks it without any change in behaviour.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `reconcile-loop-assembly`: a new requirement says a panic during a ModuleInstance or ModulePackage reconcile is recorded as a failed attempt with reason `ReconcilePanic`, never as a success, and still propagates.
- `status-conditions`: "Reason constants" adds `ReconcilePanic`.
- `library-kernel-runtime`: "Render concurrency is a manager flag bounded by memory" gains a scenario that the conversion of the result for apply runs while the reconcile holds its slot.

## Impact

- `internal/status/conditions.go` (reason and helper).
- `internal/reconcile/moduleinstance.go` (deferred commit, `renderAndConvertInstance`, `ModuleInstanceParams.convert`) and `internal/reconcile/modulepackage.go` (deferred commit, `renderModulePackage`, `ModulePackageParams.convert`).
- `internal/render/slots.go` (`Held`).
- Tests: `internal/render` (`Held`), `internal/reconcile` (slot seam, a panic in conversion), `internal/controller` (envtest: a panicking renderer on both kinds).
- Docs: `docs/site/diagnostics/operator-conditions.md`.
- Checked and out of scope: the Platform and TransformerRegistration reconcilers have no deferred status commit (no `defer func` under `internal/controller`), so a panic there leaves their status as it was and reports no false success. The deletion and suspend paths return before the deferred commit is installed.
- Downstream: none. No library pin and no cli change. This change merges first among the wave-2 operator changes, because op-j5, op-g3, op-d1, op-render-timeout and op-f5 all edit the same deferred commit.
- No enhancement decision backs this change, so there is no `enhancement.yaml`.
