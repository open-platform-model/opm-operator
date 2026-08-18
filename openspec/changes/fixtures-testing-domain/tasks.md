# Tasks — fixtures-testing-domain

Per `openspec/config.yaml`, delivery operations are not tasks — **except where publishing is itself
the deliverable**, which it is here. The publish steps below are in scope by that clause.

## 1. Fixtures become publishable artifacts

- [x] 1.1 Add `identity/identity.cue` to each of `test/fixtures/modules/{hello,hello_web,podinfo,redis}`
      declaring `testing.opmodel.dev/modules/operator/<name>@v0` and the module's current version.
- [x] 1.2 Point each `cue.mod/module.cue` `module:` at the same coordinate.
- [x] 1.3 Derive `metadata.{name,modulePath,version}` from the identity package in `module.cue`.
- [x] 1.4 `cue vet` clean and `cue eval ./identity -e Version` correct for all four.
- [x] 1.5 `opm module publish --dry-run` resolves GO with no refusals for all four.

## 2. Modulepackages follow (D2 — deviation from 0011 D17 item 5)

- [x] 2.1 Point each `modulepackages/*/cue.mod` `module:` at `testing.opmodel.dev/releases/operator/<n>@v0`.
- [x] 2.2 Re-point their `deps:` pin and `instance.cue` import at the new module coordinate.
- [x] 2.3 Re-point each `ocirepository.yaml` `url` and `.tasks/release.yaml`'s literal OCI repo paths.
- [x] 2.4 `cue mod tidy` + `cue vet` clean for all four.

## 3. Publish tooling

- [x] 3.1 Retire the grep version contract in `.tasks/examples.yaml`; read from the identity package
      and make a missing identity package or unreadable version a hard error, not a skip.
- [x] 3.2 Publish via `opm module publish`; keep the caller-side already-published tolerance.
- [x] 3.3 Replace the prerelease `sed` with a staged copy plus `opm module version set`.
- [x] 3.4 Same version source in `examples:pin` and `examples:version`.
- [x] 3.5 `.tasks/module.yaml` and `Makefile`: publish via `opm`, drop the stale hardcoded `v0.0.2` /
      `v0.0.1` versions, map both domains.

## 4. CI and mappings

- [x] 4.1 Add the testing domain to `release.yml`, `test-e2e.yml`, `.tasks/module.yaml`; export
      `OPM_REGISTRY` alongside `CUE_REGISTRY`.
- [x] 4.2 Install the pinned `opm` in both workflows (release.yml also needed a `setup-go` step).

## 5. Test guard

- [x] 5.1 Rewrite `skipIfNoTestRegistry` to test for a configured mapping rather than a localhost one;
      scope the container-tool check to the localhost case; keep `OPM_TEST_REGISTRY_FORCE`.
- [x] 5.2 Re-point the fixture coordinates in the reconcile integration tests and `config/samples`.
- [x] 5.3 Leave `internal/status/digests_test.go`'s golden pin untouched (frozen cross-repo vector).

## 6. Docs and records

- [x] 6.1 Rewrite `test/fixtures/modules/README.md`.
- [x] 6.2 Close the deviation in `opm-operator/CLAUDE.md`; add fixture-authoring guidance.
- [x] 6.3 Close the workspace deviation in the root `CLAUDE.md` (not a git repo — disk only).
- [x] 6.4 ADR-017 recording the production-vs-fixture domain split, amending ADR-008.
- [x] 6.5 `enhancements/0011`: add `opm-operator` to `affects`, add the operator `registry-cleanup`
      slice, append the history event including the D2 deviation.

## 7. Verification

- [x] 7.1 `task dev:fmt dev:vet`, `go build ./...`, unit tests.
- [x] 7.2 Publish all four to a local registry under the new coordinates; re-run is a no-op.
- [x] 7.3 Prerelease publish: declared version equals the tag on every fixture; working tree unchanged.
- [x] 7.4 Reconcile integration tier runs with zero registry-mapping skips.
- [ ] 7.5 Merge → release workflow publishes to GHCR; confirm the manifests resolve.
- [ ] 7.6 `make test-e2e` against a kind cluster (needs the fixtures on GHCR or a local publish).

## 8. Cleanup — after everything above is green

- [ ] 8.1 Delete the five `opmodel.dev/modules/test/*` GHCR packages (~377 tags, incl. the orphaned
      `hello-web`) and the two pre-2026 `testing.opmodel.dev/{modules,releases}/hello` leftovers.
      Org-admin, per-package, **irreversible** — never before 7.5 is confirmed.
