## Why

Changes 1-9 build isolated capabilities: inventory bridge, source resolution, artifact fetch, CUE rendering, digests, conditions, SSA apply, prune, and history. None of them are wired into the actual `ModuleReleaseReconciler.Reconcile` method, which is still a no-op stub. This change assembles all phases into the real reconcile loop, replacing the stub with a working controller.

## What Changes

- Implement the reconcile orchestration in `internal/reconcile/modulerelease.go` that runs phases 0-7.
- Wire the orchestrator into `ModuleReleaseReconciler.Reconcile`, replacing the TODO stub.
- Implement outcome classification: `SoftBlocked`, `NoOp`, `Applied`, `AppliedAndPruned`, `FailedTransient`, `FailedStalled`.
- Handle suspend check, patch helper setup, and deferred status commit.

## Capabilities

### New Capabilities
- `reconcile-loop`: Full ModuleRelease reconcile loop executing all phases from source resolution through status commit, with outcome classification and error handling.

### Modified Capabilities

## Impact

- `internal/reconcile/modulerelease.go` — stub replaced with full orchestration.
- `internal/controller/modulerelease_controller.go` — stub `Reconcile` body replaced with real logic, dependencies injected.
- This change depends on ALL previous changes (1-9) being merged.
- Tests require envtest with mock Flux source-controller CRDs.
- SemVer: MINOR — new capability (the controller actually does something now).
