## 1. Finalizer constant and helpers

- [x] 1.1 Define finalizer constant `FinalizerName = "releases.opmodel.dev/cleanup"` in `internal/reconcile/`
- [x] 1.2 Implement `addFinalizer` helper using `controllerutil.AddFinalizer` + client patch
- [x] 1.3 Implement `removeFinalizer` helper using `controllerutil.RemoveFinalizer` + client patch

## 2. Phase 0 finalizer registration

- [x] 2.1 In Phase 0 of the reconcile orchestrator, check if finalizer is present; if not, add it and return
- [x] 2.2 In Phase 0, check for non-zero `DeletionTimestamp`; if set, branch to deletion cleanup

## 3. Deletion cleanup path

- [x] 3.1 Implement deletion cleanup: if `spec.prune=true`, call `internal/apply.Prune` with full `status.inventory.entries` as stale set
- [x] 3.2 If `spec.prune=false`, skip resource deletion entirely
- [x] 3.3 On successful cleanup (or prune disabled), remove finalizer
- [x] 3.4 On partial failure, do NOT remove finalizer; return error to requeue
- [x] 3.5 Ensure suspend=true does not short-circuit the deletion path

## 4. Tests

- [x] 4.1 Write envtest test: first reconcile adds finalizer to ModuleRelease
- [x] 4.2 Write envtest test: deletion with prune=true deletes inventory resources and removes finalizer
- [x] 4.3 Write envtest test: deletion with prune=false removes finalizer without deleting resources
- [x] 4.4 Write envtest test: safety exclusions (Namespace, CRD) are skipped during deletion cleanup
- [x] 4.5 Write envtest test: suspend=true does not block deletion cleanup

## 5. Validation

- [x] 5.1 Run `make fmt vet lint test` and verify all checks pass
