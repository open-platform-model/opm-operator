## 1. Typed errors and ArtifactRef

- [x] 1.1 Define `ErrSourceNotFound` and `ErrSourceNotReady` sentinel errors in `internal/source/validate.go`
- [x] 1.2 Expand `ArtifactRef` struct to carry URL, Revision, and Digest fields in `internal/source/artifact.go`

## 2. Source resolution

- [x] 2.1 Implement `Resolve(ctx, client, sourceRef, namespace) (*ArtifactRef, error)` in `internal/source/resolve.go`
- [x] 2.2 Write unit tests for Resolve with fake client covering all scenarios:
  - source found and ready → assert exact field mapping (URL, Revision, Digest)
  - source not found → assert `errors.Is(err, ErrSourceNotFound)`
  - source not ready (Ready=False) → assert `errors.Is(err, ErrSourceNotReady)`
  - source not ready (Ready=Unknown) → assert `errors.Is(err, ErrSourceNotReady)`
  - source ready but nil artifact → assert `errors.Is(err, ErrSourceNotReady)`
  - namespace resolution: empty sourceRef.Namespace uses releaseNamespace, set sourceRef.Namespace overrides

## 3. Controller watch setup

- [x] 3.1 Vendor Flux OCIRepository CRD YAML into `internal/controller/testdata/crds/` and add to `CRDDirectoryPaths` in `suite_test.go`
- [x] 3.2 Register `sourcev1.AddToScheme` in the test suite scheme setup
- [x] 3.3 Add OCIRepository watch with `handler.EnqueueRequestsFromMapFunc` to `ModuleReleaseReconciler.SetupWithManager`
- [x] 3.4 Implement the map function that finds ModuleReleases referencing a given OCIRepository
- [x] 3.5 Write envtest integration test: OCIRepository status update triggers reconciliation of referencing ModuleRelease
- [x] 3.6 Write envtest integration test: map function only enqueues ModuleReleases that reference the changed OCIRepository

## 4. Validation

- [x] 4.1 Run `make fmt vet lint test` and verify all checks pass
