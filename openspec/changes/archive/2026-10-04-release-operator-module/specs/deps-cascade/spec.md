## MODIFIED Requirements

### Requirement: deps:cascade never touches release, workflow or shared files
The task SHALL NOT modify:
- `.cascade-frozen` or `.cascade-hold`;
- `.release-please-manifest.json`, `release-please-config.json` or `CHANGELOG.md`;
- anything under `.github/`;
- `.opm-docs-version`, `docs-kit.cue` or any docs-kit ref;
- `hack/fixtures.sh` or `.tasks/examples.yaml`;
- any CUE `language.version`;
- the Jellyfin sample or `ocirepository.yaml`;
- anything under the operator module's directory `modules/opm_operator/`, whose pins `deps:cascade:module` moves in a PR of its own.

It SHALL only read from registries and SHALL never publish or seed. When an upstream's `language.version` is newer than the `CUE_VERSION` in `.github/workflows/test.yml`, it SHALL warn. When the example output in `docs/site/start/install-the-operator.md` names a catalog other than the sample Platform's, or a core other than `test/fixtures/modules/hello`'s, it SHALL warn and SHALL NOT edit that page.

#### Scenario: Release files untouched
- **WHEN** the task moves every pin
- **THEN** `git diff --name-only` names no path under `.github/`, no release-please file and no `.cascade-*` file

#### Scenario: The operator module is left to its own task
- **WHEN** the newest published catalog is newer than the one the operator module pins
- **THEN** `task deps:cascade` leaves every file under `modules/opm_operator/` unchanged

#### Scenario: Install page example output drifted
- **WHEN** `docs/site/start/install-the-operator.md` prints a catalog or core version the tree no longer pins
- **THEN** the task writes a warning naming the page and both versions, and leaves the page unchanged

#### Scenario: Newer CUE language warned
- **WHEN** the target catalog's `language.version` is newer than `.github/workflows/test.yml`'s `CUE_VERSION`
- **THEN** the task still moves the pin and writes a warning naming both versions

## ADDED Requirements

### Requirement: deps:cascade:module moves only the operator module's catalog and core
`task deps:cascade:module` SHALL move the operator module's pins in `modules/opm_operator/cue.mod/module.cue` and nothing else, with the rules `deps:cascade` applies to a fixture module: the opm catalog `opmodel.dev/catalogs/opm@v4` to the newest published catalog `K` when it is below `K`, and core `opmodel.dev/core@v2` to the core the module's catalog pins after the move when that is greater. The module's pins are shipped pins: every user who installs the module receives them. Holds and frozen paths SHALL apply under the same pin keys, `opmodel.dev/catalogs/opm@v4` and `opmodel.dev/core@v2`, so a hold on either holds the module too, and a `.cascade-frozen` entry for `modules/opm_operator` freezes only the module. It SHALL report by the same exit codes as `deps:cascade` (0 changed, 3 nothing to do). It SHALL NOT edit any file outside `modules/opm_operator/`, nor the module's `operator/operator.cue`, its `identity.Version`, its generated CRD and RBAC files, its `CHANGELOG.md` or its `RELEASE`: the image moves only through the image-bump PR, the version only through the identity advance, the generated files only through the generator, and the release files only through release-please. The offline test set SHALL cover it.

#### Scenario: Module pins move, image and version stay
- **WHEN** the newest published catalog is newer than the operator module's
- **THEN** the module's `cue.mod/module.cue` names the new catalog and the core it pins, `git diff --name-only` names only that file and the module's `cue.mod` files `tidy` writes, and the module's image reference, `identity.Version`, generated files, `CHANGELOG.md` and `RELEASE` are unchanged

#### Scenario: Catalog hold holds the module
- **WHEN** an in-date hold on `opmodel.dev/catalogs/opm@v4` caps the catalog below the newest
- **THEN** the module's catalog moves no further than the hold

#### Scenario: Nothing to move
- **WHEN** the module already pins the newest catalog and the core it pins
- **THEN** the task exits 3 and the tree is unchanged

### Requirement: The operator module's pin move opens its own pull request
`.github/workflows/module-deps.yml` SHALL run `task -x deps:cascade:module` on `repository_dispatch` of type `upstream-released` and on `workflow_dispatch`. When the task changed the tree, it SHALL push the branch `module/deps` with the release App's token and open or update one pull request from it, so the PR's CI runs; while the branch carries only bot commits a later run SHALL rebuild it from `main`, and once a human commit is on it a run SHALL add its own commit and never rewrite the branch. Its title and body SHALL come from the resolver's `title` and `body` with `.tasks/cascade/module-classes`, which classifies every path under `modules/opm_operator/` as shipped, and `.tasks/cascade/module-pins.sh`, which prints two rows keyed `opmodel.dev/catalogs/opm@v4` and `opmodel.dev/core@v2`, displayed as the operator module's opm catalog and core. Because that report is its own, each key appears once. The PR SHALL change no file outside `modules/opm_operator/`, so its squash commit proposes a module release and no operator release. While the repository variable `CASCADE_DRY_RUN` is not exactly `false`, the run SHALL write the diff to the job summary and push nothing.

#### Scenario: Catalog published
- **WHEN** this repository receives `upstream-released` for catalog `opm-v4.6.0`, `CASCADE_DRY_RUN` is `false`, and the module pins `v4.5.2`
- **THEN** a PR from `module/deps` moves the module to `v4.6.0` and the core it pins, titled `fix(deps): bump the operator module's opm catalog to v4.6.0 and core to <v>`, and merging it proposes a module release and no operator release

#### Scenario: Dry run
- **WHEN** `CASCADE_DRY_RUN` is unset
- **THEN** the run writes the diff to the job summary and `module/deps` is not created or moved

#### Scenario: Pin report keys are unique
- **WHEN** the resolver reads `.tasks/cascade/module-pins.sh WORKTREE`
- **THEN** it accepts the report, and `pins.sh WORKTREE` for the repository's cascade lists no row for the module
