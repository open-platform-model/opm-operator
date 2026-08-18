# Proposal — fixtures-testing-domain

## Why

The example test modules sit on `opmodel.dev/modules/test/<m>` — the production namespace — and their
modulepackages on `opmodel.dev/releases/test/<m>`. That placement was a deliberate decision
(`2026-06-17-add-example-test-modules` D1) taken before its cost was measurable. The cost is now
measured, in three places.

**The production namespace's tag space is 97% fixture churn.** Measured on GHCR 2026-08-18:

| package | total tags | `-e2e.g<sha>` | real releases |
| --- | --- | --- | --- |
| `opmodel.dev/modules/test/hello` | 114 | 111 | 3 |
| `opmodel.dev/modules/test/hello-web` (orphaned by the `hello_web` rename) | 81 | 78 | 2 |
| `opmodel.dev/modules/test/hello_web` | 34 | 33 | 1 |
| `opmodel.dev/modules/test/podinfo` | 115 | 111 | 4 |
| `opmodel.dev/modules/test/redis` | 114 | 111 | 3 |

377 tags, 366 of them per-commit e2e prereleases, alongside 40+ real first-party modules. The e2e
workflow mints one per fixture per push and nothing prunes.

**Longest-prefix routing makes four fixtures hostage the whole prefix.** CUE resolves by longest
prefix on the module path, so serving these fixtures from a local registry requires mapping *all* of
`opmodel.dev` there — which is why `make run`, `make publish-test-module` and `.tasks/module.yaml` all
carry an `opmodel.dev=localhost` mapping and why core and the catalogs have to be mirrored locally as
collateral. `CLAUDE.md`'s known deviation names this migration as its own exit condition.

**`opm module publish` refuses these paths outright.** `gateNamespace`'s `firstPartyShape` admits one
leaf segment under `opmodel.dev/(modules|catalogs|platforms|templates)/`; a three-segment
`modules/test/<m>` fails, and the refusal text says where fixtures belong: `testing.opmodel.dev`. The
fixtures publish today only because the tooling calls ungated `cue mod publish`. Nothing about them is
gate-checked.

Reachability is *not* the motivation — these have been anonymously pullable from GHCR for months.

## What Changes

- **Fixtures relocate** to `testing.opmodel.dev/modules/operator/<name>` and modulepackages to
  `testing.opmodel.dev/releases/operator/<name>`. Versions continue their lineage (hello 0.0.5,
  hello_web 0.1.3, podinfo 0.1.4, redis 0.1.7); a new coordinate is a fresh repository, so the first
  publish always succeeds.
- **Each fixture becomes a real publishable artifact**: a new `identity/identity.cue` is the single
  source of path and version, and `module.cue`'s `metadata` derives from it (0010 D38 / 0011 D12).
- **Publishing moves to `opm module publish`**, installed in CI from a pinned cli release, so every
  publish gate runs over the fixtures — the pipeline the official templates already use.
- **The grep version contract is retired.** `.tasks/examples.yaml` read the first `version:` literal
  out of `module.cue`; under a derived metadata block that literal does not exist, and the grep would
  have silently published nothing. Version now comes from `cue eval ./identity -e Version`, and a
  fixture without an identity package is a hard error rather than a silent skip.
- **The prerelease path stops being a `sed` rewrite.** It stages a copy and calls
  `opm module version set`, which is offline and surgical, so the declared version and the tag agree
  for the acquire-time identity check (0010 D11). `opm module publish --version` cannot serve here: it
  *asserts* a declared version rather than filling one, and ours carries a default.
- **The integration skip gate is fixed.** It tested `strings.Contains(reg, "opmodel.dev=localhost")`,
  which also matches `testing.opmodel.dev=localhost` — so it proved nothing about the fixture's
  namespace — and after the move it would have skipped the registry-backed specs in every CI run
  while claiming the fixtures were unavailable. The predicate is now "is a mapping configured",
  so those specs run against GHCR with no local registry.
- **Registry mappings gain the testing domain** where they omitted it: `release.yml`,
  `test-e2e.yml`, `.tasks/module.yaml`. Stale hardcoded publish versions (`Makefile`'s `v0.0.1`,
  `.tasks/module.yaml`'s `v0.0.2`, both drifted from the declared versions and failing the identity
  check) are replaced by the identity-derived version.
- **Deliberately NOT changed**: `internal/status/digests_test.go`'s golden pin — its
  `opmodel.dev/modules/test/podinfo@v0` is a frozen cross-repo test vector for the digest *formula*,
  not a live fixture reference, and its comment forbids unilateral change.

## Capabilities

### New Capabilities

<!-- none -->

### Modified Capabilities

- `example-test-modules`: fixtures are authored on the testing domain with an identity package and
  derived metadata; modulepackages follow.
- `example-module-publishing`: publishing runs through the gated `opm module publish` pipeline at the
  new coordinates.
- `test-registry-lifecycle`: no local registry is required; the skip guard tests for a configured
  mapping rather than a localhost one.

## Impact

**SemVer: MINOR.** Fixture identity is not a published API of the operator — the same framing
`add-example-test-modules` used for the inverse move. No CRD, controller, or user-facing behaviour
changes.

The workspace Registry Policy deviation closes on both sides: with `cli` already migrated, no repo
needs a local registry to run its tests.

`openspec/config.yaml`'s tasks rule forbids delivery operations as tasks **except** where publishing
is itself the deliverable. It is here, so the publish steps in `tasks.md` are in scope by that clause.
