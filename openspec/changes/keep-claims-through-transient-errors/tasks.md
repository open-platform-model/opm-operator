## 1. Pin the library's classification at catalog acquisition

- [x] 1.1 Add a test that calls the real Kernel's `AcquireCatalogFromRegistry` with an empty `CUE_CACHE_DIR` against a closed port, a 503 registry and a 404 registry, and asserts `ErrTransient` for the first two and not for the third; verify with `go test ./internal/controller -run TestCatalogAcquisitionClassification`
- [x] 1.2 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `test(controller): pin the fetch classification of catalog acquisition`

## 2. Keep an accepted claim through a transient failure

- [x] 2.1 Add the specs of the delta spec (stub and real-Kernel, cold cache) and a unit test of the backoff function; verify that the specs for the kept verdict fail on the unchanged controller
- [x] 2.2 Add `holdVerdict` and `holdBackoff` and the transient branch in `Reconcile`; verify that every new spec passes and no existing spec changes
- [x] 2.3 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `fix(controller): keep an accepted claim through a transient registry failure`

## 3. Docs

- [ ] 3.1 Add the `Reconciling`: `CatalogUnresolved` row for TransformerRegistration to `docs/site/diagnostics/operator-conditions.md` and correct the `Ready`: `CatalogUnresolved` row; verify with `task docs:bundle:check`
- [ ] 3.2 `task dev:fmt dev:vet dev:lint dev:test docs:bundle:check` green, then commit `docs(controller): describe the held claim condition`
