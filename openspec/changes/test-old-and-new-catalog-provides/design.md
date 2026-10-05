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
- Make each spec prove which path answered it, so a library change that sends both catalogs down one path fails the spec instead of passing it twice.

**Non-Goals:**

- No change to `internal/controller` or to any fixture version.
- No new published fixture.
- No cluster (e2e) run. The claim check is fully covered by envtest plus the registry.

## Decisions

### D1. The new-core catalog is a pin-rewritten copy acquired from a directory

The old-core catalog is the published `backup` fixture `0.1.0` (core `v2.0.0-beta.1`), acquired from the registry exactly as acceptance acquires it. For the new-core catalog, the spec copies `test/fixtures/catalogs/backup` into `GinkgoT().TempDir()`, rewrites the `opmodel.dev/core@v2` dependency in its `cue.mod/module.cue` to `schema.DefaultSchemaVersion()`, and acquires it with `Kernel.AcquireCatalogFromDir`. It hands that catalog to the reconciler through a test `CatalogAcquirer` that returns it for the claim's coordinate and fails the spec for any other coordinate.

Rejected: a second published catalog fixture pinned to the current core. It would be a published artifact that exists only for this test. It would also need its own row in `hack/fixtures.sh` and `publish-fixtures.yml`, and a decision on whether the cascade moves its pin. And because published versions are immutable, every core move would make it another fixture version to maintain. Also rejected: bumping the `backup` fixture itself to a newer core. That would remove the only old-catalog case from the tree, and the claim, the e2e spec and the README would all re-pin for no gain.

A directory-acquired catalog is stamped in overlay mode, so `Requires()` reads the rewritten committed module file, the same input `pinsCoreBeforeProvides` reads on the registry route. Which path `Provides` takes depends only on that pin and on whether the field exists, so the two routes reach the same branch for the same pin. The registry route stays covered by the old-catalog spec.

### D2. Pin the copy to the library's default core, guarded against `ProvidesSince`

`schema.DefaultSchemaVersion()` is the core the generated platform pins (`platformmodule`), so the copy's core requirement equals the platform's resolved core and the 0015:D8 build-compatibility check accepts it. A literal `v2.0.0-beta.3` would also pass that check, because it trails the platform, but it would hardcode a version this repo does not own. The spec asserts `semver.Compare(schema.DefaultSchemaVersion(), "v"+schema.ProvidesSince) >= 0`, so a library whose default core predates the field fails loudly instead of silently testing the fold twice.

### D3. Each spec asserts which path answered

- Old: `Requires()["opmodel.dev/core@v2"]` compares below `"v"+schema.ProvidesSince`, and `cat.Package.LookupPath(schema.CatalogProvides).Exists()` is false.
- New: the pin compares at or above `ProvidesSince`, the field exists, and decoding it gives the same set `Provides()` returns.
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

**Context**: The change rests on two assumptions: core `v2.0.0-beta.4` evaluates the `backup` fixture (built on opm `v4.4.4`, which pins core `v2.0.0-beta.1`) and derives `provides`, and the library's directory route reads the rewritten pin.

**Explored**: On 2026-10-05, a copy of `test/fixtures/catalogs/backup` was exported with `cue export -e provides` against GHCR. Pinned to `v2.0.0-beta.4`, it gave `["opmodel.dev/catalogs/opm/traits/backup@v1alpha1"]`. Pinned to `v2.0.0-beta.1`, it gave `reference "provides" not found`. A throwaway Go probe on library `v1.0.0-beta.6` returned these results:
- registry `backup@0.1.0`: core `v2.0.0-beta.1`, field absent, `Provides()` = the backup trait, Source root set.
- `AcquireCatalogFromDir` on the unmodified copy: same as the registry route.
- `AcquireCatalogFromDir` on the copy pinned to `v2.0.0-beta.4`: field present, `Provides()` = the backup trait.

No `cue mod tidy` was needed after the rewrite.

**Decision**: D1 to D3 as above.

**Rationale**: Both assumptions hold, so section 1 needs no spike.

## Risks / Trade-offs

- **The old-catalog case depends on the `backup` fixture's core pin.** A future bump of that fixture to core `v2.0.0-beta.3` or later fails the old-catalog spec by design (D3), and the README says so. Once the library deletes the fold before GA, the old-catalog spec has to be deleted or rewritten. Its expected behaviour then changes from "accepted" to whatever the library decides for a catalog without the field.
- **Network.** Both specs resolve core and opm from the registry mapping, like every registry-backed spec. They skip without `CUE_REGISTRY`, and they fail under `OPM_TEST_REGISTRY_FORCE=1`.
- **Directory route against registry route.** The new-core spec does not fetch from a registry. D1 explains why the branch is the same. The library's own tests cover a registry-fetched catalog pinned at or after `ProvidesSince`.
