## ADDED Requirements

### Requirement: Modulepackage pins follow their module

Each modulepackage fixture under `test/fixtures/modulepackages/<m>` SHALL pin every dependency it shares with its module (`test/fixtures/modules/<m>`) exactly as CUE resolves it for the module version it names: what `cue mod get <module>@<version>` followed by `cue mod tidy` writes. CUE keeps a dependency the modulepackage already lists at its listed version, so a version re-pin alone renders the new module against an older core without any error.

`task examples:pin` SHALL, after re-pinning a modulepackage to its module's version, set every other dependency the modulepackage shares with the module to the module's pin. `task examples:consumers` SHALL run `hack/fixtures.sh consumers` over every modulepackage, resolving through `CONSUMERS_CUE_REGISTRY` (GHCR by default), and the `test.yml` workflow SHALL run it after seeding the job-local registry, with the mixed mapping. It SHALL fail naming each modulepackage whose pins differ from what CUE resolves or whose module version cannot be resolved, after checking all of them.

#### Scenario: Manual bump moves core with the module

- **WHEN** a module's `cue.mod` pins a newer core than its modulepackage and `opm module version set` then `task examples:pin` run
- **THEN** the modulepackage pins the module's new version and the module's core and catalog versions

#### Scenario: Stale modulepackage fails the test workflow

- **WHEN** a pull request re-pins a modulepackage to a new module version but leaves its core pin lower than the module's
- **THEN** the `test.yml` consumers step fails, printing the core pin diff and a `FAIL` line naming that modulepackage

#### Scenario: Modulepackages that follow their module pass

- **WHEN** every modulepackage pins what CUE resolves for its module version
- **THEN** `task examples:consumers` prints `ok` for each and exits zero

#### Scenario: Unresolvable module version is a named failure

- **WHEN** a modulepackage pins a module version the configured registry does not hold
- **THEN** the step prints a `FAIL` line naming the modulepackage and the version, still checks the others, and exits non-zero
