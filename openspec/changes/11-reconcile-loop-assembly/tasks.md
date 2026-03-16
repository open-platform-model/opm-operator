## 1. Reconcile orchestrator

- [ ] 1.1 Define outcome types (`SoftBlocked`, `NoOp`, `Applied`, `AppliedAndPruned`, `FailedTransient`, `FailedStalled`) in `internal/reconcile/outcome.go`
- [ ] 1.2 Implement `ReconcileModuleRelease` orchestrator struct with dependency fields in `internal/reconcile/modulerelease.go`
- [ ] 1.3 Implement phase 0: load ModuleRelease, check deletion, check suspend, create patch helper
- [ ] 1.4 Implement phase 1: resolve source via `internal/source.Resolve`
- [ ] 1.5 Implement phase 2: fetch and unpack artifact via `internal/source.Fetcher`
- [ ] 1.6 Implement phase 3: render via `internal/render.RenderModule`, compute digests
- [ ] 1.7 Implement phase 4: plan actions — no-op detection, compute stale set
- [ ] 1.8 Implement phase 5: apply via `internal/apply.Apply`
- [ ] 1.9 Implement phase 6: prune via `internal/apply.Prune` (only if spec.prune=true and apply succeeded)
- [ ] 1.10 Implement phase 7: commit status — conditions, digests, inventory, history

## 2. Controller wiring

- [ ] 2.1 Add dependency fields to `ModuleReleaseReconciler` struct
- [ ] 2.2 Replace stub `Reconcile` body with call to orchestrator
- [ ] 2.3 Wire dependencies in `SetupWithManager` or manager setup in `cmd/main.go`

## 3. Tests

- [ ] 3.1 Write envtest integration test: create OCIRepository (mock ready) + ModuleRelease → verify resources appear
- [ ] 3.2 Write envtest test: verify status conditions, digests, and inventory populated after reconcile
- [ ] 3.3 Write envtest test: suspend=true skips reconciliation
- [ ] 3.4 Write envtest test: source not ready → SoftBlocked outcome
- [ ] 3.5 Write envtest test: no-op detection skips apply on second reconcile with same digests

## 4. Validation

- [ ] 4.1 Run `make fmt vet lint test` and verify all checks pass
