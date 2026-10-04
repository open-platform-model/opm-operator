# deps-cascade Specification

## Purpose
The operator's `task deps:cascade`: how it moves the repo's upstream pins (library, the opm catalog and core with fixtures and consumers, and the opm CLI) to the newest published versions, which pins it leaves alone, how it reports by exit code, and how it is tested.

## Requirements

### Requirement: deps:cascade moves upstream pins and reports by exit code
The repo SHALL provide `task deps:cascade`, which moves opm-operator's upstream pins to the newest published versions in the working tree only. It SHALL NOT commit, push or create a branch. Run as `task -x deps:cascade`, it SHALL exit 0 when the working tree changed, 3 when there was nothing to do, and any other code on error, and it SHALL never turn a failure into 0 or 3. Every version decision SHALL come from the shared cascade resolver of `open-platform-model/.github` (workspace RELEASING.md, section "The cascade"). The resolver is found through the absolute path in `CASCADE_RESOLVER`, or otherwise at `../.github/.github/scripts/cascade/cascade-resolve.sh` beside the main checkout. When the resolver is missing, the task SHALL fail with a message naming both options.

#### Scenario: Up-to-date tree
- **WHEN** every pin already equals the newest published version the resolver reports
- **THEN** `task -x deps:cascade` exits 3 and `git status --porcelain` is empty

#### Scenario: A pin is behind
- **WHEN** the resolver reports a newer published library than `go.mod` pins
- **THEN** `task -x deps:cascade` exits 0 and `go.mod` and `go.sum` name the newer library

#### Scenario: Resolver not found
- **WHEN** `CASCADE_RESOLVER` is unset and no `.github` checkout sits beside the repo
- **THEN** the task fails before running and says to check out `open-platform-model/.github` or set `CASCADE_RESOLVER`

#### Scenario: A predicate's "no" is not "nothing to do"
- **WHEN** a resolver call other than `newest`, such as `pin-of` for the target catalog, exits 3
- **THEN** the task exits with a code other than 0 or 3 and `git status --porcelain` is empty

#### Scenario: Relative resolver path
- **WHEN** `CASCADE_RESOLVER` is set to a relative path
- **THEN** the task fails and says the path must be absolute

### Requirement: deps:cascade resolves everything before it edits
The task SHALL refuse to run (exit 1) on a working tree with any change or untracked file, unless `CASCADE_ALLOW_DIRTY=1`. In that case it SHALL judge its result by comparing a snapshot of the tree from before and after the run. It SHALL validate `.cascade-frozen` and `.cascade-hold` through the resolver first. It SHALL resolve every target before editing any file. It SHALL set its own `CUE_REGISTRY` and `OPM_REGISTRY` to `testing.opmodel.dev=ghcr.io/open-platform-model,opmodel.dev=ghcr.io/open-platform-model,registry.cue.works`, and SHALL never inherit the Taskfile's local `localhost:5000` default.

#### Scenario: Resolver error leaves the tree untouched
- **WHEN** the resolver fails on the first pin the task resolves
- **THEN** the task exits with a code other than 0 or 3 and `git status --porcelain` is empty

#### Scenario: Dirty tree refused
- **WHEN** the working tree holds an untracked file and `CASCADE_ALLOW_DIRTY` is unset
- **THEN** the task exits 1 and changes nothing

### Requirement: deps:cascade moves library, the catalog and core, and the opm CLI
The task SHALL move exactly these pins:
- **library.** It runs `go get github.com/open-platform-model/library@<exact version>` and then `go mod tidy`, only when library moved.
- **The opm catalog.** It moves `opmodel.dev/catalogs/opm@v4` to one target `K` in every file that pins it and is below `K`:
  - the sample Platform's `version:` (stored bare);
  - `CatalogVersion()` in `test/fixtures/catalog.go` (stored bare);
  - the four `test/fixtures/modules/*/cue.mod/module.cue` files.

  A file whose catalog is above `K` SHALL NOT be lowered.
- **Core.** It moves core in those modules and in `test/fixtures/catalogs/provider/cue.mod/module.cue` to the version that the file's catalog after the move pins (the higher of the file's catalog and `K`; `K` for the provider fixture), and only when that is greater than the file's own core. Core SHALL never come from the newest published core directly.
- **The opm CLI.** It writes `.opm-cli-version` last.
- **The resource reference.** When it changed any file under `config/samples/`, it regenerates the `hack/crdref` block of `docs/site/reference/operator-resources.md` before writing `.opm-cli-version`. If `hack/crdref` fails, it SHALL warn and still produce the rest of the diff.

`cue mod get` SHALL name only `opmodel.dev/*` and `testing.opmodel.dev/*` modules, each with an exact version, and SHALL run, followed by one `cue mod tidy`, only in a module where a pin moved. Third-party pins SHALL never be named. A third-party pin that `tidy` raises, adds or removes, a change to `go.mod`'s `go` or `toolchain` directive, and a dep a fixture module gains that its modulepackage lacks SHALL each be reported as a warning. The task SHALL never edit an import path or a `@vN` key; a new major SHALL appear only as the resolver's warning.

#### Scenario: Catalog and core move as a consistent set
- **WHEN** the newest published catalog is newer than the sample's, and that catalog pins a core newer than the fixtures'
- **THEN** the sample, `catalog.go` and the four fixture modules name the new catalog, and the four fixture modules and the provider catalog fixture name the core that catalog pins

#### Scenario: Core already ahead of the catalog's core
- **WHEN** a fixture pins a core newer than the core catalog `K` pins
- **THEN** that fixture's core is left as it is and a warning names both versions

#### Scenario: Newer core published but not pinned by the catalog
- **WHEN** core `v2.0.0-beta.2` is published and the newest catalog pins core `v2.0.0-beta.1`
- **THEN** the fixtures' core stays at `v2.0.0-beta.1`

#### Scenario: Sample Platform moved
- **WHEN** the task moves the catalog `version:` in the sample Platform
- **THEN** `go run ./hack/crdref -check` passes on the resulting tree

#### Scenario: Tidy adds a dependency
- **WHEN** `go mod tidy` or `cue mod tidy` adds or removes a third-party dependency, or `go get` raises the `go` or `toolchain` directive
- **THEN** the task writes a warning naming the dependency or the directives, and a fixture module's new dep that its modulepackage lacks is warned about too

#### Scenario: Third-party pins untouched
- **WHEN** the task runs `cue mod get` in a fixture module
- **THEN** the command names no module outside `opmodel.dev/` and `testing.opmodel.dev/`

### Requirement: deps:cascade honours frozen and held pins
Before editing a version literal in a file, or naming a key in `cue mod get` for a module, the task SHALL ask the resolver whether that exact file is frozen for that pin key. When it is, the task SHALL skip that file for that key. After `cue mod tidy`, every frozen key in that module SHALL be byte-unchanged; otherwise the task SHALL exit 1 and name the file and the key. A hold on `opmodel.dev/core@v2` whose `max` is below the core the target catalog pins SHALL keep both the catalog and core at their current versions, with a warning.

#### Scenario: Frozen fixture left alone
- **WHEN** `.cascade-frozen` freezes `test/fixtures/modules/hello/cue.mod/module.cue` for the catalog and core, and both are behind
- **THEN** that file is byte-unchanged, the other pins still move, and the task exits 0

#### Scenario: Frozen catalog keeps its core
- **WHEN** `.cascade-frozen` freezes a fixture module file for the catalog only, its catalog is behind the newest, and its core is what its own catalog pins
- **THEN** core is chosen from the file's current catalog, so that file is byte-unchanged

#### Scenario: MVS raises a frozen key
- **WHEN** `cue mod get` of an unfrozen key raises a frozen key in the same module
- **THEN** the task exits 1 and names the module file and the frozen key

#### Scenario: Core hold holds the catalog
- **WHEN** an in-date hold caps core below the core the newest catalog pins
- **THEN** neither the catalog nor core moves, and a warning names the catalog, the core it needs, and the hold

### Requirement: Fixture versions advance once per PR and consumers follow
For each fixture module under `test/fixtures/modules/` and for the provider catalog fixture, the task SHALL set the declared version in `identity/identity.cue` as follows. Let `B` be the declared version at the merge-base of `CASCADE_BASE` (default `origin/main`) and `HEAD`:
- `B` itself, when the fixture did not change against that merge-base;
- the next patch of `B`, when it did change and `B` is published;
- `B` itself, when it changed and `B` is not yet published.

The version SHALL be written with `opm module version set` or `opm catalog version set`, using an opm CLI the task installed from the unmodified `.opm-cli-version` under the git directory, and only when it differs. In the same run, each module's consumers SHALL follow it:
- the modulepackage `cue.mod/module.cue` (the fixture's `v:`, catalog and core, as a text edit with no `tidy`);
- its `moduleinstance.yaml`;
- `config/samples/opmodel.dev_v1alpha1_moduleinstance.yaml` for the module it instantiates.

#### Scenario: Second run does not advance again
- **WHEN** the task has advanced a fixture to the next patch, the result is committed, and the task runs again with the same `CASCADE_BASE`
- **THEN** it exits 3 and the fixture's declared version is unchanged

#### Scenario: Consumers name the advanced version
- **WHEN** the task advances `test/fixtures/modules/hello` from `0.0.12` to `0.0.13`
- **THEN** `test/fixtures/modulepackages/hello/cue.mod/module.cue` pins `v0.0.13`, and `test/fixtures/modules/hello/moduleinstance.yaml` and the sample ModuleInstance name `v0.0.13`

#### Scenario: Pending version not bumped twice
- **WHEN** a fixture changed against the merge-base and its merge-base version is not yet published
- **THEN** the declared version stays at the merge-base version

### Requirement: deps:cascade never touches release, workflow or shared files
The task SHALL NOT modify:
- `.cascade-frozen` or `.cascade-hold`;
- `.release-please-manifest.json`, `release-please-config.json` or `CHANGELOG.md`;
- anything under `.github/`;
- `.opm-docs-version`, `docs-kit.cue` or any docs-kit ref;
- `hack/fixtures.sh` or `.tasks/examples.yaml`;
- any CUE `language.version`;
- the Jellyfin sample or `ocirepository.yaml`.

It SHALL only read from registries and SHALL never publish or seed. When an upstream's `language.version` is newer than the `CUE_VERSION` in `.github/workflows/test.yml`, it SHALL warn. When the example output in `docs/site/start/install-the-operator.md` names a catalog other than the sample Platform's, or a core other than `test/fixtures/modules/hello`'s, it SHALL warn and SHALL NOT edit that page.

#### Scenario: Release files untouched
- **WHEN** the task moves every pin
- **THEN** `git diff --name-only` names no path under `.github/`, no release-please file and no `.cascade-*` file

#### Scenario: Install page example output drifted
- **WHEN** `docs/site/start/install-the-operator.md` prints a catalog or core version the tree no longer pins
- **THEN** the task writes a warning naming the page and both versions, and leaves the page unchanged

#### Scenario: Newer CUE language warned
- **WHEN** the target catalog's `language.version` is newer than `.github/workflows/test.yml`'s `CUE_VERSION`
- **THEN** the task still moves the pin and writes a warning naming both versions

### Requirement: Title and body come from the shared resolver
`task deps:cascade:title` and `task deps:cascade:body` SHALL call the resolver's `title` and `body` subcommands with `.tasks/cascade/classes` and `.tasks/cascade/pins.sh`.
- **`classes`** SHALL classify `.opm-cli-version` as release-tool and `config/samples/`, `test/`, any `testdata/` directory, `*_test.go` and the generated `docs/site/reference/operator-resources.md` as test. Any other path, including `go.mod` and `go.sum`, SHALL be shipped.
- **`pins.sh <ref>`** SHALL print one row per logical pin for the working tree (`WORKTREE`) or a git ref, as `<pin-key>`, `<display>`, `<class>`, `<v-prefixed version>`, `<labels>` separated by tabs. The rows are library (shipped), the opm catalog (test, from the sample Platform), core (test, from `test/fixtures/modules/hello`) and the opm CLI (release-tool).

#### Scenario: Library and catalog moved
- **WHEN** the diff against `origin/main` moves library to `v1.0.0-beta.3` and the catalog to `v4.5.1`
- **THEN** `task -x deps:cascade:title` prints `fix(deps): bump library to v1.0.0-beta.3 and opm catalog to v4.5.1`

#### Scenario: Only fixtures moved
- **WHEN** the diff changes only paths under `test/` and `config/samples/`
- **THEN** the title type is `test(fixtures)`

#### Scenario: Only samples and fixtures moved, reference regenerated
- **WHEN** the diff changes only paths under `test/` and `config/samples/` and the regenerated `docs/site/reference/operator-resources.md`
- **THEN** the title type is `test(fixtures)`

#### Scenario: Only the opm CLI moved
- **WHEN** the diff changes only `.opm-cli-version`
- **THEN** the title type is `ci(deps)`

#### Scenario: Pin report agrees between tree and HEAD
- **WHEN** the tree is clean
- **THEN** `pins.sh WORKTREE` and `pins.sh HEAD` print the same rows

### Requirement: deps:cascade is tested offline in required CI and online on demand
`task deps:cascade:test` SHALL run `deps:cascade` in throwaway copies of the tree against the contract's stub resolver, and SHALL report `PASS` or `FAIL` per scenario. It SHALL exit 0 only when every scenario passes. It SHALL fail when the stub's `sha256sum` differs from the contract's checksum. With `CASCADE_TEST_SET=offline` it SHALL need no GHCR or module proxy access beyond a warm Go module cache, and it SHALL run in the `Lint` job of `.github/workflows/lint.yml`, the check workspace RELEASING.md makes required. It SHALL NOT need the real resolver or a `.github` checkout, except for the title and body check. The full set SHALL run in a separate, non-required workflow on changes to the cascade files, by hand, and weekly. That set SHALL include older pins restored to the tree's versions with the expected version-advance diff, frozen pins, and the title and body checked against the real resolver.

#### Scenario: Offline set in PR CI
- **WHEN** a pull request runs the `Lint` job
- **THEN** the job runs `task -x deps:cascade:test` with `CASCADE_TEST_SET=offline`, and the job fails if a scenario fails

#### Scenario: Older pins produce the expected diff
- **WHEN** the full set lowers every pin to its `older.tsv` version and runs the task
- **THEN** the task exits 0, the tree differs from the original only in the fixture version-advance paths, and a second run exits 3

#### Scenario: Stub drift caught
- **WHEN** `.tasks/cascade/testdata/stub-resolve.sh` differs by one byte from the contract's text
- **THEN** `task -x deps:cascade:test` fails naming that file
