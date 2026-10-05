## Context

`catalog.Catalog.Provides` (library v1.0.0-beta.6, library#195) picks its path from the catalog's committed core pin:

```go
old, err := c.pinsCoreBeforeProvides()       // Requires()["opmodel.dev/core@v2"] < schema.ProvidesSince ("2.0.0-beta.3")
field := c.Package.LookupPath(schema.CatalogProvides)
if old || !field.Exists() {
	return c.providesFold()                   // deprecated Go walk of #transformers
}
// decode field, sort, deduplicate
```

Acceptance (`TransformerRegistrationReconciler.Reconcile`) acquires the claimed catalog through its `CatalogAcquirer` and calls `providesDrift(cat, claim.Spec.Provides)`, which calls `Provides()` and refuses `ProvidesMismatch` on any difference. Reconcile phase impact: none. The reconciler is unchanged; this change only adds integration coverage of its acceptance verdict.

## Goals / Non-Goals

**Goals:**

- Prove that acceptance accepts the `backup_provider` claim when the catalog is answered by the fold (old core pin) and when it is answered by the decoded field (core at or after `ProvidesSince`).
- Pin each path's premises in the spec that relies on them: the core pin and whether the `provides` field exists. A fixture move or a library change to `ProvidesSince` then fails loudly, naming the premise, instead of quietly testing one path twice. A library that reads the field for a catalog pinned before `ProvidesSince` fails the old spec, because that catalog has no field.
- Not a goal, because it cannot be observed from outside the library: telling the fold from the field on a catalog pinned at or after `ProvidesSince`. On a real core both give the same set, so a library that always folded still passes the new spec. The library's own tests (library#195) cover that direction.

**Non-Goals:**

- No change to `internal/controller` or to any fixture version.
- No new fixture, published or in-tree.
- No cluster (e2e) run. The claim check is fully covered by envtest plus the registry.

## Decisions

### D1. The new-core catalog is a pin-rewritten copy acquired from a directory

The old-core catalog is the `backup` fixture `0.1.0` (core `v2.0.0-beta.1`), acquired from the registry exactly as acceptance acquires it: in PR CI that is the registry seeded from this tree, elsewhere GHCR. For the new-core catalog, the spec copies `test/fixtures/catalogs/backup` into `GinkgoT().TempDir()`, rewrites the `opmodel.dev/core@v2` dependency in its `cue.mod/module.cue` to `"v"+schema.ProvidesSince` (D2), and acquires it with `Kernel.AcquireCatalogFromDir`. It hands that catalog to the reconciler through a test `CatalogAcquirer` that returns it for the claim's coordinate and fails the spec for any other coordinate.

This is a departure from using the tree-seeded registry for both catalogs: the new-core half does not touch a registry for the catalog itself (core and opm still resolve through the registry mapping).

Rejected: a second in-tree catalog fixture, with its own module path and core pinned to `v2.0.0-beta.3`. `hack/fixtures.sh` would pick it up without a new row (`fixture_dirs` enumerates `test/fixtures/catalogs/*`), `publish-fixtures.yml` already filters on `test/fixtures/catalogs/**`, and the cascade would leave its pin alone as it leaves `backup`'s, since among catalogs it plans only `test/fixtures/catalogs/provider`. A pin at `v2.0.0-beta.3` would also stay valid under a newer platform core, because build compatibility refuses only a catalog ahead of the platform. Its real costs are two. It is one more GHCR artifact that exists only for this test. And a claim for it is a second catalog path: `backup_provider` renders a claim naming `backup`, so the spec would patch the claim's `catalog` and `provides`, or the change would add a second provider module. Until it is published to GHCR, a developer run of `task dev:test` cannot resolve it either. The directory copy reaches the same library branch at none of these costs. Also rejected: bumping the `backup` fixture itself to a newer core. That would remove the only old-catalog case from the tree, and the claim, the e2e spec and the README would all re-pin for no gain.

A directory-acquired catalog is stamped in overlay mode, so `Requires()` reads the rewritten committed module file, the same input `pinsCoreBeforeProvides` reads on the registry route. Which path `Provides` takes depends only on that pin and on whether the field exists, so the two routes reach the same branch for the same pin. The registry route stays covered by the old-catalog spec.

### D2. Pin the copy to exactly `ProvidesSince`

The copy's core pin is `"v"+schema.ProvidesSince`, the first core that derives `provides`. The library chooses the fold when the pin compares below `ProvidesSince` (`cmp < 0`), so a pin at exactly that version is the boundary case. What the spec can observe there is the library's claim about core: it asserts that a catalog pinned to exactly `ProvidesSince` carries the field, so a `ProvidesSince` set to a core that does not derive `provides` fails here. An off-by-one in the comparison itself (`<=`) would fold this catalog and give the same set, which the spec cannot see (D3). The spec asserts that the copy's pin compares equal to `ProvidesSince`. `ProvidesSince` is library-owned, so the version follows the library rather than being hardcoded here. It trails the platform's core (`schema.DefaultSchemaVersion()`, `v2.0.0-beta.4` on library beta.6), and 0015:D8 build compatibility accepts a catalog that trails the platform. The spec also asserts that the generated platform's core, read from its module file, is not older than the copy's, since a platform behind the catalog would be refused for a reason unrelated to `provides`.

The pin does not move with library core bumps. The copy still depends on opm `v4.4.4`, built against core `v2.0.0-beta.1`, and a fixed core pin keeps a later breaking core release from failing this spec for a reason unrelated to `provides`.

### D3. Each spec asserts which path answered

- Old: `Requires()["opmodel.dev/core@v2"]` compares below `"v"+schema.ProvidesSince`, and `cat.Package.LookupPath(schema.CatalogProvides).Exists()` is false.
- New: the pin compares equal to `ProvidesSince`, the field exists, and decoding it gives the same set `Provides()` returns.

These are the facts that select each path, not an observation of which branch ran. They catch a fixture or library-default move, and a library that reads the field for an old catalog. They cannot catch a library that folds a new catalog, because the fold and the field agree on a real core.
- Both: `Provides()` is exactly `opmodel.dev/catalogs/opm/traits/backup@v1alpha1`, and the rendered claim's `spec.provides` equals it.

### D4. Run the real reconciler, not `providesDrift` alone

`providesDrift` is unexported in `internal/controller`, and the integration package cannot call it. Running `TransformerRegistrationReconciler.Reconcile` directly (no manager) covers the whole claim check that `providesDrift` sits inside. That check covers catalog acquisition, the provider identity check, build compatibility, duplicate and contract-holder checks, and the verdict patch. An `Accepted` verdict is the observable proof that `providesDrift` passed, since a mismatch refuses with `ProvidesMismatch` before any later check. The spec builds what the reconciler reads:

```go
store := generatedPlatformStore(k, registry)          // platform at the default core, backup not subscribed
r := &controller.TransformerRegistrationReconciler{
	Client: k8sClient, Scheme: k8sClient.Scheme(),
	EventRecorder: events.NewFakeRecorder(16),
	Catalogs: acquirer,                                // kernel (old) or dirCatalogs (new)
	Store:    store,
}
// claim: the unstructured backup_provider render, created as-is
// provider: ModuleInstance default/backup-provider with status.inventory owning the claim
_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: claim.GetName()}})
```

The claim is cluster-scoped and both specs use the same rendered name (`default.backup-provider`) and catalog. Each spec therefore removes its claim (finalizer stripped, bounded wait until gone) and its provider instance in `DeferCleanup`, so the second spec is not refused `DuplicateClaim` by the first one's leftover.

## Research & Decisions

### Does a newer core derive `provides` for this fixture, and does the library take each path?

**Context**: The change rests on two assumptions: a core at or after `ProvidesSince` evaluates the `backup` fixture (built on opm `v4.4.4`, which pins core `v2.0.0-beta.1`) and derives `provides`, and the library's directory route reads the rewritten pin.

**Explored**: On 2026-10-05, a copy of `test/fixtures/catalogs/backup` was exported with `cue export -e provides` against GHCR. Pinned to `v2.0.0-beta.4`, it gave `["opmodel.dev/catalogs/opm/traits/backup@v1alpha1"]`. Pinned to `v2.0.0-beta.1`, it gave `reference "provides" not found`. On 2026-10-05, after the plan review moved the pin to `ProvidesSince`, the same export pinned to `v2.0.0-beta.3` (cue v0.17.1) gave `["opmodel.dev/catalogs/opm/traits/backup@v1alpha1"]`. A throwaway Go probe on library `v1.0.0-beta.6` returned these results:
- registry `backup@0.1.0`: core `v2.0.0-beta.1`, field absent, `Provides()` = the backup trait, Source root set.
- `AcquireCatalogFromDir` on the unmodified copy: same as the registry route.
- `AcquireCatalogFromDir` on the copy pinned to `v2.0.0-beta.4`: field present, `Provides()` = the backup trait.

No `cue mod tidy` was needed after the rewrite.

**Decision**: D1 to D3 as above.

**Rationale**: Both assumptions hold, so section 1 needs no spike.

## Risks / Trade-offs

- **The old-catalog case depends on the `backup` fixture's core pin.** A future bump of that fixture to core `v2.0.0-beta.3` or later fails the old-catalog spec by design (D3), and the README says so. Once the library deletes the fold before GA, the old-catalog spec has to be deleted or rewritten. Its expected behaviour then changes from "accepted" to whatever the library decides for a catalog without the field.
- **Network.** Both specs resolve core and opm from the registry mapping, like every registry-backed spec. They skip without `CUE_REGISTRY`, and they fail under `OPM_TEST_REGISTRY_FORCE=1`.
- **Directory route against registry route.** The new-core spec does not fetch the catalog from a registry. D1 explains why the branch is the same. The library's own tests cover a registry-fetched catalog pinned at or after `ProvidesSince`.
- **Fold on a new catalog goes unseen.** See D3: the specs cannot tell the two paths apart when both are valid; the library's tests own that.
