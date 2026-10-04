## MODIFIED Requirements

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
- **No regenerated reference.** It SHALL NOT edit `docs/site/reference/operator/_index.md`: the docs bundle generates the resource reference from `config/samples` when it is built.

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
- **THEN** `docs/site/reference/operator/_index.md` is unchanged, and `task docs:bundle` on the resulting tree shows the moved version in the example of `reference/operator/platform.md`

#### Scenario: Tidy adds a dependency
- **WHEN** `go mod tidy` or `cue mod tidy` adds or removes a third-party dependency, or `go get` raises the `go` or `toolchain` directive
- **THEN** the task writes a warning naming the dependency or the directives, and a fixture module's new dep that its modulepackage lacks is warned about too

#### Scenario: Third-party pins untouched
- **WHEN** the task runs `cue mod get` in a fixture module
- **THEN** the command names no module outside `opmodel.dev/` and `testing.opmodel.dev/`

