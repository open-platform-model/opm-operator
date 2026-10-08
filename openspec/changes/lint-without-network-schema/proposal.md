## Why

The required `Lint` job runs `task dev:lint:config`, which runs `golangci-lint config verify`. That command downloads its JSON schema from `golangci-lint.run` on every run, so a slow or unreachable linter website fails a required check for a reason that has nothing to do with the pull request. The cli's `Lint` job failed twice on 2026-10-08 for exactly this reason and was fixed in cli#344; the operator has the same step (`lint.yml`, "Check linter configuration") and no fix.

The linter version is also named in three places that nothing compares: `Taskfile.yml`, `Makefile` and `.custom-gcl.yml`.

## What Changes

- The linter configuration check runs against a JSON schema committed in the repository, with a checksum beside it. It makes no network request. The check is kept; what the linter checks does not change.
- One file, `.golangci-lint-version`, names the linter version. `Taskfile.yml` and `Makefile` read it. `.custom-gcl.yml` keeps a literal (the linter's `custom` command reads that file and it cannot reference another), and the check refuses a literal that differs.
- A check script refuses a tree where the version file, the committed schema, its checksum, `.custom-gcl.yml`, the installed linter and the workflows' use of the check disagree. A scenario test covers each refusal and runs in the `Lint` job and locally.
- `AGENTS.md` says how to move the linter version.

No SemVer effect: no shipped artifact changes (`ci` and `docs` commits; after GA the same). No API type, controller or reconcile phase is affected. No new dependency, action, secret or permission.

## Capabilities

### New Capabilities
- `lint-config-check`: the offline linter configuration check, the single version file and the agreement the check enforces.

### Modified Capabilities

None.

## Impact

- `.github/workflows/lint.yml`: one added step (the scenario test). The existing step keeps its name and command.
- New: `.golangci-lint-version`, `.github/golangci-lint/` (schema and `SHA256SUMS`), `.github/scripts/lint-config-check.sh`, `.github/scripts/lint-config-check-test.sh`.
- `Taskfile.yml`, `.tasks/dev.yaml`, `Makefile`: read the version file; `dev:lint:config` and `lint-config` run the script; new `dev:lint:config:test`.
- `AGENTS.md`.
- Not touched: Go code, `.golangci.yml` rules, other jobs, other repos.
