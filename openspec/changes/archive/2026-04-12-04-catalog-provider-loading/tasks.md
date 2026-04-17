## 1. Catalog loading package

- [x] 1.1 Create `internal/catalog/catalog.go` with `LoadProvider(catalogDir, providerName string) (*provider.Provider, error)`
- [x] 1.2 Implement CUE module loading from catalog directory via `cue/load.Instances()`
- [x] 1.3 Extract `#Registry` from the `providers` package value as `map[string]cue.Value`
- [x] 1.4 Call `pkg/loader.LoadProvider(providerName, registry)` to produce the provider

## 2. Controller startup wiring

- [x] 2.1 Add `--catalog-path` flag to `cmd/main.go` with default `/etc/opm/catalog/v1alpha1`
- [x] 2.2 Call `catalog.LoadProvider` during startup, before `mgr.Start()`
- [x] 2.3 Fatal exit if provider loading fails
- [x] 2.4 Inject the loaded provider into `ModuleReleaseReconciler` struct

## 3. Dockerfile

- [x] 3.1 Add `COPY` stage for the catalog into `/etc/opm/catalog/v1alpha1/`
- [x] 3.2 Ensure `cue.dev/x/k8s.io` is vendored in `cue.mod/pkg/` within the catalog

## 4. Tests

- [x] 4.1 Create test fixture: minimal catalog directory with a single-transformer provider
- [x] 4.2 Unit test: successful provider load from catalog directory
- [x] 4.3 Unit test: missing catalog directory returns error
- [x] 4.4 Unit test: missing provider name returns error with available names
- [x] 4.5 Unit test: invalid CUE module returns error

## 5. Validation

- [x] 5.1 Run `make fmt vet lint test` and verify all checks pass
