## Why

Phase 2 of the release cascade (workspace RELEASING.md, section "Rollout and changes") gives each
downstream repo one task that moves its upstream pins to the newest published versions:
`task deps:cascade`. The Phase 3 receive workflow will run that task on every upstream release
and on a daily sweep, and push its diff to the rolling `deps/cascade` PR (workspace RELEASING.md,
section "The cascade"). The workflow computes nothing itself. The repo's own task decides what
moves, so the task has to exist, behave the same on every run, and be tested before the wiring
lands.

opm-operator is tier 2 (workspace RELEASING.md, section "Release order"). Today every pin it
holds is moved by hand, or by root workspace scripts that miss some of them:

- **library**, a shipped Go pin, `go.mod:14` (`v1.0.0-beta.1`, while `v1.0.0-beta.3` is
  published). Dependabot no longer proposes it (`prepare-release-cascade`), so only a hand-made
  `fix(deps)` PR moves it.
- **opm catalog and core**, test pins in eleven places:
  - `config/samples/opmodel.dev_v1alpha1_platform.yaml:22`;
  - `test/fixtures/catalog.go:19`;
  - the four fixture modules' `cue.mod/module.cue:9-14`;
  - `test/fixtures/catalogs/provider/cue.mod/module.cue:9-11` (core only);
  - the four modulepackage consumers' `cue.mod/module.cue:9-17`.

  The root `.tasks/deps/platform-pins.sh` misses `catalog.go`. The root `fixtures.sh` advances
  fixture versions again on every run, and swallows a failing `task examples:pin`.
- **opm CLI**, the release-tool pin `.opm-cli-version` (one line, read by four workflows).

The design is fixed by workspace RELEASING.md (sections "The cascade", "What each repo's task
moves", "Cascade files") and by the Phase 2 cascade contract
(`/var/home/emil/.cache/claude-tmp/claude-1000/-var-home-emil-dev-open-platform-model/2ee0ca8e-268c-4b20-8bd9-b5e4f0d96717/scratchpad/p2-cascade-contract.md`, version 1; cited as "Phase 2 cascade contract §N"). The contract binds
this change and its three siblings in catalog_opm, library and cli to one shared resolver in
`open-platform-model/.github`.

## What Changes

- **`task deps:cascade`** (Phase 2 cascade contract §5.1, §5.2, §6.3) runs
  `.tasks/cascade/cascade.sh`. The script:
  - edits the working tree only;
  - exits 0 when it changed files, 3 when there was nothing to do, and any other code on error;
  - resolves every target through the shared resolver before it edits anything;
  - moves library (`go get` plus `go mod tidy`);
  - moves the opm catalog and core as a consistent set across the sample Platform, `catalog.go`,
    the four fixture modules and the provider catalog fixture;
  - advances each changed fixture's version once per PR;
  - re-pins the fixture consumers (modulepackages, `moduleinstance.yaml` files, the sample
    ModuleInstance) in the same run;
  - writes `.opm-cli-version` last.

  It honours `.cascade-frozen` and `.cascade-hold`, never names a third-party pin, and never
  touches `.github/`, the release-please files or `hack/fixtures.sh`.
- **`task deps:cascade:title` and `task deps:cascade:body`** are thin wrappers. They call the
  resolver's `title` and `body` subcommands with this repo's pin report and path-class map, so
  every repo's cascade PR has the same title and body format (Phase 2 cascade contract §1, §4).
- **`.tasks/cascade/pins.sh`** reports the four logical pins (library, opm catalog, core, opm
  CLI) at the working tree or at a git ref. **`.tasks/cascade/classes`** is the path-class map
  (Phase 2 cascade contract §5.3, verbatim).
- **`task deps:cascade:test`** runs `.tasks/cascade/test.sh`. It runs `deps:cascade` in sandbox
  copies of the tree against a stub resolver (Phase 2 cascade contract §7, §8), covering six
  scenarios:
  - S1: nothing to do;
  - S2: older pins, with the expected diff and idempotence;
  - S3: a resolver error leaves the tree untouched;
  - S4: frozen pins;
  - S5: title and body against the real resolver;
  - S6: a dirty tree is refused.

  The offline set runs as a step in the existing `Run on Ubuntu` job of
  `.github/workflows/test.yml`. The network set runs in a new, non-required workflow,
  `.github/workflows/cascade-task.yml`.
- **`AGENTS.md`** ("Registry") gains one bullet naming the task, its exit codes and `task -x`.

Release class: none. Every commit is `ci`, `test` or `docs`, which are hidden sections, so this
change cuts no operator release. After GA it would still be no release, because it only adds
tooling. No API type, CRD, controller or reconcile phase changes.

## Depends on / gates

- **Depends on `.github` `add-cascade-resolver` merged first** (workspace RELEASING.md, section
  "Rollout and changes", the Phase 2 row; Phase 2 cascade contract §10).
  - Sections 1 to 4 build and test against the stub, a byte-identical copy of the canonical text
    in Phase 2 cascade contract §7, so they do not need the resolver.
  - The PR merges only after the resolver has merged, and after S5 has passed against it in
    `cascade-task.yml`.
- **Depends on opm-operator `prepare-release-cascade`**: merged and archived
  (`openspec/changes/archive/2026-10-02-prepare-release-cascade`). It created `.opm-cli-version`
  and `.tasks/deps.yaml`, which this change extends.
- **Gates later changes:**
  - opm-operator `join-release-cascade` (Phase 3) runs `task -x deps:cascade`,
    `deps:cascade:title` and `deps:cascade:body` from the receive workflow.
  - The workspace Phase 5 rewire of `task deps:update` calls this task.
- **Phase 2 exit gate** (workspace RELEASING.md, section "Rollout and changes": "a run on `main`
  exits 3; a run against an older pin produces the expected diff").
  - The second half is S2.
  - The first half cannot hold on the day this merges, because `main` is behind today: library
    `beta.1` against `beta.3`, and catalog `4.4.4` against `v4.5.1`.
  - It is met by a separate catch-up PR that the supervisor opens after library's catch-up
    release is published (Phase 2 cascade contract §8, tier 2). That PR is not part of this
    change.
- **Not dependent on** catalog_opm, library or cli `add-deps-cascade-task`. Those are parallel
  changes under the same contract.

## Capabilities

### New Capabilities

- `deps-cascade`: the `deps:cascade` task family. It covers:
  - what it moves and how;
  - its exit codes;
  - the frozen, hold and consistent-set rules;
  - version advance and consumers;
  - title and body delegation;
  - its tests.

### Modified Capabilities

None. `release-automation` is unchanged: G1, Dependabot, `.opm-cli-version` and the release flow
stay as they are.

## Impact

- **New files:**
  - `.tasks/cascade/`, containing:
    - `cascade.sh`, `pins.sh`, `classes` and `test.sh`;
    - `testdata/stub-resolve.sh`, `testdata/older.tsv` and `testdata/s1-calls.txt`;
  - `.github/workflows/cascade-task.yml`.
- **Changed files:**
  - `.tasks/deps.yaml`: four tasks;
  - `.github/workflows/test.yml`: one step in `Run on Ubuntu`;
  - `AGENTS.md`: one bullet.
- **Untouched:**
  - `hack/fixtures.sh`, which stays byte-identical to the cli copy (root `task fixtures:lint`);
  - `.tasks/examples.yaml`;
  - every pin value. This change moves no pin; the catch-up PR does.
- **Tools the task needs at run time:** `git`, `go`, `cue` and `awk`, and mikefarah `yq` v4,
  which the stub and the resolver use. The opm CLI is installed by the task itself into
  `$(git rev-parse --git-dir)/cascade/bin`.
- **Delivery:** one PR. The OpenSpec archive commit rides that PR, and nothing is pushed to
  `main` (owner decision 2026-10-01, workspace RELEASING.md, section "Owner settings").
