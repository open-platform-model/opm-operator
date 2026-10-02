## Why

A fixture bump can leave a modulepackage on a stale core or catalog pin, and nothing fails. Each `test/fixtures/modulepackages/<m>/cue.mod/module.cue` pins its module plus core and catalogs/opm. CUE v0.17.1 keeps a dependency the consumer already lists at its listed version, so a modulepackage re-pinned to a new module version but left on an older core passes `cue mod tidy --check` and renders against the older core.

The documented manual path (`opm module version set`, then `task examples:pin`) has exactly that gap: `examples:pin` rewrites only the module's own `v:` line. Only the workspace `task deps:pins:fixtures` makes core and the catalogs follow the module. The same bug bit the cli in its PR 254.

This change closes the manual path and adds a check, `hack/fixtures.sh consumers` (byte-identical with the cli's), that catches the drift whatever produced it.

## What Changes

- **`hack/fixtures.sh consumers <dir>...`** (new subcommand, byte-identical with the cli): per consumer, in a scratch copy, `cue mod get <fixture>@<pinned version>` for every `testing.opmodel.dev` pin, then `cue mod tidy`, then a diff against the committed `module.cue`. A difference, any `cue` error, or a tracked `cue.mod` outside the fixtures dir that pins a fixture but is not listed prints a `FAIL` line and fails the run after every consumer was checked. `FIX=1` writes the resolved file back.
- **`task examples:consumers`**: runs it over every modulepackage, resolving through `CONSUMERS_CUE_REGISTRY` (GHCR by default).
- **`task examples:pin`**: after re-pinning the module version in a modulepackage, every other dep the modulepackage shares with the module (core, the catalogs) takes the module's pin.
- **`test.yml`**: a step after the seed runs `task examples:consumers` under the mixed mapping; `task dev:test:seeded` runs it too.
- **`AGENTS.md`**: the bump sentence says the core and catalog pins follow the module and that CI checks it.

Release class: none, before and after GA. Every commit is `test` or `ci`, which release-please hides; nothing in `dist/install.yaml` or the image changes. No API type, CRD, controller or reconcile behavior changes. Complexity (Principle VII): one shell subcommand shared with the cli, one task, one CI step; the alternative is a hand-checked invariant that has already failed once.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `example-test-modules`: modulepackage pins follow their module, `examples:pin` keeps them so, and PR CI checks it.

## Impact

- Files: `hack/fixtures.sh`, `.tasks/examples.yaml`, `.tasks/dev.yaml`, `.github/workflows/test.yml`, `AGENTS.md`, `openspec/specs/example-test-modules/spec.md` (on archive).
- Cross-repo: `hack/fixtures.sh` must stay byte-identical with the cli's (workspace `task fixtures:lint`); merge this PR and the cli's back to back. The window between merges breaks no CI (`fixtures:lint` is local).
- `test.yml` runs on push to `main` as well as on PRs, so the check runs on both.
- Cost: a few seconds in a job that already has the seeded registry, `cue` and a fresh `CUE_CACHE_DIR`.
- PR e2e (`test-e2e.yml`) calls `examples:pin PRERELEASE=<id>`; the follow step copies the module's pins there too, which are already equal on a tree that passes the check.
