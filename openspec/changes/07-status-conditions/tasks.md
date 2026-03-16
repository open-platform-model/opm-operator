## 1. Condition and reason constants

- [ ] 1.1 Define condition type constants (`ReadyCondition`, `ReconcilingCondition`, `StalledCondition`, `SourceReadyCondition`) in `internal/status/conditions.go`
- [ ] 1.2 Define reason constants (`Suspended`, `SourceNotReady`, `SourceUnavailable`, `ArtifactFetchFailed`, `ArtifactInvalid`, `RenderFailed`, `ApplyFailed`, `PruneFailed`, `ReconciliationSucceeded`)

## 2. Flux condition interface compliance

- [ ] 2.1 Add `GetConditions()` and `SetConditions()` methods to `ModuleRelease` in `api/v1alpha1/` if not already present
- [ ] 2.2 Run `make manifests generate` after API changes

## 3. Condition helper functions

- [ ] 3.1 Implement `MarkReconciling(obj, reason, message)` using Flux condition helpers
- [ ] 3.2 Implement `MarkStalled(obj, reason, message)`
- [ ] 3.3 Implement `MarkReady(obj, message)` and `MarkNotReady(obj, reason, message)`
- [ ] 3.4 Implement `MarkSourceReady(obj, revision)` and `MarkSourceNotReady(obj, reason, message)`

## 4. Tests

- [ ] 4.1 Write unit tests for each condition helper verifying correct condition state transitions
- [ ] 4.2 Write test verifying `ModuleRelease` satisfies Flux `conditions.Getter` and `conditions.Setter` interfaces

## 5. Validation

- [ ] 5.1 Run `make fmt vet lint test` and verify all checks pass
