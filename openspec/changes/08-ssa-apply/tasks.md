## 1. Resource manager construction

- [ ] 1.1 Implement `NewResourceManager(client, owner string) *ResourceManager` with `opm-controller` field manager in `internal/apply/manager.go`
- [ ] 1.2 Define `ApplyResult` struct with Created, Updated, Unchanged counts

## 2. Staged apply

- [ ] 2.1 Implement resource sorting into stage 1 (CRDs, Namespaces) and stage 2 (everything else) using `resourceorder.GetWeight`
- [ ] 2.2 Implement `Apply(ctx, resources []*unstructured.Unstructured, force bool) (*ApplyResult, error)` using `ApplyAllStaged`

## 3. Tests

- [ ] 3.1 Write envtest-based test: apply ConfigMaps and verify they exist in cluster
- [ ] 3.2 Write envtest-based test: verify staged ordering (Namespace before Deployment)
- [ ] 3.3 Write envtest-based test: idempotent re-apply returns unchanged counts
- [ ] 3.4 Write envtest-based test: force-conflicts behavior

## 4. Validation

- [ ] 4.1 Run `make fmt vet lint test` and verify all checks pass
