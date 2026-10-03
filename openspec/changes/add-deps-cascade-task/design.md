## Context

Sources of truth, in order:

1. Workspace `RELEASING.md`, sections "The cascade" (the receiver and its resolver rules), "What
   each repo's task moves", "Gates", "Cascade files", and "Rollout and changes".
2. The Phase 2 cascade contract, version 1, at
   `/var/home/emil/.cache/claude-tmp/claude-1000/-var-home-emil-dev-open-platform-model/2ee0ca8e-268c-4b20-8bd9-b5e4f0d96717/scratchpad/p2-cascade-contract.md`.
   It is cited below as "contract §N". Where the contract and RELEASING.md disagree,
   RELEASING.md wins and the implementer reports the conflict to the supervisor instead of
   picking.

**Reconcile phase impact: none.** No change to Source, Render, Apply, Prune or Status. No change
to any CRD, controller or Go package under `api/`, `cmd/` or `internal/`. Everything is shell
under `.tasks/cascade/`, Taskfile wiring, and two CI workflows.

### Pins on `main` today

Measured 2026-10-04 at `d2dda57`:

| Pin key | Display | Class | Where (file:line) | Value |
| --- | --- | --- | --- | --- |
| `github.com/open-platform-model/library` | library | shipped | `go.mod:14` (+ `go.sum`) | `v1.0.0-beta.1` |
| `opmodel.dev/catalogs/opm@v4` | opm catalog | test | `config/samples/opmodel.dev_v1alpha1_platform.yaml:22`, the `version:` under the key at `:15`, stored bare and quoted | `"4.4.4"` |
| (same key) | | test | `test/fixtures/catalog.go:19` `return "4.4.4"`, bare | `4.4.4` |
| (same key) | | test | `test/fixtures/modules/{hello,hello_web,podinfo,redis}/cue.mod/module.cue:9-11` | `v4.4.4` |
| `opmodel.dev/core@v2` | core | test | the same four files `:12-14`; `test/fixtures/catalogs/provider/cue.mod/module.cue:9-11` (core only) | `v2.0.0-beta.1` |
| `github.com/open-platform-model/cli` | opm CLI | release-tool | `.opm-cli-version` (one line) | `v1.0.0-beta.7` |

The representative locations that `pins.sh` reads (contract §6.3) are the sample Platform for
the catalog and `test/fixtures/modules/hello/cue.mod/module.cue` for core.

### What the task moves but does not report as a pin

These are fixture versions and their consumers (contract §4.1: a repo's own fixture versions are
not pins):

| What | Where |
| --- | --- |
| fixture module versions | `test/fixtures/modules/*/identity/identity.cue:18` (`0.0.12`, `0.1.10`, `0.1.11`, `0.1.14`) |
| provider catalog version | `test/fixtures/catalogs/provider/identity/identity.cue:16` (`0.1.0`; "bump is `opm catalog version set`", `:12-15`) |
| modulepackage consumers | `test/fixtures/modulepackages/*/cue.mod/module.cue:9-17`: catalog, core, and `testing.opmodel.dev/modules/operator/<m>@v0` |
| moduleinstance pins | `test/fixtures/modules/hello/moduleinstance.yaml:40`, `hello_web/…:40`, `podinfo/…:43`, `redis/…:43`; `config/samples/opmodel.dev_v1alpha1_moduleinstance.yaml:12` (instantiates `hello`, `:11`) |

### Never touched

- `config/samples/opmodel.dev_v1alpha1_moduleinstance_jellyfin.yaml` and `ocirepository.yaml`.
- `internal/source/testdata/minimal-module`, which has no deps.
- `hack/fixtures.sh`, which is byte-identical to the cli copy and linted by the root
  `task fixtures:lint`.
- `.tasks/examples.yaml`.
- Synthetic skew literals such as `v4.0.1` in `internal/reconcile/warnings_test.go`. They are not
  pins (workspace RELEASING.md, section "Cascade files").
- Two deliberately old catalog literals that tests do resolve from a registry:
  `skewedCatalogVersion()` in `test/integration/reconcile/skew_test.go:42-51` (`4.0.0`) and
  `pinned` in `internal/controller/platform_controller_test.go:614` (`4.0.0`). Both must stay
  older than `CatalogVersion()`. Workspace RELEASING.md, section "Cascade files", calls such a pin
  frozen ("An old pin without an entry is stale"). The task edits no Go test file, so it never
  moves them, but the operator has no `.cascade-frozen` entry for them and the RELEASING.md "Pin
  classes" table has no operator frozen row. Whether to add one is for the supervisor (see "Open
  Questions").
- Version prose in `docs/site/start/install-the-operator.md` (see "Open questions"). The
  generated block of `docs/site/reference/operator-resources.md` is not prose: the task
  regenerates it (see "Regenerating the resource reference").
- `docs-kit.cue`, `.opm-docs-version` and every docs-kit ref.
- Everything in contract §5.2 rule 14.

### Live facts the design relies on

Read-only checks on 2026-10-04:

- **proxy.golang.org** `library/@v/list`, sorted, ends `v1.0.0-alpha.36`, `v1.0.0-beta.1`,
  `beta.2`, `beta.3`.
- **The GHCR modulefile of `opmodel.dev/catalogs/opm`:**

  | Catalog | Core it pins | `language.version` |
  | --- | --- | --- |
  | `v4.4.2` | `v2.0.0-alpha.12` | `v0.17.0` |
  | `v4.4.4` | `v2.0.0-beta.1` | `v0.17.0` |
  | `v4.5.1` | `v2.0.0-beta.1` | `v0.17.0` |

- **Consequence for the catch-up.** On today's `main`, a real run moves:
  - library `beta.1` to `beta.3`;
  - the catalog `4.4.4` to `v4.5.1`.

  Core stays at `v2.0.0-beta.1`, the version `v4.5.1` pins, even though core `v2.0.0-beta.2` is
  published: that is the consistent-set rule. The opm CLI is already at the newest, `beta.7`.

  The expected catch-up title is therefore
  `fix(deps): bump library to v1.0.0-beta.3 and opm catalog to v4.5.1`.
- **The opm CLI has both setters.** `opm module version set <version> [path]` and
  `opm catalog version set <version> [path]` exist (cli `internal/cmd/module/version.go:30`,
  `internal/cmd/catalog/version.go:31`). Both are also present at cli `v1.0.0-beta.4`, the
  version S2 lowers `.opm-cli-version` to and therefore the one phase B installs there. Spike 1.5
(2026-10-04): at both `v1.0.0-beta.7` and `v1.0.0-beta.4`, `opm module version set 0.0.13
test/fixtures/modules/hello` and `opm catalog version set 0.1.1 test/fixtures/catalogs/provider`
change only the `Version:` line of the fixture's `identity/identity.cue`, with no registry
access.
- **go-task** is 3.52.0 locally. Contract §3 records that only `task -x` propagates exit 3.

## Goals / Non-Goals

**Goals:**

- `task -x deps:cascade` moves exactly the pins in the table above, plus the fixture versions and
  consumers they force, and exits 0, 3 or other per contract §2.3 and §5.2 rule 13.
- `task -x deps:cascade:title` and `task -x deps:cascade:body` print the shared title and body
  for this repo's diff.
- `task -x deps:cascade:test` proves S1 to S6 (contract §8). The offline set is part of required
  PR CI.

**Non-Goals:**

- Moving any pin on `main`. That is the supervisor's catch-up PR (contract §8).
- The receive workflow, labels, `deps-cascade:breaking`, or pushing. Those are Phase 3.
- Changing `hack/fixtures.sh`, `.tasks/examples.yaml`, or the root `.tasks/deps/*.sh` (contract
  §10).
- Crossing a major (`catalogs/opm@v5`, `core@v3`, `library/v2`). The task only passes the
  resolver's warning through.

## Decisions

### File layout and task wiring

```
.tasks/deps.yaml                    # + deps:cascade, deps:cascade:title, deps:cascade:body, deps:cascade:test
.tasks/cascade/cascade.sh           # the mover (phases A, B, C)
.tasks/cascade/pins.sh              # contract §4.1 pin report
.tasks/cascade/lib.sh               # stdin readers shared by pins.sh and cascade.sh
.tasks/cascade/classes              # contract §5.3, verbatim
.tasks/cascade/test.sh              # contract §8
.tasks/cascade/testdata/stub-resolve.sh   # contract §7, byte-identical, mode 0755
.tasks/cascade/testdata/older.tsv
.tasks/cascade/testdata/s1-calls.txt
```

- **Task names.** The tasks are added to the existing `.tasks/deps.yaml`, which `Taskfile.yml:116-117`
  includes as `deps`. That yields `deps:cascade`, `deps:cascade:title`, `deps:cascade:body` and
  `deps:cascade:test`, so the invoked names match contract §5.1 exactly.
- **The resolver path.** `deps:cascade`, `deps:cascade:title` and `deps:cascade:body` declare
  the `CASCADE_RESOLVER_PATH` var at task level, through a YAML anchor, never as a global `vars:`
  entry. Each exports `CASCADE_RESOLVER: '{{.CASCADE_RESOLVER_PATH}}'` and has the `test -x`
  precondition with the contract's message (contract §3).
- **`deps:cascade:test` has no resolver var and no precondition.** This deviates from contract §3,
  which gives all four tasks both, and is reported to the supervisor. `test.sh` exports the
  stub itself (contract §8 step 5) and reads the real resolver only from
  `CASCADE_RESOLVER_REAL` (S5). With the precondition, the offline CI step would fail on every
  PR: CI has no `.github` checkout beside the repo and sets no `CASCADE_RESOLVER`, so the
  default path does not exist.
- **Working directory.** An included taskfile runs in the root Taskfile's directory unless it
  sets `dir:`, so the scripts are called with repo-relative paths.

```yaml
# .tasks/deps.yaml (sketch)
x-cascade-resolver: &cascade_resolver
  CASCADE_RESOLVER_PATH:
    sh: |
      if [ -n "${CASCADE_RESOLVER:-}" ]; then
        case "$CASCADE_RESOLVER" in /*) ;; *) echo "CASCADE_RESOLVER must be absolute" >&2; exit 1 ;; esac
        printf '%s\n' "$CASCADE_RESOLVER"; exit 0
      fi
      common=$(git rev-parse --path-format=absolute --git-common-dir) || exit 1
      printf '%s\n' "$(dirname "$common")/../.github/.github/scripts/cascade/cascade-resolve.sh"

tasks:
  cascade:
    desc: 'Move upstream pins to the newest published versions in the working tree (exit 0 changed, 3 nothing to do; run with task -x)'
    vars: *cascade_resolver
    env: { CASCADE_RESOLVER: '{{.CASCADE_RESOLVER_PATH}}' }
    preconditions:
      - sh: test -x '{{.CASCADE_RESOLVER_PATH}}'
        msg: cascade resolver not found: check out open-platform-model/.github beside this repo, or set CASCADE_RESOLVER
    cmds: [.tasks/cascade/cascade.sh]
```

Spike 1.3 (go-task 3.52.0, 2026-10-04): an included taskfile accepts a top-level
`x-cascade-resolver:` anchor that its tasks alias with `vars: *cascade_resolver`. `task -x` passes
a script's `exit 3` through as 3, while plain `task` gives 201. A relative `CASCADE_RESOLVER` fails
the `sh:` var with "must be absolute" (exit 1 under `-x`), and a missing resolver fails the
precondition with the contract's message.

### `cascade.sh`: three phases

The script follows contract §5.2 rules 1 to 15, in this shape:

```bash
#!/usr/bin/env bash
set -euo pipefail
R="$CASCADE_RESOLVER"
STATE="$(git rev-parse --git-dir)/cascade"; mkdir -p "$STATE"; : >"$STATE/warnings"
export CASCADE_WARNINGS="$STATE/warnings"
export CUE_REGISTRY='testing.opmodel.dev=ghcr.io/open-platform-model,opmodel.dev=ghcr.io/open-platform-model,registry.cue.works'
export OPM_REGISTRY="$CUE_REGISTRY"          # rule 4: never Taskfile.yml:34's localhost:5000 default
clean_start_or_snapshot                      # rule 1 (CASCADE_ALLOW_DIRTY=1 → snapshot)
"$R" check-files --repo-root .               # rule 3

# Phase A: resolve everything (no edit). Each newest call goes through:
resolve() { # resolve <var> <kind> <coord|-> <current>   → sets <var> to target or ""
  local out rc; set +e   # ONLY to capture rc; the case below re-raises every other code
  out=$("$R" newest "$2" ${3:+"$3"} --current "$4" --repo-root . $(expect_for "$5")); rc=$?
  set -e
  case $rc in 0) printf -v "$1" '%s' "$out" ;; 3) printf -v "$1" '' ;; *) exit "$rc" ;; esac
}
resolve LIB  go  github.com/open-platform-model/library  "$(go_pin)"            github.com/open-platform-model/library
resolve CAT  cue opmodel.dev/catalogs/opm@v4            "v$(sample_catalog)"    opmodel.dev/catalogs/opm@v4
K=${CAT:-v$(sample_catalog)}
core_of "$K"                                       # pin-of, inside a case: 3 (no core row) is an error, never "nothing to do"
HOLD_CORE=$(hold_or_empty opmodel.dev/core@v2)     # rule 7: a hold caps core, and the catalog with it
resolve CLI  opm-cli ""                                  "$(cat .opm-cli-version)" github.com/open-platform-model/cli
language_warnings "$K"                             # rule 10, against .github/workflows/test.yml:19 CUE_VERSION
plan_fixture_edits                                  # per-file targets: C = max(file catalog, K), core = pin-of C; frozen lookups (rule 8)
plan_advances                                       # which advance modules will change: a pin moves there, or f_changed already true

# Phase B: tools, from the unmodified tree, only if a setter may run
[ -z "$setter_needed" ] || GOBIN="$STATE/bin" go install \
  "github.com/open-platform-model/cli/cmd/opm@$(cat .opm-cli-version)"

# Phase C: edit (rule 12 order)
move_library            # go get library@$LIB; go mod tidy; warn on a raised third-party pin
move_catalog_core       # sample (bare), catalog.go (bare), cue mod get/tidy per moved module, provider core
advance_versions        # rule 11: f_changed, published, next-patch, opm {module,catalog} version set
follow_consumers        # modulepackages (text), moduleinstance.yaml, sample ModuleInstance
regenerate_reference    # go run ./hack/crdref when a file under config/samples/ changed
write_cli_version       # last
result                  # rule 13: exit 0 changed, 3 not
```

- **Every resolver call is inside an `if` or a `case`.** Under `set -e`, a bare
  `X=$("$R" pin-of …)` that answers 3 would end the script with status 3, which the caller reads
  as "nothing to do", even in phase C with a partly edited tree. So every call (`newest`,
  `pin-of`, `hold`, `is-frozen`, `published`, `language-of`, `next-patch`, `check-files`) is
  written `if out=$("$R" …); then …; else rc=$?; case $rc in 3) …;; *) exit "$rc";; esac; fi`,
  never with `set +e`.
- **An `EXIT` trap is the backstop.** It rewrites an exit status of 3 to 1 unless `result()` set
  `CASCADE_RESULT_SET=1` first, and names the line that failed. Only `result()` can report
  "nothing to do".
- **Phase B decides from the plan, not from "a pin moves".** A fixture can already differ from
  the merge-base before the run (a human commit on the `deps/cascade` branch), while no pin
  moves. If `B` is published, the target is `next-patch(B)`, so the setter must exist. Phase A
  therefore evaluates `f_changed` for each advance module against the unmodified tree, and phase B
  installs the binary when any pin moves or any advance module already changed. The setter is
  only called when the target differs from the file.

### Consistent set across the operator's files (contract §5.2 rule 7)

Contract §6.3 names one representative per pin. The other files are moved per file, the same way
library's module loop works (contract §6.2 step 4):

- **The catalog target `K`.**
  - Resolve once, with `--current` set to the sample's value with a `v` added.
  - `K` is the target if the catalog moved, otherwise the sample's value.
  - Every catalog-pinning file whose catalog is below `K` moves to `K`. A file above `K` is never
    lowered. This covers the sample, `catalog.go` and the four fixture modules.
- **Core per file.**
  - `C` is the file's catalog after the move: `max(file catalog, K)`. That is `K` when the file
    moved to it, and the file's own catalog when that is above `K` (contract §5.2 rule 7: "`C` =
    the catalog target if it moved, otherwise the file's current catalog version").
  - The target is `pin-of opmodel.dev/catalogs/opm@v4 C opmodel.dev/core@v2`, but only if it is
    greater than the file's own core. `pin-of` is called once per distinct `C`.
  - Otherwise core stays, with the warning "core `<cur>` is ahead of the core `<x>` that catalog
    `<C>` pins" (contract §9.10, reported to the owner).
  - The provider catalog fixture pins core only. It uses the same `pin-of` value with the
    representative `K`, never `newest cue opmodel.dev/core@v2`.
- **A hold on core.** If `hold opmodel.dev/core@v2` is in date and its `max` is below the
  `pin-of` value, the catalog does not move in any file and core stays too. The warning is
  "catalog `<t>` needs core `<c>`, above the hold `<max>`; catalog held too" (contract §9.11).
- **The `cue mod get` call.** In each fixture module where a pin moved, the task runs one
  `cue mod get` that names `opmodel.dev/catalogs/opm@<K>` and `opmodel.dev/core@<core>` with
  exact versions (the module path without its `@vN`: `cue mod get` refuses
  `opmodel.dev/core@v2@v2.0.0-beta.1`). It leaves out any key frozen for that file, then runs `cue mod tidy` once. A
  module where nothing moved is not touched (rule 6).
- **The frozen check after tidy.** After `tidy`, the `v:` of each frozen key in that file is
  compared byte for byte. If MVS raised it, the task exits 1 with "freeze the whole module, or
  hold the upstream" (rule 8).

### Text edits (bare versions)

Two edits are plain text, each preceded by `is-frozen <file> opmodel.dev/catalogs/opm@v4`:

- **The sample Platform.** The `version:` line after the `opmodel.dev/catalogs/opm@v4:` key is
  rewritten, keeping the quotes. The awk is the `bump_after_key` idiom from the root
  `.tasks/deps/platform-pins.sh`, copied into `cascade.sh`. The root script is not called.
- **`catalog.go`.** The single `return "<bare>"` literal inside `CatalogVersion()` is rewritten,
  then `gofmt -l test/fixtures/catalog.go` must print nothing.

The `v` is removed when writing and added when reading (contract §2.2).

### Version advance once per PR (contract §5.2 rule 11)

There are five advance modules `F`, each with identity file `I` and setter `S`:

| F | Module | I | S |
| --- | --- | --- | --- |
| `test/fixtures/modules/<m>` (×4) | `testing.opmodel.dev/modules/operator/<m>@v0` | `<F>/identity/identity.cue` | `opm module version set <ver> <F>` |
| `test/fixtures/catalogs/provider` | `testing.opmodel.dev/catalogs/operator/provider@v0` | `<F>/identity/identity.cue` | `opm catalog version set <ver> <F>` |

- `M` is `git merge-base "${CASCADE_BASE:-origin/main}" HEAD`, and `B` is `M`'s `^Version:` line
  in `I`.
- `f_changed` is copied verbatim from contract §5.2 rule 11.
- The target is:
  - `B` when `F` did not change;
  - `next-patch(B)` when `published cue <module> v<B>` answers 0;
  - `B` otherwise, because B is pending and so is not bumped again.
- The setter is the binary installed in phase B, `$STATE/bin/opm`. It runs only when the target
  differs from the file.
- **Agreement with the existing gate.** This matches `hack/fixtures.sh check`
  (`hack/fixtures.sh:157-162,271-273`), which also measures from the merge-base, so PR CI's
  "changed implies bumped" step (`.github/workflows/test.yml:66-69`) agrees with the task.

### Consumers follow in the same PR (contract §6.3 step 4, §9.6)

PR CI seeds a job-local registry from the tree (`.github/workflows/test.yml:71-75`), so the
consumers must name the tree's fixture version. For each fixture module `m`, after the advance:

- **`test/fixtures/modulepackages/<m>/cue.mod/module.cue`.** The `v:` after
  `"testing.opmodel.dev/modules/operator/<m>@v0"` is rewritten to the module's version, and the
  catalog and core `v:` to the module's own values. This is a text edit and never `tidy`,
  because the new fixture version is not on GHCR until `publish-fixtures.yml` runs on merge.
- **`test/fixtures/modules/<m>/moduleinstance.yaml`.** The indented `version:` line is rewritten
  to `v<ver>`.
- **`config/samples/opmodel.dev_v1alpha1_moduleinstance.yaml`.** The `version:` after
  `path: <module path>` is rewritten, for the module that sample instantiates.

Each edit is preceded by `is-frozen <file> <key>`.

### Regenerating the resource reference

`docs/site/reference/operator-resources.md:10-434` is a block that `hack/crdref` generates from
`config/crd/bases`, `config/samples` and `internal/controller`. Its line 329 is the sample
Platform's `version: 4.4.4`. The required `Lint` job runs `task dev:docs:reference:check`
(`.github/workflows/lint.yml`), and `task docs:bundle:parity` compares the docs bundle's page with
that block. A cascade PR that moves the sample Platform without regenerating the block fails
`Lint`.

- When any file under `config/samples/` changed in this run, phase C runs `go run ./hack/crdref`
  as its regenerator (contract §5.2 rule 12), after the consumers and before `.opm-cli-version`.
  It never runs `task dev:docs:reference`, whose `manifests` dependency installs and runs
  controller-gen.
- If `crdref` fails to build or run (a library move that breaks compilation), the task warns with
  key `-` ("`hack/crdref` failed; regenerate `docs/site/reference/operator-resources.md` by
  hand") and continues, so the PR is still produced and its own `Lint` shows the break. This
  mirrors the cli's `docskit-dump` rule (contract §6.4 step 6). The failure is handled by an
  explicit `if`, never `|| true`.
- **Class.** The regenerated page changes only because a test pin moved. Under the contract's
  §5.3 map it matches no line and is therefore `shipped`, which would title a catalog-only
  cascade `fix(deps)` and cut an operator release. That contradicts workspace RELEASING.md,
  section "Pin classes" (opm-operator test class: `config/samples/`, `test/fixtures/`) and
  `AGENTS.md` ("a pin bump there is `test(fixtures)` and must not release the operator").
  RELEASING.md wins over the contract (contract preamble), so `classes` adds one line after the
  verbatim contract block: `test docs/site/reference/operator-resources.md`. This is reported to
  the supervisor as a contract conflict, with the proposal that the contract's opm-operator map
  gain the same line.

### `.opm-cli-version` last

`newest opm-cli --current "$(cat .opm-cli-version)"`. On 0 the task writes `<v>\n`. The opm CLI
used for phase B is the version before this write, so a run never depends on the pin it is moving
(contract §5.2 rule 11).

### Pin report and class map

- **`pins.sh <ref>`** reads the four representative locations, from disk for `WORKTREE` or with
  `git show <ref>:<path>`. It prints:

  ```
  github.com/open-platform-model/library	library	shipped	<v>	
  opmodel.dev/catalogs/opm@v4	opm catalog	test	v<sample>	
  opmodel.dev/core@v2	core	test	<hello core>	
  github.com/open-platform-model/cli	opm CLI	release-tool	<.opm-cli-version>	
  ```

  - The labels column is empty. `need-human-review` is library-only (contract §6.2).
  - The library version is read from the `require` line of `go.mod` with awk, not
    `go list` / `go mod edit`, so that the same code works on `git show` output.
- **`classes`** is contract §5.3's opm-operator block, verbatim, plus one line for the
  regenerated resource reference (see "Regenerating the resource reference"; reported to the
  supervisor):

  ```
  release-tool .opm-cli-version
  test config/samples/
  test test/
  test **/testdata/
  test *_test.go
  test docs/site/reference/operator-resources.md
  ```

  `go.mod` and `go.sum` fall through to `shipped`. With the catch-up diff above, the title is
  therefore `fix(deps)`, and a catalog-only or core-only diff, including the regenerated
  reference, is `test(fixtures)`.

## Research & Decisions

### Own consumer re-pin vs `task examples:pin`
**Context**: Contract §6.3 step 4 allows calling `task examples:pin` when it accepts explicit versions.
**Explored**: `.tasks/examples.yaml:78-134`. It reads each module's declared `Version` and
`ModulePath` with `cue eval ./identity`, which reflects the advanced version after the setter
runs. It rewrites `moduleinstance.yaml`, the modulepackage dep and its shared core and catalog,
and the sample. It does this unconditionally for every module, and it never consults
`.cascade-frozen`.
**Decision**: `cascade.sh` does its own re-pin, porting the awk from `examples:pin`, with an
`is-frozen` check per file. It does not call `examples:pin`.
**Rationale**: A frozen consumer must be skipped, not overwritten and then caught (rule 8). The
port is about 30 lines and leaves `examples.yaml` untouched for the e2e path that uses its
`PRERELEASE` mode.

### Where `language.version` is compared
**Context**: Contract §5.2 rule 10 says each repo names one file for "the local `CUE_VERSION`".
**Decision**: `.github/workflows/test.yml:19` (`CUE_VERSION: 'v0.17.1'`), the env of the PR test job, which contract §5.2 rule 10 names for opm-operator. It is read with `grep -oP "CUE_VERSION: '\K[^']+"`. If the value cannot be read, the task
warns with key `-` and continues.
**Rationale**: It is the version that PR CI installs (`test.yml:49-52`) and that runs the
fixtures. `test-e2e.yml` is not required.

### The S4 frozen choice (contract §8)
**Decision**: S4 appends this entry to `.cascade-frozen`, creating the file because the operator
has none:

```yaml
- path: test/fixtures/modules/hello/cue.mod/module.cue
  pins: [opmodel.dev/catalogs/opm@v4, opmodel.dev/core@v2]
  reason: S4
```

**Rationale**:
- It freezes every OPM key the file pins, as the contract requires.
- It exercises the "leave the key out of `cue mod get`" path.
- It keeps `hello`'s consumer (`modulepackages/hello`) following an unchanged module. The file
  must be byte-unchanged, `hello`'s identity is not advanced because `f_changed` is false, and
  exit 0 holds because library and the other three modules still move.

### `older.tsv` (contract §8)
**Context**: S2 needs real published versions strictly older than the tree.
**Decision**:

```
github.com/open-platform-model/library	v1.0.0-alpha.36
opmodel.dev/catalogs/opm@v4	v4.4.2
opmodel.dev/core@v2	v2.0.0-alpha.12
github.com/open-platform-model/cli	v1.0.0-beta.4
pin-of	opmodel.dev/catalogs/opm@v4	v4.4.2	opmodel.dev/core@v2	v2.0.0-alpha.12
```

**Rationale**:
- All five values were read live on 2026-10-04.
- Core `alpha.12` is what catalog `v4.4.2` pins, so the S2 setup is itself a consistent set.

### S2 setup for the Go pin
**Context**: S2 must restore `go.mod` and `go.sum` byte-identically, or the golden diff fails on
lines that have nothing to do with the task.
**Decision**:
- The setup lowers library with `go mod edit -require=github.com/open-platform-model/library@v1.0.0-alpha.36`
  and no `tidy`.
- The task's `go get …@<tree value>` plus `go mod tidy` must then reproduce the tree's `go.mod`
  and `go.sum`.
**Rationale**: `go get` of an older version could lower other modules by MVS, and the later
upgrade would not restore them. Spike 1.4 (2026-10-04): `go mod edit -require=…library@v1.0.0-alpha.36`, then
`go get …library@v1.0.0-beta.1 && go mod tidy`, reproduces `go.mod` and `go.sum` byte for byte.
The test therefore compares both files.

### The S2 golden list
After S2's first run, the diff against the original tree must be exactly these version-advance
paths. Each fixture's pins changed between the setup commit and the run, so each advances once:

- `test/fixtures/modules/{hello,hello_web,podinfo,redis}/identity/identity.cue`
- `test/fixtures/catalogs/provider/identity/identity.cue`
- `test/fixtures/modules/{hello,hello_web,podinfo,redis}/moduleinstance.yaml`
- `config/samples/opmodel.dev_v1alpha1_moduleinstance.yaml`
- `test/fixtures/modulepackages/{hello,hello_web,podinfo,redis}/cue.mod/module.cue` (the fixture
  `v:` only)

The second run, with `CASCADE_BASE` still the setup SHA, exits 3: `B` is unchanged and its
`next-patch` already equals the file.

### `s1-calls.txt` (contract §8)
On an up-to-date tree the stub log, with each version normalized to `V` and the lines sorted,
must be:

```
check-files --repo-root .
hold opmodel.dev/core@v2 --repo-root .
newest cue opmodel.dev/catalogs/opm@v4 --current V --repo-root .
newest go github.com/open-platform-model/library --current V --repo-root .
newest opm-cli --current V --repo-root .
pin-of opmodel.dev/catalogs/opm@v4 V opmodel.dev/core@v2
```

- No `published` call appears, because no fixture changed.
- No `language-of` call appears, because no CUE upstream moved.
- The exact argument order is fixed by the implementation in section 3. This file is written
  from the first green S1 run and reviewed against this list.

### S3b: a non-`newest` answer of 3 is never "nothing to do"
**Decision**: The offline set adds one scenario beyond contract §8, S3b. The stub table has no
`pin-of` row for the tree's catalog, so `pin-of` exits 3. The task must exit with a code other
than 0 or 3 and leave `git status --porcelain` empty.
**Rationale**: It proves the `if`/`case` rule and the `EXIT` trap in "`cascade.sh`: three phases"
(the spec's "never turns a failure into 0 or 3").

### Test placement (contract §8, §9.12)
**Context**: Contract §8 places the offline step in `test.yml` job `Run on Ubuntu`, calling it
"an existing required job". Workspace RELEASING.md, section "Rulesets on main", lists
opm-operator's only required check as `Lint`. RELEASING.md wins (contract preamble); the conflict
is reported to the supervisor.
**Decision**:
- **Offline set.** `CASCADE_TEST_SET=offline task -x deps:cascade:test` runs as a step in
  `.github/workflows/lint.yml` job `Lint`, after "Install Task" and the G1 step. `Lint` already
  has setup-go (with its module cache restore) and setup-task. The offline set needs `git`,
  `awk`, `tar`, `sha256sum` and `yq`, which `ubuntu-latest` has, and no `cue` and no opm CLI
  (S1, S3 and S6 move nothing).
- **Sandbox commits.** `actions/checkout` sets no git identity, so every sandbox `git commit` in
  `test.sh` runs with `GIT_AUTHOR_NAME`, `GIT_AUTHOR_EMAIL`, `GIT_COMMITTER_NAME` and
  `GIT_COMMITTER_EMAIL` exported and `-c commit.gpgsign=false`, so a runner (or a developer with
  signing on) never aborts with "Author identity unknown".
- **Network set.** It runs in the new `.github/workflows/cascade-task.yml`, job
  `Cascade task (network)`, with `timeout-minutes: 20` and `permissions: contents: read`.
  - Triggers: `pull_request` on `.tasks/cascade/**`, `Taskfile.yml`, `.tasks/*.yaml` and the
    workflow itself; `workflow_dispatch`; and a weekly `schedule`.
  - It checks out this repo at `path: repo` and `open-platform-model/.github` at `main`
    beside it (`path: org-github`, `persist-credentials: false`), so the resolver is never an
    untracked directory inside the tree that the scenarios copy (contract §3, "In CI"). It sets
    `CASCADE_RESOLVER_REAL` only when `cascade-resolve.sh` exists there, so S5 reports SKIP until
    the resolver is on `.github` `main`.
  - Every action is SHA-pinned as in `test.yml`.
  - It is not a required check.

### `task -x` everywhere (contract §3, §9.9)
**Decision**:
- `test.sh`, both workflows and the `AGENTS.md` bullet invoke `task -x`.
- `test.sh` asserts exit codes from `task -x deps:cascade` only.

## Supervisor choices carried from the contract

These are recorded as contract §9 states them. The owner may override any of them.

- §9.1: the title and body are implemented in the resolver; this repo only wraps them.
- §9.3: prereleases count only when the current pin is a prerelease. Library and the opm CLI are
  beta lines; the catalog is stable.
- §9.4: versions are `v`-prefixed everywhere except the two bare stores.
- §9.6: fixture consumers follow in the same PR.
- §9.7: the Phase 2 gate is met through a tier-2 catch-up PR.
- §9.8: `deps-cascade:breaking` is computed in Phase 3.
- §9.9: `task -x` is mandatory.
- §9.10: core never moves backwards to match a catalog. This is reported to the owner.
- §9.11: a hold on core holds the catalog too.
- §9.12: the tests are split into an offline set and a network set.

## Risks / Trade-offs

- **[Catalog `v4.5.x` may not render the fixtures]** → Out of scope here: S2 restores the tree's
  own versions, not the newest. The catch-up PR is where a break shows, and it may need hand
  fixes (contract §8).
- **[Fixture publish on merge]** → A cascade PR that advances fixtures makes
  `publish-fixtures.yml` (`:20-29`) publish the new versions to GHCR when it merges. This is
  intended and is how every fixture bump works today.
- **[`go mod tidy` raising a third-party pin]** → The task warns, not reverts (rule 6). The PR's
  CI shows any break.
- **[Stub vs real resolver]** → The stub does not apply holds inside `newest` and has no title or
  body (contract §7). Holds and title/body are tested once in `.github`, and S5 covers this
  repo's title and body against the real resolver.
- **[Every task runs two global `go list -m` vars]** → `Taskfile.yml:19-28` (`ENVTEST_VERSION`,
  `ENVTEST_K8S_VERSION`) run for every task, including `deps:*`. With an empty module cache and
  `GOPROXY=off`, even `task --dry deps:release-check` fails. The "offline" set therefore needs a
  warm Go module cache or the proxy; in CI it runs after setup-go's cache restore, which holds in
  `Lint`.
- **[`yq` on a developer machine]** → The stub needs mikefarah `yq` v4 only when a `.cascade-*`
  file exists. The operator has none, so only S4 needs it. `test.sh` checks `yq --version` up
  front and fails naming the tool.

## Open Questions

- **Docs prose drifts.** `docs/site/start/install-the-operator.md:35,87,190` prints `4.4.4` and
  `v2.0.0-beta.1` as example output. The contract gives the operator no warning for it (library
  has a similar one for `docs/getting-started.md`, contract §6.2 step 5). Proposed: leave it out
  of the task. Should a `-` warning be added? This is for the supervisor.
  (`docs/site/reference/operator-resources.md:329` is generated, not prose; the task regenerates
  it.)
- **A `.cascade-frozen` for the operator?** The two `4.0.0` catalog literals in "Never touched"
  are frozen pins in RELEASING.md's sense but have no entry. Should opm-operator ship a
  `.cascade-frozen` listing them (and RELEASING.md "Pin classes" gain an operator frozen row)?
  This change does not create one. For the supervisor.
- **The provider identity comment.** It says the catalog version is "Hand-managed"
  (`test/fixtures/catalogs/provider/identity/identity.cue:12-15`). After this change the cascade
  advances it too, through the same `opm catalog version set`. Editing that comment changes the
  fixture and forces a republish (`0.1.0` to `0.1.1`) in this PR. Proposed: leave the comment,
  which is still true about how a bump is made, and correct it with the next real provider bump.

## Plan review

The plan review of 2026-10-04 raised 14 findings. Applied: 1 (regenerate the resource
reference), 2 (its class; reported as a contract conflict), 3 (`deps:cascade:test` without the
resolver precondition; reported as a contract §3 deviation), 4 (offline step in `Lint`; reported
as a contract §8 conflict with RELEASING.md), 5 (`if`/`case` for every resolver call, the `EXIT`
trap, S3b), 6 (sandbox git identity), 7 (phase B decides from `f_changed` too), 8 (the two `4.0.0`
literals listed, `.cascade-frozen` asked), 9 (setters checked at cli `v1.0.0-beta.4` too), 10
(global `go list` vars in Risks), 11 (`C = max(file catalog, K)`), 12 (docs-kit refs in the
spec), 14 (line reference).

Rejected: 13 (a tasks.md item that opens the PR). The repo's `openspec/config.yaml` allows no
delivery operation in tasks.md besides the section commit, and PRs are opened by the supervisor.
The PR title, `ci(cascade): add the deps:cascade tasks` (contract §10), is recorded in the
proposal instead.
