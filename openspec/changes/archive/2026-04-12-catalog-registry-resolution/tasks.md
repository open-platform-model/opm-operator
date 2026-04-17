## 1. CUE composition module

- [x] 1.1 Create `catalog/cue.mod/module.cue` with module declaration and pinned dependency versions for all 6 catalog modules + 2 external deps
- [x] 1.2 Create `catalog/config.cue` with imports from all 5 provider modules and `providers.kubernetes` unification via `&`
- [x] 1.3 Verify composition evaluates correctly: `CUE_REGISTRY=opmodel.dev=ghcr.io/open-platform-model,registry.cue.works cue eval ./catalog/`

## 2. Rewrite `internal/catalog` for composition loading

- [x] 2.1 Rewrite `loadRegistry` in `catalog.go`: load package `.` (not `./providers`), extract `providers` (not `#Registry`)
- [x] 2.2 Remove the `cue.mod/module.cue` existence check (registry resolution handles validation)

## 3. Controller startup wiring

- [x] 3.1 Add `--registry` flag to `cmd/main.go` with default `opmodel.dev=ghcr.io/open-platform-model,registry.cue.works`
- [x] 3.2 Add `--cue-cache-dir` flag to `cmd/main.go` with default `/tmp/cue-cache`
- [x] 3.3 Set `CUE_REGISTRY`, `OPM_REGISTRY`, and `CUE_CACHE_DIR` env vars before `catalog.LoadProvider` call
- [x] 3.4 Update `--catalog-path` default from `/etc/opm/catalog/opm/v1alpha1` to `/catalog`

## 4. Dockerfile and build

- [x] 4.1 Replace full catalog COPY with `COPY catalog/ /catalog/`
- [x] 4.2 Add `!catalog/**` to `.dockerignore`

## 5. Tests

- [x] 5.1 Update test fixtures: replace `testdata/valid-catalog` with a composition-shaped fixture that has `providers:` (not `#Registry`)
- [x] 5.2 Update `TestLoadProvider_Success` and `TestLoadProvider_DefaultProviderName` for new extraction path
- [x] 5.3 Verify `TestLoadProvider_MissingDirectory`, `TestLoadProvider_ProviderNotFound`, `TestLoadProvider_EmptyRegistry` still pass
- [x] 5.4 Remove or update `TestLoadProvider_MissingCUEModule` (registry resolution changes this behavior)

## 6. Validation

- [x] 6.1 Run `make fmt vet lint test` and verify all checks pass
