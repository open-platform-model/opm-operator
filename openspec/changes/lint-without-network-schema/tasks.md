## 1. Offline configuration check

- [ ] 1.1 Add `.golangci-lint-version` (`v2.8.0`) and commit the schema of the Go module `github.com/golangci/golangci-lint/v2@v2.8.0` (`jsonschema/golangci.jsonschema.json`) as `.github/golangci-lint/golangci.v2.8.jsonschema.json` with `SHA256SUMS`. Verify: `sha256sum --check SHA256SUMS` in that directory passes and the sum equals the module cache file's.
- [ ] 1.2 `Taskfile.yml` and `Makefile` read the version from the file. Verify: `task dev:lint` reuses `bin/golangci-lint-v2.8.0` without a reinstall, and `make -n golangci-lint` names no other version.
- [ ] 1.3 Add `.github/scripts/lint-config-check.sh` (design.md, all decisions). Point `dev:lint:config` and `Makefile`'s `lint-config` at it. Verify: `unshare -rn task dev:lint:config` exits 0 and prints `lint-config: ok`.
- [ ] 1.4 Add `.github/scripts/lint-config-check-test.sh` (pass case plus one scenario per refusal in the spec) and `task dev:lint:config:test`. Verify: the task exits 0 under `unshare -rn`.
- [ ] 1.5 `lint.yml`: add the scenario-test step after "Check linter configuration". Verify: `task cascade:wiring:check` passes and the step list holds both steps.
- [ ] 1.6 `task dev:fmt dev:vet dev:lint dev:test docs:bundle:check` green, then commit `ci(lint): check the linter configuration against a committed schema`

## 2. Contributor docs

- [ ] 2.1 `AGENTS.md`: update the `dev:lint:config` line, add `dev:lint:config:test`, and add "Moving the golangci-lint version" (patch move, minor move, where the schema comes from, the hidden flag). Verify: every command in the section runs as written.
- [ ] 2.2 `task dev:fmt dev:vet dev:lint dev:test docs:bundle:check` green, then commit `docs: say how to move the golangci-lint version`
