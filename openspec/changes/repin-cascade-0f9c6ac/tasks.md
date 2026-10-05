## 1. Move the pin

- [x] 1.1 Replace `6938f8e0247e019cb0c2db13fff5b7b558a6b67d` with `0f9c6ac2c9b752a79f4874f637ef9955bcf00c13` on all six `.github` references in `.github/workflows`, keeping ` # .github main`. Verify: `grep -rn -A1 'open-platform-model/.github' .github/workflows` shows only the new SHA.
- [x] 1.2 Confirm `.tasks/cascade/wiring-check.sh` is the file at the new SHA: `gh api -H 'Accept: application/vnd.github.raw' "repos/open-platform-model/.github/contents/.github/scripts/cascade/wiring-check.sh?ref=0f9c6ac2c9b752a79f4874f637ef9955bcf00c13" | cmp - .tasks/cascade/wiring-check.sh`.
- [x] 1.3 Confirm `.tasks/cascade/wiring-check.yaml` and `lint.yml` need no change against the README at the new SHA, and that `.tasks/cascade/{pins.sh,lib.sh,classes,cascade.sh}` on `origin/main` hash to `mirror_sources opm-operator` in `wiring/lib.sh` at the new SHA.
- [x] 1.4 Verify: `GH_TOKEN=$(gh auth token) bash .tasks/cascade/wiring-check.sh --pin-on-main` prints `cascade wiring: ok`; `task cascade:wiring:check`, `actionlint`, `task dev:lint` and `CASCADE_TEST_SET=offline task -x deps:cascade:test` pass. Commit `ci(deps): pin the cascade to .github 0f9c6ac`.

## 2. Verify and archive

- [ ] 2.1 `openspec validate repin-cascade-0f9c6ac --strict` passes and the verify skill reports no CRITICAL finding.
- [ ] 2.2 `openspec archive repin-cascade-0f9c6ac -y`; `openspec validate --all --strict` passes. Commit `chore(openspec): archive repin-cascade-0f9c6ac`.
