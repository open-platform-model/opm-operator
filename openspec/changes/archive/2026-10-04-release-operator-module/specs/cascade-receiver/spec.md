## MODIFIED Requirements

### Requirement: The cascade references are pinned to one .github main SHA and checked in the Lint job
Every reference to the cascade code SHALL name one full 40-character SHA of a commit on `open-platform-model/.github` `main`, never a branch or a tag, followed by the comment `# .github main`: the `cascade-notify` step in `release.yml`, the `cascade-receive.yml` call and the `cascade-publish` step in `deps-cascade.yml`, the `cascade-gates.yml` call, the `ref:` of the `open-platform-model/.github` checkout in `cascade-task.yml`, and the `ref:` of the resolver checkout in `module-deps.yml` (the operator module's own cascade PR, see `deps-cascade`). All six SHALL carry the same SHA, which moves only through a `ci(deps): pin the cascade to .github <sha7>` pull request. `cascade-task.yml` SHALL fail, not skip, when the resolver is missing at the pinned commit. `task cascade:wiring:check` (`.tasks/cascade/wiring-check.sh`) SHALL run as the step "Verify the cascade wiring" of the required `Lint` job on every pull request and SHALL fail when: a key-holding job (`notify-downstream`, `publish`) differs from its contract shape in its keys, step keys, `with` keys, Environment, permissions, step count, action reference, key inputs or a `runs-on` other than `ubuntu-latest`; the `notify-downstream` job's `needs` differs from `[release-please, publish-release]`, or its `if` from `needs.release-please.outputs.release_created == 'true' && vars.CASCADE_NOTIFY != 'off'` (contract §4.6 prints `releases_created`, which a module-only release also sets), or its `tag` input or `timeout-minutes` (20) from contract §4.6; the `publish` job's `needs` differs from `cascade` or its `timeout-minutes` from 15; `release.yml`'s workflow-level `env` is not a map or has a key other than `REGISTRY`, `IMAGE_NAME` and `CUE_VERSION`; `deps-cascade.yml`'s top-level keys, concurrency group, `publish` condition or either `dry-run` value differ from the contract; any other workflow value reads the key or any other job declares `environment: cascade`; a call into `.github` has a `secrets` key; or the set of `.github` references, their SHA or their comment differs. It guards against mistakes; review and the `main` ruleset guard against a deliberate edit (Phase 3 wiring contract (version 3.1) §2.4, §10.1 items 2, 3 and 6; the supervisor's addendum).

#### Scenario: One reference moves alone
- **WHEN** a pull request changes only the `cascade-gates.yml` call to another SHA
- **THEN** the "Verify the cascade wiring" step fails with `one .github SHA` naming both SHAs, and the `Lint` check is red

#### Scenario: A branch ref
- **WHEN** a pull request names `cascade-notify@main`
- **THEN** the wiring check fails on the notify `uses` and on `one .github SHA`

#### Scenario: A startup-code variable in release.yml
- **WHEN** a pull request adds `BASH_ENV` or any key other than `REGISTRY`, `IMAGE_NAME` and `CUE_VERSION` to `release.yml`'s workflow-level `env`
- **THEN** the wiring check fails naming the key

#### Scenario: A self-hosted runner for a key job
- **WHEN** a pull request sets `runs-on: self-hosted` on `notify-downstream`
- **THEN** the wiring check fails naming `release.yml:notify-downstream runs-on`

#### Scenario: The resolver is missing at the pin
- **WHEN** `cascade-task.yml` runs and the pinned `.github` commit has no `cascade-resolve.sh`
- **THEN** the run fails with `no cascade resolver at the pinned .github commit` instead of skipping S5

#### Scenario: The wired tree passes
- **WHEN** the `Lint` job runs on a pull request that leaves the cascade wiring as the contract shapes it
- **THEN** the step prints `cascade wiring: ok, .github <SHA> (.github main)`

#### Scenario: The module's resolver checkout follows the pin
- **WHEN** a pull request moves the five other `.github` references to a new SHA and leaves `module-deps.yml`'s resolver checkout behind
- **THEN** the wiring check fails on `one .github SHA`
