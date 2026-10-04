## MODIFIED Requirements

### Requirement: deps:cascade moves library, the catalog and core, and the opm CLI
The task SHALL move exactly these pins:
- **library.** It runs `go get github.com/open-platform-model/library@<exact version>` and then `go mod tidy`, only when library moved.
- **The opm catalog.** It moves `opmodel.dev/catalogs/opm@v4` to one target `K` in every file that pins it and is below `K`:
  - the sample Platform's `version:` (stored bare);
  - `CatalogVersion()` in `test/fixtures/catalog.go` (stored bare);
  - the four `test/fixtures/modules/*/cue.mod/module.cue` files;
  - the operator module's `cue.mod/module.cue`, a shipped pin (0028:D7).

  A file whose catalog is above `K` SHALL NOT be lowered.
- **Core.** It moves core in those modules, the operator module included, and in `test/fixtures/catalogs/provider/cue.mod/module.cue` to the version that the file's catalog after the move pins (the higher of the file's catalog and `K`; `K` for the provider fixture), and only when that is greater than the file's own core. Core SHALL never come from the newest published core directly.
- **The opm CLI.** It writes `.opm-cli-version` last.
- **No regenerated reference.** It SHALL NOT edit `docs/site/reference/operator-resources.md`: the docs bundle generates the resource reference from `config/samples` when it is built.

The task SHALL NOT edit the operator module's image reference, its `identity.Version` or its generated CRD and RBAC files: the image moves only through the image-bump PR, and the version only through the identity advance (`operator-module-release`). `cue mod get` SHALL name only `opmodel.dev/*` and `testing.opmodel.dev/*` modules, each with an exact version, and SHALL run, followed by one `cue mod tidy`, only in a module where a pin moved. Third-party pins SHALL never be named. A third-party pin that `tidy` raises, adds or removes, a change to `go.mod`'s `go` or `toolchain` directive, and a dep a fixture module gains that its modulepackage lacks SHALL each be reported as a warning. The task SHALL never edit an import path or a `@vN` key; a new major SHALL appear only as the resolver's warning.

#### Scenario: Catalog and core move as a consistent set
- **WHEN** the newest published catalog is newer than the sample's, and that catalog pins a core newer than the fixtures'
- **THEN** the sample, `catalog.go`, the four fixture modules and the operator module name the new catalog, and the four fixture modules, the operator module and the provider catalog fixture name the core that catalog pins

#### Scenario: Core already ahead of the catalog's core
- **WHEN** a fixture pins a core newer than the core catalog `K` pins
- **THEN** that fixture's core is left as it is and a warning names both versions

#### Scenario: Newer core published but not pinned by the catalog
- **WHEN** core `v2.0.0-beta.2` is published and the newest catalog pins core `v2.0.0-beta.1`
- **THEN** the fixtures' core stays at `v2.0.0-beta.1`

#### Scenario: Sample Platform moved
- **WHEN** the task moves the catalog `version:` in the sample Platform
- **THEN** `docs/site/reference/operator-resources.md` is unchanged, and `task docs:bundle` on the resulting tree shows the moved version in the Platform entry's example

#### Scenario: Tidy adds a dependency
- **WHEN** `go mod tidy` or `cue mod tidy` adds or removes a third-party dependency, or `go get` raises the `go` or `toolchain` directive
- **THEN** the task writes a warning naming the dependency or the directives, and a fixture module's new dep that its modulepackage lacks is warned about too

#### Scenario: Third-party pins untouched
- **WHEN** the task runs `cue mod get` in a fixture module
- **THEN** the command names no module outside `opmodel.dev/` and `testing.opmodel.dev/`

#### Scenario: Module pins move, image and version stay
- **WHEN** the newest published catalog is newer than the operator module's
- **THEN** the module's `cue.mod/module.cue` names the new catalog and the core it pins, and the module's image reference, `identity.Version` and generated files are unchanged

### Requirement: Title and body come from the shared resolver and the contract's path classes
`task deps:cascade:title` and `task deps:cascade:body` SHALL call the resolver's `title` and `body` subcommands with `.tasks/cascade/classes` and `.tasks/cascade/pins.sh`.
- **`classes`** SHALL classify `.opm-cli-version` as release-tool and `config/samples/`, `test/`, any `testdata/` directory and `*_test.go` as test, the contract's opm-operator block and nothing more. Any other path, including `go.mod`, `go.sum` and the operator module's directory, SHALL be shipped.
- **`pins.sh <ref>`** SHALL print one row per logical pin for the working tree (`WORKTREE`) or a git ref, as `<pin-key>`, `<display>`, `<class>`, `<v-prefixed version>`, `<labels>` separated by tabs. The rows are library (shipped), the opm catalog (test, from the sample Platform), core (test, from `test/fixtures/modules/hello`), the operator module's opm catalog and core (shipped, from the module's `cue.mod/module.cue`, displayed as the module's catalog and core) and the opm CLI (release-tool).

#### Scenario: Library and catalog moved
- **WHEN** the diff against `origin/main` moves library to `v1.0.0-beta.3` and the catalog to `v4.5.1`
- **THEN** `task -x deps:cascade:title` prints `fix(deps): bump library to v1.0.0-beta.3 and opm catalog to v4.5.1`

#### Scenario: Only fixtures moved
- **WHEN** the diff changes only paths under `test/` and `config/samples/`
- **THEN** the title type is `test(fixtures)`

#### Scenario: Only the sample moved, no page regenerated
- **WHEN** the task moves only the sample Platform's catalog `version:`, so the diff changes only paths under `config/samples/` and leaves `docs/site/` untouched
- **THEN** the title type is `test(fixtures)`

#### Scenario: Only the opm CLI moved
- **WHEN** the diff changes only `.opm-cli-version`
- **THEN** the title type is `ci(deps)`

#### Scenario: Pin report agrees between tree and HEAD
- **WHEN** the tree is clean
- **THEN** `pins.sh WORKTREE` and `pins.sh HEAD` print the same rows

#### Scenario: Module pins make the PR releasable
- **WHEN** the diff moves the operator module's catalog and core together with the test fixtures' catalog and core
- **THEN** the title type is `fix(deps)`, and merging it proposes a module release
