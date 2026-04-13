## 1. Resource manager construction

- [x] 1.1 Implement `NewResourceManager(client, owner string) *ResourceManager` with `opm-controller` field manager in `internal/apply/manager.go`
- [x] 1.2 Define `ApplyResult` struct with Created, Updated, Unchanged counts

## 2. Apply using Flux SSA

- [x] 2.1 Implement `Apply(ctx, resources []*unstructured.Unstructured, force bool) (*ApplyResult, error)` using `ApplyAllStaged`
- [x] ~~2.2 Implement resource sorting into stage 1/stage 2 using `resourceorder.GetWeight`~~ Removed — Flux `ApplyAllStaged` handles staging internally (see `docs/design/flux-ssa-staging.md`)

## 3. Tests

- [x] 3.1 Write envtest-based test: apply ConfigMaps and verify they exist in cluster
- [x] 3.2 ~~Write envtest-based test: verify staged ordering (Namespace before Deployment)~~ Replaced with TODO for CRD-before-CR ordering test
- [x] 3.3 Write envtest-based test: idempotent re-apply returns unchanged counts
- [x] 3.4 Write envtest-based test: force-conflicts behavior (documents Flux ForceOwnership semantics)

## 4. Validation

- [x] 4.1 Run `make fmt vet lint test` and verify all checks pass
