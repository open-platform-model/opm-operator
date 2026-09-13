## MODIFIED Requirements

### Requirement: CLI packages copied to `pkg/`
The controller MUST contain the locally copied CLI package `core` under `pkg/`, with its internal import paths rewritten from `github.com/opmodel/cli/pkg/` to `github.com/open-platform-model/opm-operator/pkg/`. No other copied CLI package SHALL be present under `pkg/`: a copy that nothing in the controller imports MUST be deleted rather than kept in step with the CLI.

#### Scenario: Copied packages compile under the renamed module
- **WHEN** `go build ./pkg/...` is run from the module root
- **THEN** the package compiles without errors

#### Scenario: No stale reference to the old module path
- **WHEN** `go.mod` is inspected and all Go files under `pkg/` are searched
- **THEN** there is no `require` entry for `github.com/opmodel/cli` and no import path beginning with `github.com/open-platform-model/poc-controller/`

#### Scenario: Only imported copies are present
- **WHEN** the packages under `pkg/` are listed
- **THEN** every one of them is imported by at least one file outside `pkg/`
