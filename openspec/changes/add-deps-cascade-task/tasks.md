Depends on: `.github` `add-cascade-resolver` merged before this change's PR merges (proposal, "Depends on / gates"). Sections 1 to 4 run against the stub and do not wait for it; task 5.4 (S5 against the real resolver) does.

Every section runs `shellcheck` on the scripts it adds or changes, as well as the Go gates.

## 1. Spike: verify the assumptions and land the stub

- [x] 1.1 Copy the stub text from Phase 2 cascade contract §7 to `.tasks/cascade/testdata/stub-resolve.sh` (mode 0755), byte for byte. Confirm `sha256sum` prints `970130f7d55c07f5b86d4f5b6f392330427ff923eb34f93553656bcd4b893d9c`, and that `shellcheck` is clean.
- [x] 1.2 Add `.tasks/cascade/testdata/older.tsv` with the five rows in design.md ("`older.tsv`"). Re-check each against the live service:
  - `proxy.golang.org/.../library/@v/v1.0.0-alpha.36.info` answers 200;
  - the GHCR manifest `HEAD` answers 200 for `catalogs/opm v4.4.2` and `core v2.0.0-alpha.12`;
  - the cli `v1.0.0-beta.4` release assets `opm-linux-amd64.tar.gz` and `checksums.txt` answer 200;
  - the `v4.4.2` modulefile pins core `v2.0.0-alpha.12`.
- [x] 1.3 In a scratch Taskfile, check two go-task 3.52 behaviours, and record both in design.md ("File layout and task wiring"):
  - whether a top-level `x-cascade-resolver:` anchor is accepted;
  - that `task -x` propagates `exit 3` while a plain `task` gives 201.
- [x] 1.4 In a scratch copy of the tree, check that the S2 library restore is byte-identical:
  - run `go mod edit -require=github.com/open-platform-model/library@v1.0.0-alpha.36`;
  - then run `go get github.com/open-platform-model/library@v1.0.0-beta.1 && go mod tidy`;
  - check that this reproduces `go.mod` and `go.sum` byte for byte.

  Record the result in design.md ("S2 setup for the Go pin").
- [x] 1.5 In a scratch copy, install the opm CLI from `.opm-cli-version` into a throwaway `GOBIN`. Run `opm module version set 0.0.13 test/fixtures/modules/hello` and `opm catalog version set 0.1.1 test/fixtures/catalogs/provider`, and confirm each changes only the `Version:` line of its `identity/identity.cue`. Repeat with cli `v1.0.0-beta.4`, the version S2 lowers `.opm-cli-version` to and so the one phase B installs there. Record any deviation in design.md.
- [x] 1.6 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `test(cascade): add the cascade stub resolver and older pins`

## 2. Pin report, class map, title and body tasks

- [x] 2.1 Add `.tasks/cascade/classes` with Phase 2 cascade contract §5.3's opm-operator block, verbatim.
- [x] 2.2 Add `.tasks/cascade/pins.sh <WORKTREE|ref>` (executable, `set -euo pipefail`), printing the four TSV rows in design.md ("Pin report and class map"):
  - library from the `go.mod` require line;
  - the catalog from the sample Platform's `version:` after the `opmodel.dev/catalogs/opm@v4:` key, with a `v` added;
  - core from `test/fixtures/modules/hello/cue.mod/module.cue`;
  - the opm CLI from `.opm-cli-version`.

  A file missing at the ref omits its row. Any other failure exits 1.
- [x] 2.3 In `.tasks/deps.yaml`, add `cascade:title` and `cascade:body`. They run `"$CASCADE_RESOLVER" title|body --classes .tasks/cascade/classes --pins .tasks/cascade/pins.sh`, with the task-level `CASCADE_RESOLVER_PATH` var (a top-level `x-` anchor), the `CASCADE_RESOLVER` env and the `test -x` precondition with the contract's message (Phase 2 cascade contract §3).
- [x] 2.4 Check by hand:
  - `pins.sh WORKTREE` and `pins.sh HEAD` print identical rows on the clean tree;
  - `pins.sh HEAD~5` reads the older values;
  - `CASCADE_RESOLVER=relative task deps:cascade:title` fails with "must be absolute";
  - a missing resolver fails with the precondition message.
- [x] 2.5 `shellcheck .tasks/cascade/pins.sh`, `task dev:fmt dev:vet dev:lint dev:test` green, then commit `ci(cascade): add the cascade pin report, class map, and title and body tasks`

## 3. task deps:cascade

- [x] 3.1 Add `.tasks/cascade/cascade.sh` with the prologue from design.md ("`cascade.sh`: three phases"):
  - the clean-start check, or the `CASCADE_ALLOW_DIRTY=1` snapshot (Phase 2 cascade contract §5.2 rule 1);
  - the state directory and the warnings file;
  - the GHCR `CUE_REGISTRY` and `OPM_REGISTRY`;
  - `check-files --repo-root .`.
- [x] 3.2 Phase A, resolve. Implement it as follows:
  - `newest` for library, the catalog (`--current` = the sample, with `v` added) and the opm CLI, each with `--current <now> --repo-root .`, plus `--expect <v>` when `CASCADE_EXPECT` names that pin;
  - `pin-of` for the core of each distinct per-file catalog `C = max(file catalog, K)`, and `hold opmodel.dev/core@v2`;
  - `language-of` for each moved CUE upstream, compared with `CUE_VERSION` at `.github/workflows/test.yml:19`;
  - the per-file plan, including `is-frozen` per file and key.

  - `f_changed` for each advance module against the unmodified tree, to decide whether phase B is needed.

  Every resolver call, in every phase, sits inside an `if` or a `case` (0 move, 3 stay or "no", other: exit with that code); a 3 from `pin-of`, which means the catalog pins no core, is an error. An `EXIT` trap rewrites a status of 3 to 1 unless `result()` set the result. There is no `|| true`, no `2>/dev/null ||` and no `set +e`.
- [x] 3.3 Phase B: when any pin will move or any advance module already changed (`f_changed`), run `GOBIN=$STATE/bin go install github.com/open-platform-model/cli/cmd/opm@$(cat .opm-cli-version)` from the unmodified tree.
- [x] 3.4 Phase C, edits in Phase 2 cascade contract §5.2 rule 12 order:
  - library: `go get library@<v>`, then `go mod tidy`, warning on a raised third-party pin;
  - the catalog, as text: the sample Platform (bare, quoted) and `test/fixtures/catalog.go` (bare; `gofmt -l` must be clean);
  - the four fixture modules and the provider catalog: `cue mod get` with exact versions and no frozen keys, then one `cue mod tidy`, then the frozen-key byte check;
  - the core-ahead and core-hold warnings (design.md, "Consistent set across the operator's files").
- [x] 3.5 Version advance for the four fixture modules and the provider catalog:
  - `f_changed` is copied verbatim from Phase 2 cascade contract §5.2 rule 11;
  - `B` comes from the merge-base;
  - the target follows the `published cue <module>@v0 v<B>` rule;
  - the version is written with `$STATE/bin/opm module|catalog version set`, only when it differs.
- [x] 3.6 Consumers, each edit preceded by `is-frozen`:
  - the modulepackage `cue.mod/module.cue` text re-pin of the fixture `v:`, the catalog and core, with no `tidy`;
  - `moduleinstance.yaml`;
  - the sample ModuleInstance for the module it instantiates.

  Then, when any file under `config/samples/` changed, regenerate the resource reference with `go run ./hack/crdref`, warning with key `-` and continuing if it fails (design.md, "Regenerating the resource reference"). Then write `.opm-cli-version` last, as `<v>\n`. Then the result: exit 0 if changed, 3 if not (rule 13).
- [x] 3.7 In `.tasks/deps.yaml`, add `cascade`, which runs `.tasks/cascade/cascade.sh`. It gets the same resolver var, env and precondition as 2.3, and a `desc` that names the exit codes and `task -x`.
- [x] 3.8 Smoke-test by hand in a scratch copy (`git ls-files | tar` into a temp dir, `git init`), with the stub and a hand-written table:
  - current values give exit 3 and a clean tree;
  - an `ERROR` row gives a non-0/3 exit and a clean tree;
  - an untracked file gives exit 1;
  - a missing `pin-of` row gives a non-0/3 exit and a clean tree.
- [x] 3.9 `shellcheck .tasks/cascade/cascade.sh`, `task dev:fmt dev:vet dev:lint dev:test` green, then commit `ci(cascade): add task deps:cascade`

## 4. task deps:cascade:test

- [x] 4.1 Add `.tasks/cascade/test.sh`, following Phase 2 cascade contract §8.
  - Pre-checks:
    - `yq --version` names mikefarah;
    - the stub's `sha256sum` matches;
    - `pins.sh WORKTREE` and `pins.sh HEAD` agree on a clean sandbox.
  - Per-scenario sandbox:
    - `git ls-files … | tar` into `$(mktemp -d)/r`;
    - `git init` and a `base` commit, the setup edits and a `setup` commit, and `CASCADE_BASE` set to that SHA; every sandbox git call runs with `GIT_AUTHOR_NAME`, `GIT_AUTHOR_EMAIL`, `GIT_COMMITTER_NAME` and `GIT_COMMITTER_EMAIL` exported and `-c commit.gpgsign=false`, so a CI runner without a git identity can commit;
    - export `CASCADE_RESOLVER` (the stub), `CASCADE_STUB_TABLE`, `CASCADE_STUB_LOG` and `CASCADE_TODAY=2026-10-03`;
    - a cleanup `trap`.
  - Build the stub tables at test time, from `pins.sh WORKTREE`, the `pin-of` of the tree catalog, `published` rows for each advance module's `B`, and `older.tsv`. Assert with the stub's `semver-cmp` that each `older.tsv` version is strictly older than the tree's.
- [x] 4.2 Offline scenarios (asserted with `task -x deps:cascade`):
  - **S1:** exit 3, a clean tree, the normalized stub log equal to `testdata/s1-calls.txt`, and every `newest` line carrying `--current` and `--repo-root`;
  - **S3:** the library `newest` row is `ERROR`; the exit is neither 0 nor 3, and the tree is clean;
  - **S6:** with an untracked file, exit 1 and nothing else changed;
  - **S3b:** no `pin-of` row for the tree's catalog; the exit is neither 0 nor 3, and the tree is clean (design.md, "S3b").

  Write `testdata/s1-calls.txt` from the first green S1 run, and check it against the list in design.md ("`s1-calls.txt`").
- [x] 4.3 Network scenarios:
  - **S2:** lower every pin location to `older.tsv`, with library through `go mod edit -require` (design.md, "S2 setup for the Go pin"), and run. Expect:
    - exit 0;
    - the diff against the original tree is exactly the design.md golden list;
    - after a commit, with `CASCADE_BASE` kept, a second run exits 3 and leaves the identity versions unchanged.
  - **S4:** the S2 setup plus the design.md `.cascade-frozen` entry for `test/fixtures/modules/hello/cue.mod/module.cue`. Expect that file byte-unchanged and exit 0.
  - **S5:** runs only when `CASCADE_RESOLVER_REAL` is set, after S2. Expect:
    - `title` prints `fix(deps): bump 4 upstream pins` (library, the opm catalog, core and the opm CLI all move back from `older.tsv`);
    - the body has both markers, one table row per moved pin and `## Notes` last.
- [x] 4.4 In `.tasks/deps.yaml`, add `cascade:test`, running `.tasks/cascade/test.sh` and honouring `CASCADE_TEST_SET` (`offline` or `all`, default `all`). It has no `CASCADE_RESOLVER_PATH` var and no resolver precondition, because `test.sh` exports the stub itself and CI has no `.github` checkout beside the repo (design.md, "File layout and task wiring"; a contract §3 deviation reported to the supervisor).
- [x] 4.5 Run `CASCADE_TEST_SET=offline task -x deps:cascade:test` (expect PASS for the pre-checks and S1, S3 and S6). Then run `task -x deps:cascade:test` with network (expect PASS for S2 and S4), and once more with `CASCADE_RESOLVER_REAL` pointing at the resolver's worktree (expect PASS for S5). Then confirm the real checkout's `git status` is unchanged.
- [x] 4.6 `shellcheck .tasks/cascade/*.sh`, `task dev:fmt dev:vet dev:lint dev:test` green, then commit `test(cascade): test task deps:cascade against the stub resolver`

## 5. CI and docs

- [x] 5.1 In `.github/workflows/lint.yml` job `Lint`, after the G1 step, add the step `Cascade task (offline)`, which runs `task -x deps:cascade:test` with `env: CASCADE_TEST_SET: offline`.
- [x] 5.2 Add `.github/workflows/cascade-task.yml`, as in design.md ("Test placement"):
  - job `Cascade task (network)`, with `timeout-minutes: 20` and `permissions: contents: read`;
  - triggers: `pull_request` paths `.tasks/cascade/**`, `Taskfile.yml`, `.tasks/*.yaml` and the workflow; `workflow_dispatch`; and a weekly `schedule`;
  - checkout with `fetch-depth: 0`, then Go (`go-version-file: go.mod`), CUE (`CUE_VERSION` env) and Task, every action SHA-pinned like `test.yml`;
  - a checkout of `open-platform-model/.github` at `main` into `org-github`, with `persist-credentials: false`, and `CASCADE_RESOLVER_REAL` set to its `cascade-resolve.sh`;
  - then `task -x deps:cascade:test`.
- [x] 5.3 Check that both workflows parse (`python3 -c 'import yaml,sys;[yaml.safe_load(open(f)) for f in sys.argv[1:]]' .github/workflows/*.yml`, and `actionlint` if available).
- [ ] 5.4 Once `.github` `add-cascade-resolver` has merged, run `cascade-task.yml` on the PR (or locally with `CASCADE_RESOLVER_REAL` pointing at a `.github` checkout of `main`), and confirm S5 passes. Until then, leave this box open and say so in the PR body.
- [x] 5.5 Add one bullet to `AGENTS.md` "Registry". It says that `task -x deps:cascade` moves the upstream pins (library, the opm catalog and core with fixtures and consumers, and `.opm-cli-version`) and exits 0 changed, 3 nothing, or other on error; that `deps:cascade:title|body|test` exist; and that `.tasks/cascade/` holds the scripts (workspace RELEASING.md, section "What each repo's task moves").
- [x] 5.6 `task dev:fmt dev:vet dev:lint dev:test` and `CASCADE_TEST_SET=offline task -x deps:cascade:test` green, then commit `ci(cascade): run the deps:cascade tests in CI`

## 6. Verify follow-ups

- [x] 6.1 A fixture consumer's catalog and core only move up (design.md, "Consumers follow in the same PR"): under a core hold, a module held below its modulepackage dragged the consumer down. Committed as `ci(cascade): never lower a fixture consumer's catalog or core`.
- [x] 6.2 Script the spec scenarios the contract's S1 to S6 leave to hand checks (design.md, "Scenarios beyond the contract"): S7 core hold, S8 pending fixture, S9 core ahead (offline), S10 MVS raising a frozen key (network). Confirm S7 fails against the mover before 6.1.
- [x] 6.3 `shellcheck .tasks/cascade/*.sh`, `task dev:fmt dev:vet dev:lint dev:test` and `task -x deps:cascade:test` green, then commit `test(cascade): cover core holds, pending fixtures, core ahead and frozen MVS raises`

## 7. Implementation review follow-ups

- [x] 7.1 Check a module's catalog freeze before choosing its core, so a frozen catalog keeps the core it pins (contract §5.2 rule 7), and script it as S12 (offline). Confirm S12 fails against the mover before the fix. Commit `fix(cascade): choose core from a frozen catalog, not the newest`

- [x] 7.2 Add a repo-root `.cascade-frozen` that freezes `opmodel.dev/catalogs/opm@v4` at `test/integration/reconcile/skew_test.go` and `internal/controller/platform_controller_test.go` (both resolve catalog `4.0.0` on purpose). Commit `test(cascade): freeze the two deliberately old catalog literals`

## 8. Archive

- [ ] 8.1 Run `openspec verify` for `add-deps-cascade-task` and resolve its findings. Then archive the change on this branch (`openspec archive add-deps-cascade-task`), so the archive rides the implementing PR; never push to main (owner decision 2026-10-01, workspace RELEASING.md, section "Owner settings"). Commit `chore(openspec): archive add-deps-cascade-task`
