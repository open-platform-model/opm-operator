## 1. Offline configuration check

- [x] 1.1 Add `.golangci-lint-version` (`v2.8.0`) and commit the schema of the Go module `github.com/golangci/golangci-lint/v2@v2.8.0` (`jsonschema/golangci.jsonschema.json`) as `.github/golangci-lint/golangci.v2.8.jsonschema.json` with `SHA256SUMS`. Verify: `sha256sum --check SHA256SUMS` in that directory passes and the sum equals the module cache file's.
- [x] 1.2 `Taskfile.yml` and `Makefile` read the version from the file. Verify: `task dev:lint:config` installs `v2.8.0` (`bin/golangci-lint-v2.8.0` exists), and `make -n golangci-lint` names `@v2.8.0` and no other version.
- [x] 1.3 Add `.github/scripts/lint-config-check.sh` (design.md, all decisions). Point `dev:lint:config` and `Makefile`'s `lint-config` at it. Verify: `task dev:lint:config` exits 0, and with the linter installed `unshare -rn env GOLANGCI_LINT=bin/golangci-lint bash .github/scripts/lint-config-check.sh` exits 0 and prints `lint-config: ok` (the task itself reinstalls the linter on every run, which needs the network; see design.md).
- [x] 1.4 Add `.github/scripts/lint-config-check-test.sh` (pass case plus one scenario per refusal in the spec) and `task dev:lint:config:test`. Verify: the script exits 0 under `unshare -rn` with `GOLANGCI_LINT=bin/golangci-lint`, and the task exits 0.
- [x] 1.5 `lint.yml`: add the scenario-test step after "Check linter configuration". Verify: `task cascade:wiring:check` passes and the step list holds both steps.
- [x] 1.6 `task dev:fmt dev:vet dev:lint dev:test docs:bundle:check` green, then commit `ci(lint): check the linter configuration against a committed schema`

## 2. Contributor docs

- [x] 2.1 `AGENTS.md`: update the `dev:lint:config` line, add `dev:lint:config:test`, and add "Moving the golangci-lint version" (patch move, minor move, where the schema comes from, the hidden flag). Verify: every command in the section runs as written.
- [x] 2.2 `task dev:fmt dev:vet dev:lint dev:test docs:bundle:check` green, then commit `docs: say how to move the golangci-lint version`
