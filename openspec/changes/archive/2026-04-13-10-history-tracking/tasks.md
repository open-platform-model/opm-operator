## 1. Entry construction

- [x] 1.1 Implement `NewSuccessEntry(action, phase string, digests DigestSet, inventoryCount int64) v1alpha1.HistoryEntry`
- [x] 1.2 Implement `NewFailureEntry(action, message string, digests DigestSet) v1alpha1.HistoryEntry`

## 2. Bounded append

- [x] 2.1 Implement `RecordHistory(status *v1alpha1.ModuleReleaseStatus, entry v1alpha1.HistoryEntry)` with prepend and trim to 10
- [x] 2.2 Implement sequence number auto-increment logic

## 3. Tests

- [x] 3.1 Write unit tests: append to empty, trim at boundary (10→10), ordering (newest first)
- [x] 3.2 Write unit tests: sequence monotonicity, timestamp population
- [x] 3.3 Write unit tests: success and failure entry construction

## 4. Validation

- [x] 4.1 Run `make fmt vet lint test` and verify all checks pass
