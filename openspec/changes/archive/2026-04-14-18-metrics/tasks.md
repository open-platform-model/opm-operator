## 1. Metric definitions

- [x] 1.1 Create `internal/metrics/metrics.go` with Prometheus metric variable definitions
- [x] 1.2 Define `ReconcileTotal` counter vec with labels `name`, `namespace`, `outcome`
- [x] 1.3 Define `ReconcileDuration` histogram vec with labels `name`, `namespace`
- [x] 1.4 Define `ApplyResourcesTotal` counter vec with labels `name`, `namespace`, `action`
- [x] 1.5 Define `PruneResourcesTotal` counter vec with labels `name`, `namespace`
- [x] 1.6 Define `InventorySize` gauge vec with labels `name`, `namespace`

## 2. Metric registration

- [x] 2.1 Register all metrics with `controller-runtime/pkg/metrics.Registry` in `init()`
- [x] 2.2 Import `internal/metrics` in `cmd/main.go` to trigger registration

## 3. Reconcile integration

- [x] 3.1 Record reconcile outcome and duration in Phase 7 status commit
- [x] 3.2 Record apply resource counts after Phase 5
- [x] 3.3 Record prune resource count after Phase 6
- [x] 3.4 Update inventory size gauge after Phase 7

## 4. Tests

- [x] 4.1 Write unit test: verify metrics registered with correct names and labels
- [x] 4.2 Write unit test: verify metric recording functions update counters/gauges correctly

## 5. Validation

- [x] 5.1 Run `make fmt vet lint test` and verify all checks pass
