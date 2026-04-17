## 1. API Types

- [ ] 1.1 Add `Release` and `ReleaseList` types in `api/v1alpha1/release_types.go` with `ReleaseSpec` (sourceRef, path, interval, prune, suspend, dependsOn, serviceAccountName, rollout) and `ReleaseStatus` (mirroring ModuleRelease: conditions, digests, inventory, history, failureCounters, nextRetryAt, source)
- [ ] 1.2 Run `make manifests generate` — CRD, DeepCopy, RBAC
- [ ] 1.3 Add sample CR in `config/samples/`

## 2. Source Resolution (GitRepository + Bucket support)

- [ ] 2.1 Extend `internal/source/resolve.go` to handle GitRepository and Bucket source kinds alongside OCIRepository. Add `ErrUnsupportedSourceKind` sentinel error
- [ ] 2.2 Add unit tests for GitRepository and Bucket resolution in `internal/source/resolve_test.go`

## 3. Artifact Fetch (tar.gz support)

- [ ] 3.1 Add `extractTarGz()` in `internal/source/extract.go` with path traversal protection
- [ ] 3.2 Add option to `ArtifactFetcher.Fetch()` to select extraction format (zip vs tar.gz) and skip root CUE module validation
- [ ] 3.3 Add unit tests for tar.gz extraction and format selection

## 4. Release Reconciler

- [ ] 4.1 Create `internal/controller/release_controller.go` — controller scaffold with `SetupWithManager`, watches for Release + source objects, RBAC markers
- [ ] 4.2 Create `internal/reconcile/release.go` — `ReconcileRelease()` function: phase 0 (load CR, finalizer, suspend), source resolution, artifact fetch, path navigation, CUE load, kind detection, dispatch to render pipeline, apply, prune, status commit
- [ ] 4.3 Wire `ReleaseReconciler` into `cmd/main.go` — register scheme, create controller
- [ ] 4.4 Implement `dependsOn` check — verify all referenced Releases are `Ready=True` before proceeding

## 5. Tests

- [ ] 5.1 Add envtest integration tests for `ReleaseReconciler` in `internal/controller/release_controller_test.go` — happy path, source not ready, path not found, suspend/resume
- [ ] 5.2 Add unit tests for `dependsOn` logic

## 6. Validation

- [ ] 6.1 Run `make fmt vet lint test`
- [ ] 6.2 Run `make build` to verify compilation
