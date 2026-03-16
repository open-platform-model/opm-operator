## Context

The design doc (`module-release-reconcile-loop.md`) specifies 8 phases (0-7) with clear entry/exit conditions per phase. Changes 1-9 implement each phase's internal logic in isolation. This change wires them into a single orchestrated flow.

The reconciler follows the Flux pattern: create a patch helper early, run through phases, set conditions based on outcomes, and patch status at the end.

## Goals / Non-Goals

**Goals:**
- Implement `ReconcileModuleRelease` orchestrator in `internal/reconcile` that accepts all phase dependencies and runs phases 0-7.
- Replace the stub `Reconcile` method in `ModuleReleaseReconciler`.
- Inject dependencies (source resolver, fetcher, renderer, applier, etc.) into the reconciler struct.
- Implement outcome classification and requeue behavior.
- Handle the suspend check and finalizer patterns.

**Non-Goals:**
- Drift detection (deferred per design doc — detection only, no correction).
- Finalizer-based cleanup on deletion (deferred).
- BundleRelease reconcile loop (separate future change).

## Decisions

### 1. Dependencies injected via reconciler struct fields

The `ModuleReleaseReconciler` gains fields for each dependency: source resolver, artifact fetcher, module renderer, resource applier, etc. These are set during `SetupWithManager`. This follows standard controller-runtime patterns and enables testing with mocks.

### 2. Phases run sequentially, errors halt progression

Each phase returns either success (proceed to next) or an error (classify and stop). No parallel phase execution. This keeps the flow simple and debuggable.

### 3. Outcome classification drives requeue and conditions

| Outcome | Ready | Reconciling | Requeue |
|---|---|---|---|
| SoftBlocked | Unknown | True | Wait for event |
| NoOp | True | False | None |
| Applied | True | False | None |
| AppliedAndPruned | True | False | None |
| FailedTransient | False | True | Backoff requeue |
| FailedStalled | False | False (Stalled=True) | Wait for change |

### 4. Status patched once at end via deferred function

A `defer` block runs the serial patcher after all phases complete. This ensures status is always updated, even on early returns or panics.

### 5. Temp directory lifecycle managed by reconcile orchestrator

The reconciler creates a temp dir for artifact extraction and defers `os.RemoveAll`. The fetcher and renderer both operate on this directory.

## Risks / Trade-offs

- **[Risk] Long reconcile duration** — CUE evaluation + apply could take significant time. Mitigation: acceptable for v1alpha1; add timeout context if needed.
- **[Risk] Status patch conflicts** — Another process may modify the status between our read and patch. Mitigation: `SerialPatcher` handles this via resource version-based optimistic concurrency.
- **[Trade-off] No parallel phases** — Sequential execution is simpler but slower. Acceptable for v1alpha1 where correctness beats performance.
