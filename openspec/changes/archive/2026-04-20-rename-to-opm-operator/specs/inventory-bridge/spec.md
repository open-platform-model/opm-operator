## MODIFIED Requirements

### Requirement: CLI packages copied to `pkg/`
The controller MUST contain locally copied CLI packages under `pkg/` with all internal import paths rewritten from `github.com/opmodel/cli/pkg/` to `github.com/open-platform-model/opm-operator/pkg/`. The following packages MUST be present: `core`, `errors`, `validate`, `provider`, `module`, `loader`, `render`, `resourceorder`. (`bundle` was excluded — not yet implemented in OPM.)

#### Scenario: Copied packages compile under the renamed module
- **WHEN** `go build ./pkg/...` is run from the module root
- **THEN** all packages compile without errors

#### Scenario: No stale reference to the old module path
- **WHEN** `go.mod` is inspected and all Go files under `pkg/` are searched
- **THEN** there is no `require` entry for `github.com/opmodel/cli` and no import path beginning with `github.com/open-platform-model/poc-controller/`
