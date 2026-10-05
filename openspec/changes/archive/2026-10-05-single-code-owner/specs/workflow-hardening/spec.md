## MODIFIED Requirements

### Requirement: Code owners on the release machinery
`.github/CODEOWNERS` SHALL name the owner (`@emil-jacero`) as the only code owner for `/.github/`, `/.tasks/`, `/Taskfile*.yml`, `/release-please-config.json`, `/.release-please-manifest.json`, `/.cascade-frozen`, `/hack/` (scripts that run in jobs holding write tokens or the release App key, `hack/operator-module/` included), `/.opm-cli-version` (the opm binary the publishing jobs install), and `/.opm-docs-version` and `/docs-kit.cue` (the signed docs publish).

#### Scenario: Workflow edit needs a code owner
- **WHEN** a pull request edits a file under `.github/workflows/`
- **THEN** GitHub requests review from the code owners listed for `/.github/`

#### Scenario: One code owner per path
- **WHEN** `.github/CODEOWNERS` is read
- **THEN** every path line names `@emil-jacero` and no other owner
