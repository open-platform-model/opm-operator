## 1. Drift detection function

- [x] 1.1 Define `DriftResult` and `DriftedResource` structs in `internal/apply/drift.go`
- [x] 1.2 Implement `DetectDrift(ctx, resourceManager, resources) (DriftResult, error)` using SSA dry-run
- [x] 1.3 Compare dry-run result against desired objects to identify drifted resources

## 2. Condition constants

- [x] 2.1 Add `DriftedCondition` type constant to `internal/status/`
- [x] 2.2 Add `DriftDetected` reason constant to `internal/status/`
- [x] 2.3 Implement `MarkDrifted(obj, count int)` and `ClearDrifted(obj)` helpers

## 3. Phase 4 integration

- [x] 3.1 Add drift detection call in Phase 4 after no-op digest check
- [x] 3.2 Set `Drifted=True` when drift found, clear after successful apply in Phase 5
- [x] 3.3 On dry-run failure: log warning, increment `failureCounters.drift`, continue reconcile
- [x] 3.4 Ensure drift detection runs even on no-op reconciles

## 4. Tests

- [x] 4.1 Write unit test: `DetectDrift` returns empty result when no drift
- [x] 4.2 Write unit test: `DetectDrift` identifies drifted resources
- [x] 4.3 Write envtest test: drift detected sets `Drifted=True` condition
- [x] 4.4 Write envtest test: successful apply clears `Drifted` condition
- [x] 4.5 Write envtest test: drift on no-op reconcile preserves `Ready=True`

## 5. Validation

- [x] 5.1 Run `make fmt vet lint test` and verify all checks pass
