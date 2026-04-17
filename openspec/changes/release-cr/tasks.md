## 1. API Types

- [x] 1.1 Add `Release` and `ReleaseList` types in `api/v1alpha1/release_types.go` with `ReleaseSpec` (sourceRef, path, interval, prune, suspend, dependsOn, serviceAccountName, rollout) and `ReleaseStatus` (mirroring ModuleRelease: conditions, digests, inventory, history, failureCounters, nextRetryAt, source)
- [x] 1.2 Run `task dev:manifests dev:generate` — CRD, DeepCopy, RBAC
- [x] 1.3 Add sample CR in `config/samples/`

## 2. Source Resolution (GitRepository + Bucket support)

- [x] 2.1 Extend `internal/source/resolve.go` to handle GitRepository and Bucket source kinds alongside OCIRepository. Add `ErrUnsupportedSourceKind` sentinel error
- [x] 2.2 Add unit tests for GitRepository and Bucket resolution in `internal/source/resolve_test.go`

## 3. Artifact Fetch (tar.gz support)

- [x] 3.1 Add `extractTarGz()` in `internal/source/extract.go` with path traversal protection
- [x] 3.2 Add option to `ArtifactFetcher.Fetch()` to select extraction format (zip vs tar.gz) and skip root CUE module validation
- [x] 3.3 Add unit tests for tar.gz extraction and format selection

## 4. Release Reconciler

- [x] 4.1 Create `internal/controller/release_controller.go` — controller scaffold with `SetupWithManager`, watches for Release + source objects, RBAC markers
- [x] 4.2 Create `internal/reconcile/release.go` — `ReconcileRelease()` function: phase 0 (load CR, finalizer, suspend), source resolution, artifact fetch, path navigation, CUE load, kind detection, dispatch to render pipeline, apply, prune, status commit
- [x] 4.3 Wire `ReleaseReconciler` into `cmd/main.go` — register scheme, create controller
- [x] 4.4 Implement `dependsOn` check — verify all referenced Releases are `Ready=True` before proceeding

## 5. Tests

- [x] 5.1 Add envtest integration tests for `ReleaseReconciler` in `internal/controller/release_controller_test.go` — happy path, source not ready, path not found, suspend/resume
- [x] 5.2 Add unit tests for `dependsOn` logic

## 6. Validation

- [x] 6.1 Run `task dev:fmt dev:vet dev:lint dev:test`
- [x] 6.2 Run `task operator:binary` to verify compilation
