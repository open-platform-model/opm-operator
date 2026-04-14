## 1. Event recorder wiring

- [x] 1.1 Add `record.EventRecorder` field to `ModuleReleaseReconciler` struct
- [x] 1.2 Create recorder via `mgr.GetEventRecorderFor("opm-controller")` in `cmd/main.go` or `SetupWithManager`
- [x] 1.3 Pass recorder to reconcile orchestrator

## 2. Event emission points

- [x] 2.1 Emit `Normal/Applied` event after successful Phase 5 with resource counts
- [x] 2.2 Emit `Warning/ApplyFailed` event on Phase 5 failure
- [x] 2.3 Emit `Normal/Pruned` event after successful Phase 6 with deleted count
- [x] 2.4 Emit `Warning/PruneFailed` event on Phase 6 failure
- [x] 2.5 Emit `Warning/SourceNotReady` event on Phase 1 soft-block
- [x] 2.6 Emit `Warning/ArtifactFetchFailed` event on Phase 2 failure
- [x] 2.7 Emit `Warning/RenderFailed` event on Phase 3 failure
- [x] 2.8 Emit `Normal/Suspended` and `Normal/Resumed` events on suspend transitions
- [x] 2.9 Emit `Normal/ReconciliationSucceeded` event on full reconcile success
- [x] 2.10 Emit `Normal/NoOp` event on Phase 4 no-op detection

## 3. Tests

- [x] 3.1 Write envtest test: verify `Applied` event emitted after successful reconcile
- [x] 3.2 Write envtest test: verify `Warning` event emitted on failure
- [x] 3.3 Write envtest test: verify event messages include resource counts
- [x] 3.4 Write envtest tests: Suspended, Resumed, NoOp, ArtifactFetchFailed events

## 4. Validation

- [x] 4.1 Run `make fmt vet lint test` and verify all checks pass
