## Why

The raw Kubernetes catalog `opmodel.dev/catalogs/k8s@v1` is retired (owner decision 2026-10-02). Three
registry-backed specs still use its published build `1.0.0-alpha.2` as "a real, resolvable second
catalog the Platform does not subscribe":

- `internal/controller/platform_claims_test.go:42-45` (`claimCatalogPath`, `claimCatalogVersion`), the
  claim-to-platform specs;
- `test/integration/reconcile/platform_claim_watch_test.go:59-60`, the manager-driven claim watch spec,
  where the regenerated platform must pin, import and build the claim's catalog;
- `internal/controller/platform_controller_test.go:247,278,487,493`, a disabled second subscription
  that must still resolve.

The main spec `platform-module-generation` names the same build in its two-catalog scenario. A
retired catalog can disappear from GHCR or fall behind core, and these specs would then test the
retired catalog instead of the operator. No `testing.opmodel.dev/catalogs/*` exists on GHCR today;
the owner chose to publish a tiny test catalog fixture.

## What Changes

- **A catalog fixture.** `test/fixtures/catalogs/provider`, module path
  `testing.opmodel.dev/catalogs/operator/provider@v0`, version `0.1.0`: one `probe` resource and one
  `probe-transformer` that renders it as a ConfigMap, FQNs under the catalog's own path, core pinned
  at the library's verified core (`v2.0.0-beta.1`).
- **One fixture flow publishes it.** `hack/fixtures.sh` learns a catalog root (`CATALOGS_DIR`,
  default the `catalogs/` sibling of `FIXTURES_DIR`) and runs `opm catalog ...` for fixtures under it,
  `opm module ...` for the rest. `check`, `seed`, `publish` and `pins` cover catalogs; nothing else
  changes for a repo without a catalog root. `test/fixtures/fixtures.go` gains `LoadCatalog` /
  `MustCatalog`. Both files stay byte-identical with the cli, which takes the same edit in its own PR
  (cli change `fixtures-catalogs`).
- **CI.** `publish-fixtures.yml` also triggers on `test/fixtures/catalogs/**`. `test.yml` needs no
  edit: its seed step runs the script, which now seeds the catalog too.
- **Tests and spec.** The three specs read the catalog's coordinate through `fixtures.MustCatalog`;
  the `platform-module-generation` scenario names the test catalog.

Release class: none. Every commit is `test(...)` or `chore(openspec)`, hidden sections; after GA it
would still be no release (test tooling only). No API type, CRD, controller or reconcile phase
changes.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `example-test-modules`: a test catalog fixture is published beside the module fleet through the
  same flow.
- `platform-module-generation`: the two-catalog scenario names the test catalog instead of the
  retired k8s catalog.

## Impact

- **Files**: new `test/fixtures/catalogs/provider/**`; `hack/fixtures.sh`,
  `test/fixtures/fixtures.go`, `test/fixtures/fixtures_test.go` (shared with the cli);
  `.github/workflows/{publish-fixtures,test}.yml`, `.tasks/examples.yaml`, `AGENTS.md`; the three
  test files above.
- **Merge order**: the cli PR carrying the identical `hack/fixtures.sh` and `fixtures.go` merges in
  the same sitting, so the workspace `task fixtures:lint` stays green on both `main`s.
- **GHCR window**: the catalog reaches GHCR only when `publish-fixtures.yml` runs on the merge. PR CI
  and `test.yml` on `main` seed from the tree and never wait for it; a GHCR-backed `task dev:test`
  run between merge and that workflow finishing fails the three specs (they build against a catalog
  GHCR does not hold yet).
- **Not covered**: the workspace `task deps:pins:fixtures` bumps module fixtures only; moving the
  catalog fixture's core pin stays a hand edit plus `opm catalog version set` until that task learns
  the catalog root.
