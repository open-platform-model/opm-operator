# Design — fixtures-testing-domain

## Research & Decisions

### D1: Relocate to `testing.opmodel.dev/modules/operator/<name>` — supersedes `add-example-test-modules` D1

**Context.** `2026-06-17-add-example-test-modules` D1 moved every fixture *onto* `opmodel.dev/modules/test/<m>`,
rejecting the alternative of keeping them on `testing.opmodel.dev`. Its reasoning, verbatim: *"two path
conventions is more confusing for a 'getting started' surface, and the migration is mechanical"*,
supported by *"the example modules' public path and their registry are therefore the same decision"*.

**Explored.** That argument conflates the path with the registry. They are independent: a
`testing.opmodel.dev` path published to GHCR resolves for any consumer carrying the canonical mapping,
which every OPM consumer already does — the operator's own compiled-in `--registry` default routes both
domains to GHCR, and `module-instance-synthesis` already specifies that. So the "no extra config"
property D1 was protecting survives the move intact. What D1 could not weigh, because it had not
happened yet, is the cost measured since: 366 `-e2e.g<sha>` tags accumulated in the production
namespace, the whole `opmodel.dev` prefix pinned to localhost in every local flow because of
longest-prefix routing, and `opm module publish` — which did not exist in June — refusing the nested
path outright.

**Decision.** Fixtures move to `testing.opmodel.dev/modules/operator/<name>`. The owning-repo segment
follows the `cli` precedent (`testing.opmodel.dev/modules/cli/podinfo`, landed 2026-08-18) so both
repos' `podinfo` coexist without a leaf collision.

**Rationale.** Two path conventions is exactly the point: production and fixture artifacts *should* be
distinguishable by path, because CUE's resolver treats the path as the routing key. The confusion D1
feared is cheaper than the coupling it created.

### D2: Modulepackages move too, deviating from enhancement 0011 D14/D17 item 5

**Context.** 0011 D14 places `opmodel.dev/releases/*` outside the namespace migration — *"they are not
in the namespace D5 partitions… OCI repository paths that share a spelling convention with module
paths and nothing else"* — and D17 item 5 restates it as *"out of scope per D14; Flux artifacts, not
CUE modules"*. Root `CLAUDE.md` Registry Policy rule 4 puts Flux bundle artifacts outside the policy
altogether.

**Explored.** D14's reasoning is that these artifacts are not *in* the partitioned namespace, not that
their spelling is correct. Leaving them means fixture-shaped artifacts keep occupying
`opmodel.dev/releases/test/*` while the modules beside them move, and the four modulepackage
`cue.mod` files — which do look like publish coordinates even though nothing publishes them as CUE
modules — would keep a coordinate no longer matching anything. GHCR already carries a
`testing.opmodel.dev/releases/hello` from a prior hand-push, so the shape has precedent.

**Decision.** Modulepackages move to `testing.opmodel.dev/releases/operator/<name>`. **This is an
explicit deviation from 0011 D17 item 5** and is recorded as such in 0011's history, not silently
absorbed.

**Rationale.** The purpose is one rule a contributor can hold: nothing fixture-shaped lives under
`opmodel.dev`. A carve-out for artifacts that merely happen to be pushed by a different tool
reintroduces the ambiguity the move exists to remove. Cost is low — these have never been published to
GHCR, so no consumer is re-pointed.

### D3: Publish through `opm module publish`, not `cue mod publish`

**Context.** The repo has no dependency on the cli and publishes with raw `cue mod publish`, which runs
no gates.

**Explored.** Adopting `opm module publish` costs a CI dependency (`go install` from a pinned release)
and an identity package per fixture. Keeping `cue mod publish` costs nothing but leaves the fixtures the
only OPM artifacts nothing validates — and the relocation is precisely what unblocks the gated path.

**Decision.** Adopt it, pinned to `v1.0.0-alpha.11`. Verified: that released binary already carries
`gateNamespace`'s testing-domain exemption, so nothing waits on a new cli release.

**Rationale.** A fixture that violates a publish gate should fail CI rather than ship. This is the same
argument the cli made for its templates (0011 D25) and its own fixture.

### D4: The version contract moves from grep to the identity package

**Context.** `.tasks/examples.yaml` documented its `grep -oP '^\s*version:'` on `module.cue` as "the
stable contract", chosen because it needs no registry access or evaluation.

**Explored.** Under a derived metadata block there is no `version:` literal in `module.cue` at all. The
grep would match nothing and hit its `continue` branch — a **silent no-publish that still exits 0**, the
worst available failure mode.

**Decision.** Read with `cue eval ./identity --out text -e Version`, and make a missing identity package
or an unreadable version a hard error (`rc=1`), never a skip.

**Rationale.** The identity package is import-free by construction, so evaluating it needs no registry
access — it preserves the exact property the grep was chosen for, while being the artifact's actual
source of truth.

### D5: Prerelease publishing uses `opm module version set` on a staged copy

**Context.** A prerelease tag must be matched by the artifact's *declared* version or the library's
acquire-time identity check (0010 D11) refuses it. The old flow staged a copy and `sed`-rewrote the
first `version:` literal.

**Explored.** `opm module publish --version` looked like the natural replacement, but its contract is
"fill an *open* identity Version, or *assert* the declared one". Ours carries a default, so it asserts
and refuses the prerelease. `opm module version set` is the writer built for this: offline, surgical,
and it preserves the defaulted-disjunction assertion byte-for-byte.

**Decision.** Stage a copy, `opm module version set "${ver}-${prerelease}"`, then publish.

**Rationale.** Replaces a regex against a formatting convention with a structural writer that the cli
already tests. Verified end-to-end: every prerelease publish reports `identity Version concrete
(default) = <ver>-<prerelease>` matching its tag exactly.

### D6: The registry skip guard tests for a configured mapping, not a localhost one

**Context.** `skipIfNoTestRegistry` required `CUE_REGISTRY` to contain `opmodel.dev=localhost` or
`opmodel.dev/modules/test=localhost`.

**Explored.** The first pattern is a substring match that `testing.opmodel.dev=localhost` also
satisfies, so the guard already proved nothing about the fixture's own namespace. After the move it
inverts: the fixtures become publicly resolvable while the guard skips every registry-backed spec in
plain CI, claiming they are unavailable. A permanently-skipping test is worse than a slow one.

**Decision.** Require only that a mapping is configured. Keep the container-tool check, scoped to the
localhost case, and keep `OPM_TEST_REGISTRY_FORCE=1`.

**Rationale.** The fixtures are published; the specs should run wherever they resolve. Verified under
`OPM_TEST_REGISTRY_FORCE=1`: 41 specs pass with zero registry-mapping skips.

## Consequences

- No repo in the workspace needs a local registry for its tests; the root Registry Policy's known
  deviation closes on both sides.
- The registry-backed integration specs run in plain CI for the first time.
- The 377 tags under `opmodel.dev/modules/test/*` and the two pre-2026
  `testing.opmodel.dev/{modules,releases}/hello` leftovers are orphaned. Their deletion is a separate,
  irreversible step taken only after this change is green.
