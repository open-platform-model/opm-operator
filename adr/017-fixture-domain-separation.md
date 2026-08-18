# ADR-017: Fixtures Live on a Separate CUE Module Domain

## Status

Accepted

## Context

ADR-008 established a naming taxonomy across the operator's naming layers and observed that "all of
these use the `opmodel.dev` domain, making it easy to conflate them." Its fifth rule keeps CUE module
paths separate from Kubernetes CRD naming, but it draws no line *within* the CUE module namespace:
production modules and test fixtures shared one domain.

The example test modules were placed under `opmodel.dev/modules/test/<m>` in June 2026, deliberately,
so that "the example modules' public path and their registry are therefore the same decision" and a
consumer would need no extra registry configuration. That reasoning conflated two independent things —
the path a module declares and the registry a mapping routes it to.

Three costs surfaced afterwards.

CUE resolves modules by **longest-prefix match on the module path**. A fixture under
`opmodel.dev/modules/test/` can only be served from a local registry by mapping *all* of `opmodel.dev`
there, which drags core and the catalogs along as collateral and forces them to be mirrored locally.
Every local development flow in this repository carried such a mapping for exactly this reason.

The e2e workflow publishes a per-commit `-e2e.g<sha>` pre-release tag for every fixture on every push.
Measured on GHCR on 2026-08-18, the five `opmodel.dev/modules/test/*` packages held 377 tags, of which
366 were per-commit e2e churn — accumulating in the same namespace as more than forty first-party
production modules.

The publish pipeline that arrived later refuses these paths outright. Its namespace gate admits exactly
one leaf segment under `opmodel.dev/(modules|catalogs|platforms|templates)/`, so a three-segment
`modules/test/<m>` fails, and its refusal text names the intended home: fixtures belong under
`testing.opmodel.dev`. The fixtures published only because the tooling called an ungated command, which
meant nothing validated them.

## Decision

Test fixtures are authored on a separate CUE module domain from production artifacts.

The operator's fixtures live under `testing.opmodel.dev/modules/operator/<name>` and their
modulepackage fixtures under `testing.opmodel.dev/releases/operator/<name>`. The owning-repo segment
(`operator`, alongside the cli's `cli`) keeps two repositories' identically-named fixtures from
colliding on a leaf.

Nothing fixture-shaped is authored under `opmodel.dev/*`. This extends ADR-008's taxonomy with a
distinction it did not draw: **the domain, not a path segment, separates production artifacts from
fixtures**, because the domain is the unit CUE's resolver routes on.

The domain is independent of the registry. `testing.opmodel.dev` is published to
`ghcr.io/open-platform-model` like everything else, and the canonical registry mapping routes both
domains there, so the "no extra configuration" property the original placement protected is preserved.
Pointing the fixture domain at a local registry becomes a one-entry override for iterating on an
unpublished fixture, rather than a precondition for running the tests.

The alternative — keeping one domain and relying on a `test/` path segment — was the status quo and is
rejected: a segment is invisible to longest-prefix routing, which is the mechanism that produced every
cost above.

## Consequences

**Positive:** No local registry is required to run any test tier; the fixtures and their dependencies
resolve from GHCR, and the registry-backed integration specs run in ordinary CI for the first time. The
fixtures become publishable through the gated pipeline, so a malformed fixture fails CI instead of
shipping. The production namespace stops accumulating per-commit e2e tags.

**Negative:** Two path conventions now exist, which is the confusion the original decision sought to
avoid. It is mitigated by the domain being self-describing and by the publish gate refusing the wrong
one mechanically, but a contributor must know which domain a new artifact belongs to.

**Trade-off:** Fixture identity is not a published API, but the relocation still orphans every
previously published artifact under `opmodel.dev/modules/test/*`. Those tags are left in place by this
decision; their deletion is a separate, deliberate act.

**Trade-off:** The modulepackage fixtures move too, even though they are Flux OCI artifacts rather than
CUE modules and a cross-repo decision record places that path class outside the namespace migration.
The deviation is deliberate and recorded upstream: the value of the rule lies in having no exceptions
to remember.

## Related

- ADR-008 — Naming Taxonomy. This ADR extends it; ADR-008 is not superseded.
- ADR-006 — Native CUE OCI Artifacts. Defines the artifact format being published.
- ADR-013 — OCIRepository as Sole Source Type. Governs the modulepackage fixtures' consumer side.
