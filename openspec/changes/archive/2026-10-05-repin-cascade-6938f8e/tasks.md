Gates for the one section: `GH_TOKEN=$(gh auth token) bash .tasks/cascade/wiring-check.sh --pin-on-main`, `task cascade:wiring:check`, `actionlint`, `task dev:lint`, `CASCADE_TEST_SET=offline task -x deps:cascade:test`, `openspec validate repin-cascade-6938f8e --strict`. Every commit is `ci` or `docs` typed, so nothing releases.

## 1. Pin, copy, config and callers

- [x] 1.1 Move every `open-platform-model/.github` reference from `2376ffa` to `6938f8e0247e019cb0c2db13fff5b7b558a6b67d # .github main` (`grep -rn -A1 'open-platform-model/.github' .github/workflows`).
- [x] 1.2 Replace `.tasks/cascade/wiring-check.sh` with the file at that SHA; `cmp` against `gh api ... ?ref=<SHA>` shows no difference.
- [x] 1.3 Add `.tasks/cascade/wiring-check.yaml` (publish workflows re-derived from main, `extra-references` for `module-deps.yml`).
- [x] 1.4 `deps-cascade.yml` publish: gates-only `if:` clause and `gates-only` input.
- [x] 1.5 `lint.yml`: the README step shape with `--pin-on-main`, after only SHA-pinned actions.
- [x] 1.6 Doc fixes: `module-image.yml` header, `AGENTS.md` (module-image-pr note, wiring-check description), `CODEOWNERS` header, `Taskfile.yml` task description.
- [x] 1.7 Negative checks: an appended line in the copy fails with `differs from`; a `run:` step above the wiring step fails; `cache: true` in `test-e2e.yml` fails.
- [x] 1.8 Gates green; commit `ci(deps): pin the cascade to .github 6938f8e`.

