# Tasks: publish-test-catalog

One PR, title `test(fixtures): publish a test catalog for the registry-backed specs`. The cli change
`fixtures-catalogs` carries the identical `hack/fixtures.sh` and `tests/fixtures/fixtures.go`; the two
PRs merge in the same sitting.

Registry-backed checks run against a throwaway `registry:2` (not the workspace `opm-registry`), with
`MIXED='testing.opmodel.dev=localhost:5055+insecure,opmodel.dev=ghcr.io/open-platform-model,registry.cue.works'`
and a fresh `CUE_CACHE_DIR`.

## 1. Test catalog and the shared flow (test/fixtures, hack, CI)

- [x] 1.1 Add `test/fixtures/catalogs/provider` per design.md D2. Verify: `cue vet -c ./...` and `cue mod tidy --check` in it pass; `opm catalog publish --dry-run` passes with cli `main` and with the `.opm-cli-version` release.
- [x] 1.2 Extend `hack/fixtures.sh` per design.md D3. Verify: `shellcheck hack/fixtures.sh` is clean; `hack/fixtures.sh pins` lists the catalog first; `check` against GHCR passes; `check` against a registry that already holds `v0.1.0` fails naming `opm catalog version set`; `seed` twice into the throwaway registry pushes then reports already present; a `PRERELEASE` seed pushes `v0.1.0-<id>` and leaves the tree unchanged.
- [x] 1.3 Add `CatalogDir`, `LoadCatalog`, `MustCatalog` to `test/fixtures/fixtures.go` and the catalog checks to `test/fixtures/fixtures_test.go` per design.md D3. Verify: `go test ./test/fixtures/` passes.
- [x] 1.4 `.github/workflows/publish-fixtures.yml` triggers on `test/fixtures/catalogs/**`; header comments in it and `test.yml`, the `.tasks/examples.yaml` descriptions, and the `AGENTS.md` test-fixtures bullet name the catalog root. Verify: actionlint is clean on both workflows.
- [x] 1.5 `task dev:fmt dev:vet dev:lint dev:test` green and `openspec validate publish-test-catalog --strict` passes, then commit `test(fixtures): publish a test catalog through the shared fixture flow`.

## 2. Move the specs off the retired k8s catalog (internal/controller, test/integration)

- [x] 2.1 `internal/controller/platform_claims_test.go`, `internal/controller/platform_controller_test.go` and `test/integration/reconcile/platform_claim_watch_test.go` read the second catalog through `fixtures.MustCatalog(GinkgoT(), "provider")` per design.md D4. Verify: `grep -rn 'catalogs/k8s' internal test` returns nothing.
- [x] 2.2 Seed the throwaway registry (`task examples:seed CUE_REGISTRY="$MIXED"`), then run the claim-watch spec (`-ginkgo.focus='Claim-driven regeneration'`) and the `internal/controller` Platform specs with `CUE_REGISTRY="$MIXED"`, `OPM_TEST_REGISTRY_FORCE=1`, `OPM_TEST_CATALOG_PATH=opmodel.dev/catalogs/opm@v4`: all pass, none skipped for the registry. Stop the registry afterwards.
- [x] 2.3 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `test(controller): build the claim and platform specs against the test catalog`.

## 3. Archive

- [x] 3.1 `openspec archive publish-test-catalog --yes` on this branch, so the archive rides the implementing PR. Verify: `openspec validate platform-module-generation --type spec --strict --no-interactive` and the same for `example-test-modules` pass. Commit `chore(openspec): archive publish-test-catalog`.
