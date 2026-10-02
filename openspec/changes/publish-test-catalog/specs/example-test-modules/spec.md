## ADDED Requirements

### Requirement: A test catalog is published beside the module fleet

The repo SHALL carry a catalog fixture under `test/fixtures/catalogs/provider`, declaring the CUE module path `testing.opmodel.dev/catalogs/operator/provider@v0`. It SHALL be the smallest valid core `#Catalog`: one resource and one transformer that requires it, each with an FQN under the catalog's own registry path, so a platform can subscribe it beside `opmodel.dev/catalogs/opm@v4` without a key collision. It SHALL carry an `identity/identity.cue` package as the single source of its module path and version, with `Version` a plain SemVer literal.

The fixture SHALL be published through `hack/fixtures.sh`, the same flow as the module fleet, with `opm catalog publish` in place of `opm module publish`: `check` runs every catalog publish gate as a dry run and enforces changed-implies-bumped, `seed` publishes it into the job-local registry before the registry-backed specs run in `test.yml`, and `publish` puts it on GHCR when `publish-fixtures.yml` runs after a merge that touches `test/fixtures/catalogs/`. Registry-backed specs that need a resolvable catalog other than `opmodel.dev/catalogs/opm@v4` SHALL use this fixture and SHALL read its coordinate from its identity package (`fixtures.MustCatalog`), never a literal.

#### Scenario: The catalog passes the catalog publish gates

- **WHEN** `opm catalog publish --dry-run test/fixtures/catalogs/provider` runs
- **THEN** the plan resolves with no refusals, checking two members through the member FQN gate, and derives repository `testing.opmodel.dev/catalogs/operator/provider` and the tag from the identity package

#### Scenario: PR CI seeds the catalog from the tree

- **WHEN** `test.yml` runs on a pull request
- **THEN** the seed step publishes `testing.opmodel.dev/catalogs/operator/provider` at its declared version into the job-local registry before any registry-backed spec runs

#### Scenario: A changed catalog must carry a new version

- **WHEN** a pull request changes `test/fixtures/catalogs/provider` but keeps a version GHCR already holds
- **THEN** `task examples:check` fails, naming the fixture and `opm catalog version set` as the fix

#### Scenario: The claim specs build against the test catalog

- **WHEN** the claim-driven regeneration specs activate a claim for a catalog the Platform does not subscribe
- **THEN** that catalog is `testing.opmodel.dev/catalogs/operator/provider@v0` at the version its identity package declares, and the regenerated platform pins, imports and builds it
