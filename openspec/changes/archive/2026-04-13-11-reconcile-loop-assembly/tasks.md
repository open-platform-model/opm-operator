## 1. Reconcile orchestrator

- [x] 1.1 Define outcome types (`SoftBlocked`, `NoOp`, `Applied`, `AppliedAndPruned`, `FailedTransient`, `FailedStalled`) in `internal/reconcile/outcome.go`
- [x] 1.2 Implement `ReconcileModuleRelease` orchestrator struct with dependency fields in `internal/reconcile/modulerelease.go`
- [x] 1.3 Implement phase 0: load ModuleRelease, check deletion, check suspend, create patch helper
- [x] 1.4 Implement phase 1: resolve source via `internal/source.Resolve`
- [x] 1.5 Implement phase 2: fetch and unpack artifact via `internal/source.Fetcher`
- [x] 1.6 Implement phase 3: render via `internal/render.RenderModule`, compute digests
- [x] 1.7 Implement phase 4: plan actions — no-op detection, compute stale set
- [x] 1.8 Implement phase 5: apply via `internal/apply.Apply`
- [x] 1.9 Implement phase 6: prune via `internal/apply.Prune` (only if spec.prune=true and apply succeeded)
- [x] 1.10 Implement phase 7: commit status — conditions, digests, inventory, history

## 2. Controller wiring

- [x] 2.1 Add dependency fields to `ModuleReleaseReconciler` struct
- [x] 2.2 Replace stub `Reconcile` body with call to orchestrator
- [x] 2.3 Wire dependencies in `SetupWithManager` or manager setup in `cmd/main.go`

## 3. Tests

- [x] 3.1 Write envtest integration test: create OCIRepository (mock ready) + ModuleRelease → verify resources appear
- [x] 3.2 Write envtest test: verify status conditions, digests, and inventory populated after reconcile
- [x] 3.3 Write envtest test: suspend=true skips reconciliation
- [x] 3.4 Write envtest test: source not ready → SoftBlocked outcome
- [x] 3.5 Write envtest test: no-op detection skips apply on second reconcile with same digests

## 4. Validation

- [x] 4.1 Run `make fmt vet lint test` and verify all checks pass
