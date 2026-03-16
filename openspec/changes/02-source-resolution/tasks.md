## 1. Typed errors and ArtifactRef

- [ ] 1.1 Define `ErrSourceNotFound` and `ErrSourceNotReady` sentinel errors in `internal/source/validate.go`
- [ ] 1.2 Expand `ArtifactRef` struct to carry URL, Revision, and Digest fields in `internal/source/artifact.go`

## 2. Source resolution

- [ ] 2.1 Implement `Resolve(ctx, client, sourceRef, namespace) (*ArtifactRef, error)` in `internal/source/resolve.go`
- [ ] 2.2 Write unit tests for Resolve: source found and ready, source not found, source not ready, source ready but no artifact

## 3. Controller watch setup

- [ ] 3.1 Add OCIRepository watch with `handler.EnqueueRequestsFromMapFunc` to `ModuleReleaseReconciler.SetupWithManager`
- [ ] 3.2 Implement the map function that finds ModuleReleases referencing a given OCIRepository

## 4. Validation

- [ ] 4.1 Run `make fmt vet lint test` and verify all checks pass
