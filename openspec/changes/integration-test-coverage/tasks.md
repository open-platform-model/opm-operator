## 1. Integration stubs — Change Propagation

- [ ] 1.1 Create `test/integration/reconcile/change_propagation_test.go` with Ginkgo stubs for: values change triggers re-apply (design 1.1), source revision change triggers re-apply (design 1.2)

## 2. Integration stubs — Stale Pruning

- [ ] 2.1 Create `test/integration/reconcile/stale_pruning_test.go` with Ginkgo stubs for: render removes resource → pruned (design 2.1), prune=false skips stale (design 2.2), multiple resources partial stale (design 2.3)

## 3. Integration stubs — State Recovery

- [ ] 3.1 Create `test/integration/reconcile/state_recovery_test.go` with Ginkgo stubs for: Stalled→Ready (design 3.1), SoftBlocked→Ready (design 3.2), suspend→unsuspend (design 3.3)

## 4. Integration stubs — Status Tracking

- [ ] 4.1 Create `test/integration/reconcile/status_tracking_test.go` with Ginkgo stubs for: ObservedGeneration (design 4.1), history across outcomes (design 4.2), ForceConflicts passthrough (design 4.3), cross-namespace source (design 4.4)

## 5. E2E stubs — Full Lifecycle

- [ ] 5.1 Create `test/e2e/lifecycle_test.go` with Ginkgo stubs for: full create→Ready→update→delete lifecycle (design 5.1), real OCI artifact fetch (design 5.2)

## 6. E2E stubs — Concurrent Reconciliation

- [ ] 6.1 Create `test/e2e/concurrent_test.go` with Ginkgo stubs for: multiple ModuleReleases from same source (design 6.1), controller restart mid-reconcile (design 6.2)

## 7. Validation

- [ ] 7.1 Run `task dev:fmt dev:vet dev:test` and verify all stubs compile and are skipped correctly
