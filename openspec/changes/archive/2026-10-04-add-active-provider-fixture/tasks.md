# Tasks: add-active-provider-fixture

Registry-backed runs below seed a local registry and map only `testing.opmodel.dev` to it, as `test.yml` does (`MIXED_CUE_REGISTRY`), with a fresh `CUE_CACHE_DIR`: a warm cache serves a fixture without touching the registry and hides an unpublished dependency (design.md D1).

## 1. Fixtures (test/fixtures)

- [x] 1.1 Add `test/fixtures/catalogs/backup` (design.md D1): `cue.mod` for `testing.opmodel.dev/catalogs/operator/backup@v0` pinning core v2.0.0-beta.1 and `opmodel.dev/catalogs/opm@v4` v4.4.4, `identity/identity.cue` at 0.1.0, `catalog.cue` listing one transformer, and `transformers/backup_transformer.cue` requiring opm's backup trait and rendering one ConfigMap (`<resourceName>-backup`, data `schedule` and JSON `retention`). Verify: `cue vet ./...` and `opm catalog publish --dry-run` report one member, no refusals.
- [x] 1.2 Add `test/fixtures/modules/backup_provider`: identity 0.1.0, metadata derived from it, one component `registration` that is opm's `#TransformerRegistration` with `catalog: "testing.opmodel.dev/catalogs/operator/backup@v0"`, `version: "0.1.0"` and `provides: [tra.#BackupTrait.metadata.fqn]`, and a `moduleinstance.yaml` with a ServiceAccount, a ClusterRole on `transformerregistrations` and its ClusterRoleBinding. Verify: `opm module build` renders one `TransformerRegistration` named `default.backup-provider`.
- [x] 1.3 Add `test/fixtures/modules/backup_consumer` (design.md D2): identity 0.1.0, `#config` with `schedule` and `keepDaily` defaults, one component `data` with `res.#Volumes` (one `emptyDir` volume) and `tra.#Backup`, and a `moduleinstance.yaml` with a namespaced Role on configmaps. Verify: `opm module build` is refused naming the backup trait, and renders one ConfigMap with `--platform` pointing at a platform that also holds the backup catalog.
- [x] 1.4 Add the three fixtures to `test/fixtures/modules/README.md` and the catalog fixture paragraph of `AGENTS.md` ("Test fixtures live on the testing domain").
- [x] 1.5 Run `hack/fixtures.sh check` with a fresh `CUE_CACHE_DIR` (the three new fixtures GO, the others unchanged), `hack/fixtures.sh seed` into a local registry, then `task dev:fmt dev:vet dev:lint dev:test` green, then commit `test(fixtures): add a backup provider, its claim and a consumer`.
- [x] 1.6 Found after 1.5: `CASCADE_TEST_SET=all task -x deps:cascade:test` fails S2, whose golden list names every fixture module that advances on a cascade, and the two backup modules pin core and the opm catalog. Add their `identity.cue` and `moduleinstance.yaml` to the list, rerun (all scenarios pass, S5 skipped without the real resolver), then commit `test(fixtures): count the backup fixtures in the cascade golden list`.

## 2. Registry-backed spec (test/integration/reconcile)

- [x] 2.1 In `registry_helpers_test.go`, split `generatedPlatformStoreAt` so the entries come from the caller: `generatedPlatformStoreFor(k, registry, entries, skew)`, and add `generatedPlatformStoreWith(k, registry, extra...)` that subscribes the test catalog plus `extra`.
- [x] 2.2 Add `backup_fixture_test.go` with the three specs of design.md D3. Verify against the seeded registry with `OPM_TEST_REGISTRY_FORCE=1`: 3 of 3 pass.
- [x] 2.3 On a throwaway podman kind cluster, record design.md D4: the released operator refuses the claim `CatalogUnresolved`, an operator built with library v1.0.0-beta.4 accepts and activates it and `backup-consumer` reaches Ready. Delete the cluster.
- [x] 2.4 Run `task dev:fmt dev:vet dev:lint dev:test` green with the seeded mapping, then commit `test(reconcile): check the backup fixture set renders a claim that fits its catalog`.

## 3. Verify and archive

- [x] 3.1 Run the repo's verify skill for the change, fix what it raises, then `openspec archive add-active-provider-fixture --yes`, confirm `openspec validate example-test-modules --type spec --strict` passes, and commit `docs(openspec): archive add-active-provider-fixture`.

## 4. Review fixes

- [x] 4.1 Open the library bump as its own `fix(deps)` PR (opm-operator#213) and carry its commit here until it merges; record it in design.md D4 and the proposal.
- [x] 4.2 Resolve the backup catalog in the first spec with the claim's own `catalog` and `version`, as acceptance does.
- [x] 4.3 Say in the fixture README, `AGENTS.md`, the spec requirement and design.md D1 that the literal claim deviates from 0015:D11:R1 on purpose.
- [x] 4.4 Post the captured cluster YAML on opm-operator#212 and link it from design.md D4; cite 0015:D3:R2; name the cluster-wide grant in `backup_provider/moduleinstance.yaml`; say the change adds two manifests to the next release's examples bundle.
