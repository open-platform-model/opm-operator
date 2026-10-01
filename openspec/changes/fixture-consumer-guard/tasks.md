# Tasks: fixture-consumer-guard

One PR, title `test(fixtures): make modulepackages follow their module and check them`. Sections 1 and 2 land in it; section 3 (archive) is a follow-up PR after merge. The cli change `fixture-consumer-guard` carries the same `hack/fixtures.sh`; the two PRs merge back to back.

Environment for every Go/CUE command, exported on two lines:

```bash
export CUE_REGISTRY='opmodel.dev=ghcr.io/open-platform-model,testing.opmodel.dev=ghcr.io/open-platform-model,registry.cue.works'
export OPM_REGISTRY="$CUE_REGISTRY"
```

Design.md "Research & Decisions" records the scratch proof; no assumption is unverified, so there is no spike section.

## 1. Shared check and the manual pin path (hack, .tasks/examples.yaml)

- [x] 1.1 Apply design.md Appendix A to `hack/fixtures.sh` as is. Verify: `shellcheck hack/fixtures.sh` is clean; once the cli's section 1 is committed, `git -C <workspace>/cli show test/fixture-consumer-guard:hack/fixtures.sh | cmp - hack/fixtures.sh` reports no difference.
- [x] 1.2 Apply the `.tasks/examples.yaml` part of design.md Appendix B: the `consumers` task (design.md D2) and the follow loop plus `desc` change in `pin` (D1). Verify: `CUE_CACHE_DIR=$(mktemp -d) task examples:consumers` prints four `ok`; with `test/fixtures/modulepackages/podinfo/cue.mod/module.cue` set to core `v2.0.0-alpha.6` it prints the diff and a `FAIL` line and fails, and `FIX=1` then restores the file byte for byte (`git diff --exit-code`).
- [x] 1.3 Verify the manual path: set the podinfo modulepackage to core `v2.0.0-alpha.6` and catalogs/opm `v4.0.1`, run `opm module version set 0.1.900 test/fixtures/modules/podinfo` and `task examples:pin`: the modulepackage pins `v0.1.900`, `v2.0.0-beta.1` and `v4.4.4`, and the output carries two "follows the module" lines. Discard every file change from this task (`git checkout -- .`).
- [x] 1.4 `task dev:fmt dev:vet dev:lint dev:test` green and `openspec validate fixture-consumer-guard --strict` passes, then commit `test(fixtures): make modulepackage pins follow their module`.

## 2. Run the check in CI and the seeded test task (test.yml, .tasks/dev.yaml, AGENTS.md)

- [x] 2.1 `.github/workflows/test.yml`: a step `Fixture consumers follow their fixtures` after "Seed the job-local registry from the tree", `run: task examples:consumers CONSUMERS_CUE_REGISTRY="$MIXED_CUE_REGISTRY"` (design.md D3). Extend the header comment by one line. Verify: `go run github.com/rhysd/actionlint/cmd/actionlint@latest .github/workflows/test.yml` is clean.
- [x] 2.2 Apply the `.tasks/dev.yaml` part of design.md Appendix B (`dev:test:seeded` runs `:examples:consumers` after the seed). Verify: `task --dry --verbose dev:test:seeded` lists it between the seed and the test.
- [x] 2.3 `AGENTS.md`, the test-fixtures bullet under "Registry": "then `task examples:pin` (re-pins ...)" gains that the modulepackage's core and catalog pins follow the module, and that `task examples:consumers` checks it in `test.yml`. No other prose.
- [x] 2.4 Seeded proof (Registry Policy rule 3 opt-in, local only): `task registry:start` at the workspace root; `opm module version set 0.1.900 test/fixtures/modules/podinfo`; hand-edit the podinfo modulepackage to `v0.1.900` with core `v2.0.0-alpha.6` (the state the current `examples:pin` leaves after a core move); `CUE_CACHE_DIR=$(mktemp -d) task dev:test:seeded`. Expected: the consumers step fails with the alpha.6 diff before any Go test runs. Discard every file change (`git checkout -- .`); the `0.1.900` tag stays in the local registry only.
- [x] 2.5 `task dev:fmt dev:vet dev:lint dev:test` green and `openspec validate fixture-consumer-guard --strict` passes, then commit `ci(test): check modulepackage pins after seeding the fixtures`.

## 3. Archive (after merge)

- [ ] 3.1 Precondition: the PR merged and `test.yml` on it showed the new step green. `openspec archive fixture-consumer-guard --yes`. Then replace the placeholder `## Purpose` of `openspec/specs/example-test-modules/spec.md` ("TBD - created by archiving change add-example-test-modules", which fails strict validation on `main` today) with one sentence: the operator's example test module fleet and its modulepackage fixtures, and how they are versioned, pinned, checked and published. Verify: `openspec validate example-test-modules --type spec --strict --no-interactive` passes; the spec carries the ADDED requirement and every existing scenario heading unchanged; `openspec validate --all --strict --no-interactive` fails no spec that passed before (on `c3e4232` it reports 19 pre-existing failures, `example-test-modules` among them).
- [ ] 3.2 `openspec validate example-test-modules --type spec --strict --no-interactive` green, then commit `chore(openspec): archive fixture-consumer-guard`.
