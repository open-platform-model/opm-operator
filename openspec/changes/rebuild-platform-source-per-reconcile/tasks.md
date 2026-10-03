# Tasks: rebuild-platform-source-per-reconcile

There are two sections, following design.md. Section 1 moves the recovery spec's setup into shared helpers, with no change in what that spec tests. Section 2 lands the flaky front, the fix and the regression spec together, so the front has its only user in the same commit; its spike step runs the regression spec against the unchanged controller first and is not committed separately.

Every focused verify below runs with the registry and the force flag, so a registry-backed spec cannot pass by skipping:
`KUBEBUILDER_ASSETS=<envtest> CUE_REGISTRY=<GHCR_CUE_REGISTRY> OPM_TEST_REGISTRY_FORCE=1 OPM_TEST_CATALOG_PATH=opmodel.dev/catalogs/opm@v4 go test ./test/integration/reconcile -run TestReconcileIntegration -count=1 -ginkgo.focus=<focus>`, and the output must show the focused specs passed (none skipped by a registry gate).

## 1. Shared setup for the registry-backed platform specs (test/integration/reconcile)

- [ ] 1.1 Add `useEmptyCUECache` in a test file: it points the process `CUE_CACHE_DIR` at a new `os.MkdirTemp` directory, returns an explicit restore func and also registers it with `DeferCleanup`, and walks the directory back to writable before removing it at cleanup. Add `createClusterPlatform(catalogPath)`: delete any leftover `cluster` Platform, create one subscribing to the catalog at the test version, delete it at cleanup, and return it with its generation.
- [ ] 1.2 Rewire `platform_recovery_test.go` onto both helpers. It keeps calling restore before its live phase, so that phase still runs against the warm cache. Verify: `go vet ./test/integration/...` passes and the focused run with focus `Platform build recovery` passes.
- [ ] 1.3 `task dev:fmt dev:vet dev:lint` green, then commit `test(reconcile): share the empty CUE cache and cluster Platform setup`.

## 2. A module-file source per reconcile (internal/controller, test/integration/reconcile)

- [ ] 2.1 Add the `flakyFront` helper as a test file, as in design.md § The regression spec: an `httptest.Server` that answers `503` while down and, once switched, forwards through `httputil.ReverseProxy` (`Rewrite` + `pr.SetURL(upstream)`) to the host the live mapping resolves for the catalog. Its mapping is the live mapping plus `<catalogBasePath>=127.0.0.1:<port>/<upstream repository prefix>+insecure`; the helper asserts that the fronted repository equals the live one. It counts refused and forwarded requests.
- [ ] 2.2 Add the regression spec "recovers from a transient registry failure on the same reconciler", as in design.md § The regression spec. Spike step: run it against the unchanged controller and confirm phase 2 fails with phase 1's cached 503; record the observed message in design.md.
- [ ] 2.3 In `internal/controller/platform_controller.go`, make `modFiles()` return an injected `r.ModFiles` as is and otherwise build and return a new `platformmodule.NewRegistry` source on every call without storing it. Rewrite its doc comment and the `ModFiles` field's doc comment as in design.md § A source per reconcile. Verify: `rg -n 'r\.ModFiles\s*=' internal cmd` finds no assignment outside `_test.go` files, `go build ./... && go vet ./...` pass, and the focused run with focus `transient registry failure` passes.
- [ ] 2.4 In `platform_recovery_test.go`, delete `r.ModFiles = nil` and the sentence of its comment that explains it. Leave the `r.Kernel`/`r.Registry` swap as it is. Verify: the focused run with focus `Platform build recovery` passes.
- [ ] 2.5 Confirm that the injected-source path is unchanged: the `requiringSource` spec in `internal/controller/platform_controller_test.go` passes unmodified. Verify: `KUBEBUILDER_ASSETS=<envtest> CUE_REGISTRY=<GHCR_CUE_REGISTRY> OPM_TEST_REGISTRY_FORCE=1 OPM_TEST_CATALOG_PATH=opmodel.dev/catalogs/opm@v4 go test ./internal/controller -count=1 -ginkgo.focus="generation defect"` passes with the spec run, not skipped.
- [ ] 2.6 `task dev:fmt dev:vet dev:lint dev:test` green and `openspec validate rebuild-platform-source-per-reconcile --strict` passes, then commit `fix(controller): build the platform module-file source per reconcile`.
