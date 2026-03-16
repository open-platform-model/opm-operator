## 1. Prune implementation

- [ ] 1.1 Define `PruneResult` struct with Deleted and Skipped counts in `internal/apply/prune.go`
- [ ] 1.2 Implement `Prune(ctx, client, stale []v1alpha1.InventoryEntry) (*PruneResult, error)` with fail-slow error collection
- [ ] 1.3 Implement safety exclusions for Namespace and CustomResourceDefinition kinds

## 2. Tests

- [ ] 2.1 Write envtest-based test: prune removes stale ConfigMap from cluster
- [ ] 2.2 Write envtest-based test: Namespace in stale set is skipped
- [ ] 2.3 Write envtest-based test: already-deleted resource does not error
- [ ] 2.4 Write envtest-based test: empty stale set is no-op

## 3. Validation

- [ ] 3.1 Run `make fmt vet lint test` and verify all checks pass
