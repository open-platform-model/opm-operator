## 1. Counter helper functions

- [x] 1.1 Implement `EnsureCounters(status *ModuleReleaseStatus) *FailureCounters` — initializes if nil
- [x] 1.2 Implement `IncrementCounter(counters *FailureCounters, field string)` in `internal/status/`
- [x] 1.3 Implement `ResetCounter(counters *FailureCounters, field string)` in `internal/status/`

## 2. Phase 7 integration

- [x] 2.1 Collect phase outcomes during phases 1-6 (which phases succeeded/failed)
- [x] 2.2 In Phase 7, apply counter updates: increment failed phase counters, reset succeeded phase counters
- [x] 2.3 Reset `reconcile` counter on overall success, increment on any failure

## 3. Tests

- [x] 3.1 Write unit test: `EnsureCounters` initializes nil counters
- [x] 3.2 Write unit test: `IncrementCounter` increments correct field
- [x] 3.3 Write unit test: `ResetCounter` zeros correct field without affecting others
- [x] 3.4 Write envtest test: failed reconcile increments reconcile counter
- [x] 3.5 Write envtest test: successful reconcile resets counters to 0

## 4. Validation

- [x] 4.1 Run `make fmt vet lint test` and verify all checks pass
