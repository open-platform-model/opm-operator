## Purpose

Keeps the linter configuration check of the required `Lint` job independent of the network, and keeps the linter version, the committed schema and the workflows that use them in agreement.

## ADDED Requirements

### Requirement: Linter configuration check needs no network
The linter configuration check (`task dev:lint:config`) SHALL validate `.golangci.yml` with the linter's own `config verify` command against a JSON schema file committed in the repository, and SHALL NOT make a network request. The `Lint` job SHALL run this check on every run.

#### Scenario: Valid configuration without a network
- **WHEN** `task dev:lint:config` runs in a network namespace with no route, with the linter already installed
- **THEN** it exits 0 and reports the version, the schema file and that no network was used

#### Scenario: Invalid configuration
- **WHEN** `.golangci.yml` holds a key the schema does not allow
- **THEN** the check exits non-zero and its output names the key

#### Scenario: Linter reaches for the network
- **WHEN** the linter's `config verify` tries to open a connection
- **THEN** the connection goes to a closed local port and the check fails at once

### Requirement: One file names the linter version
`.golangci-lint-version` SHALL be the only file that the linter version is edited in for the install. `Taskfile.yml` and `Makefile` SHALL read the version from it. `.custom-gcl.yml` SHALL carry the same version, and the check SHALL refuse a tree where it differs.

#### Scenario: Version file drives the install
- **WHEN** `.golangci-lint-version` holds `vX.Y.Z` and `task dev:lint` installs the linter
- **THEN** it installs `vX.Y.Z` and builds the custom binary from `.custom-gcl.yml` at `vX.Y.Z`

#### Scenario: Plugin build file disagrees
- **WHEN** `.custom-gcl.yml` names another version than `.golangci-lint-version`
- **THEN** the check exits non-zero and names both values

#### Scenario: A second version literal appears
- **WHEN** `Taskfile.yml`, a file under `.tasks/`, `Makefile` or a workflow carries a literal linter version
- **THEN** the check exits non-zero and names the file and line

### Requirement: Version, schema, checksum and use agree
The check SHALL refuse, with one line per problem and a non-zero exit, a tree where any of these does not hold: the version file holds `vX.Y.Z`; the schema of the `vX.Y` line is committed and is the only schema; the schema matches the committed SHA-256 checksum; the installed linter is the version the file names; `lint.yml` runs the check through `task dev:lint:config`; no workflow, task file or `Makefile` runs `config verify` without the committed schema or uses the linter's GitHub action.

#### Scenario: Version moved without its schema
- **WHEN** `.golangci-lint-version` moves to another minor line and the schema does not
- **THEN** the check names the missing schema file and the stale one

#### Scenario: Schema edited
- **WHEN** the committed schema differs from its checksum
- **THEN** the check reports the mismatch

#### Scenario: Workflow stops running the check
- **WHEN** `lint.yml` has no step that runs `task dev:lint:config`
- **THEN** the check reports it

#### Scenario: A direct, downloading verify returns
- **WHEN** a workflow, a task file or `Makefile` runs `config verify` without `--schema`
- **THEN** the check names the file and line

#### Scenario: Installed linter is another version
- **WHEN** the linter binary reports another version than the file names
- **THEN** the check reports both versions

### Requirement: The check has a scenario test in the Lint job
A scenario test SHALL run the check on a copy of the tree (pass) and on one copy per defect (refusal with the expected message). It SHALL run offline, in the `Lint` job and through `task dev:lint:config:test`.

#### Scenario: Scenario test passes on the tree
- **WHEN** `task dev:lint:config:test` runs on an unmodified tree
- **THEN** every scenario reports `ok` and the task exits 0
