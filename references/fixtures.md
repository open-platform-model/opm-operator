# Test fixtures and the registry they resolve from

Covers where the test fixtures live, how they are published and bumped, which registry each test flow uses, and how the operator binary picks its registry. Test tiers and commands are not covered here.

## Contents

- Which registry a test uses
- Fixture layout and identity
- Publishing and bumping a fixture
- Catalog fixtures
- Shared scripts
- Registry on the operator binary
- Pitfalls
- Check before you finish

## Which registry a test uses

Workspace registry policy applies: reads of `opmodel.dev/*` resolve from GHCR. Two repo-specific points:

- **No local registry is required by default.** The fixtures live at `testing.opmodel.dev/modules/operator/*`, and their modulepackages at `testing.opmodel.dev/releases/operator/*`. They are published to GHCR on merge (`publish-fixtures.yml`), on release and by the e2e workflow. So `task dev:test`, the controller and every non-PR context resolve them from GHCR with nothing running locally.
- **PR CI is the one exception, by design.** The PR test workflow seeds a job-local registry from the tree (`task examples:seed`) and runs `task dev:test TEST_CUE_REGISTRY=<MIXED_CUE_REGISTRY>`. Reason: a fixture bump and all its consumers land in one PR. `task examples:check` enforces that a changed fixture carries an unpublished version.

| Flow | Task | Registry |
| --- | --- | --- |
| Default | `task dev:test` | GHCR |
| Reproduce PR CI | `task dev:test:seeded` against `task registry:start` (workspace root) | local, seeded from the tree |
| Fully local | `task dev:test:local`, the `make` local-run flows and the module and release tasks in `.tasks/` | all of `opmodel.dev` mapped to localhost; only for the kind-internal path |

## Fixture layout and identity

- `test/fixtures/modules/*` declare `testing.opmodel.dev/modules/operator/<name>@v0`.
- Each carries an `identity/` package as the single source of path and version. Edit the `identity/` package, never the `metadata` block, which derives from it.
- **Never give a fixture an `opmodel.dev/*` path.** CUE routes by longest prefix, so a fixture there drags core and the catalogs onto whatever registry serves it. The publish gates refuse a nested path under `opmodel.dev` outright.
- Tests read a fixture's version through the helpers in `test/fixtures/`, never a literal.

## Publishing and bumping a fixture

- Fixtures publish through `opm module publish` (the cli's gated pipeline, installed in CI from a pinned release), not raw `cue mod publish`.
- Bump a fixture with `opm module version set`, then `task examples:pin`. The pin task re-pins the instance manifest, the sibling modulepackage's `deps` and `config/samples`.
- The modulepackage's core and catalog pins follow the module. `task examples:consumers` checks that in CI.
- A changed fixture carries one new version per PR, with its consumers (`task examples:check`).

## Catalog fixtures

`test/fixtures/catalogs/*` hold catalog fixtures on `testing.opmodel.dev/catalogs/operator/<name>@v0`. They are published by the same script through `opm catalog publish`. Bump one with `opm catalog version set`; tests read it through `fixtures.MustCatalog`.

- `provider`: the second, resolvable catalog that the registry-backed platform specs subscribe to or claim beside `opmodel.dev/catalogs/opm@v4`.
- `backup`: the provider of opm's backup trait. The `backup_provider` module fixture claims it and `backup_consumer` demands it. It sits outside `task deps:cascade` because the claim names its build literally. That is a deliberate deviation from enhancement 0015 (every field of the claim is derived or stamped; the only authored fact is the module's catalog dependency version), which the fixture README explains.

## Shared scripts

The fixture script in `hack/` and the helper in `test/fixtures/` are byte-identical copies of the cli's. The workspace root `task fixtures:lint` checks that, so edit both.

## Registry on the operator binary

The manager in `cmd/` resolves in this order: `--registry` (default empty), then the `OPM_REGISTRY` env var, then the built-in `defaultRegistry`. The default is GHCR for `opmodel.dev` and `testing.opmodel.dev`, and `registry.cue.works` for the rest. The binary logs the winning source at startup.

Retarget the operator with `--registry` or `OPM_REGISTRY` on the Deployment. `CUE_REGISTRY` as pod env is still ignored: the binary overwrites it with the resolved value.

## Pitfalls

- A fixture under `opmodel.dev/*` pulls core and the catalogs onto the wrong registry.
- Editing the `metadata` block of a fixture instead of the `identity/` package: the next derive overwrites it.
- Bumping a fixture version without `task examples:pin` leaves consumers on the old version.
- Editing the fixture script or helper without the cli copy fails `task fixtures:lint`.
- Setting `CUE_REGISTRY` on the Deployment and expecting it to apply.

## Check before you finish

- [ ] `task examples:check` passes after a fixture change.
- [ ] `task examples:consumers` passes (modulepackage pins follow the module).
- [ ] No test holds a fixture version literal.
- [ ] A change to a shared fixture script is mirrored in the cli.
