## Context

Line numbers are at `277ca18` (opm-operator#253). Re-check them before editing.

**The library package.** Library `v1.0.0-beta.6` (already pinned, opm-operator#248) ships `opm/k8s/inventory` (library#203):

- `Entry{Group, Kind, Namespace, Name, Version, Component}` has no struct tags.
- `NewEntry(*unstructured.Unstructured) Entry` reads the GVK, namespace, name and the `labels.ComponentName` label.
- `SameObject` compares group, kind, namespace and name.
- `StaleSet(previous, current []Entry) []Entry` keeps previous order, returns non-nil empty, and is component- and version-blind.
- `Digest([]Entry) string` hashes the tag line `opm-inventory-v1\n`, then each entry sorted by Group, Kind, Namespace, Name, Component, Version, with each field length-prefixed in a fixed order.
- `RenderDigest([]object.Exported) (string, error)` re-decodes each object's JSON, blanks the managed-by value, re-encodes with sorted keys and hashes the tag line `opm-render-v1\n`, then the sorted objects. It errors only when an object's JSON is not a single JSON object.

The library's tests pin both encodings.

**The operator's copies.**

- `internal/inventory/entry.go`: `NewEntryFromResource`, `IdentityEqual` (component-aware, used by tests only) and `K8sIdentityEqual`.
- `internal/inventory/stale.go`: `ComputeStaleSet`, an O(n·m) scan with `K8sIdentityEqual`. Its outcome is the same as `StaleSet`'s: previous order, non-nil empty.
- `internal/inventory/digest.go`: `ComputeDigest`, sort, then `json.Marshal` of the CRD entries.
- `internal/status/digests.go:76-125`: `RenderDigest` over `[]object.Exported` (sorted JSON, managed-by included, no tag line) and `apiGroup`.

`internal/inventory/inventory.go` holds the `Current` alias, which the `inventory-bridge` spec requires.

**Readers.**

- ModuleInstance: `internal/reconcile/moduleinstance.go:452-453` (render and inventory digests), `:542` (stale set), `:615` (entries committed), `:769-780` (`nextInventory` digests the entries).
- ModulePackage: `internal/reconcile/modulepackage.go:643-648` (`computeModulePackageDigests`), `:675` (stale set), `:721` (entries committed).
- Conversion: `internal/reconcile/converted.go:47-57` (`convertRender`: one `object.Export`, `status.RenderDigest`, the apply copies, and `Resources = nil` in a defer).

The entries come from `internal/render/kernel_module_renderer.go:240` → `internal/render/module.go:79-89` (`buildInventoryEntries`: `ToUnstructured` per resource, the second export). `internal/apply/prune.go:55` names `ComputeStaleSet` in a comment.

**Render skip (opm-operator#246).** A reconcile skips its render when the render input key (source, config, platform package identity, skew policy, operator version, library version) matches `status.lastAppliedInputs` and the other `maySkip` conditions hold. The key holds no stored digest. `render-input-key` already states the rule this change relies on: a change to how a stored digest is computed ships with an operator or library version change, so the first reconcile after the upgrade renders, finds the stored digests out of date and applies once. `version.Full()` carries `version.Version` and the VCS revision when the build has one. A release image builds without `.git`, so the release's `version.Version` bump is what moves the key.

**Phases touched.** Render: the result loses its entries. Conversion (inside the slot): it builds entries and both digests from the one export. Plan: stale set. Status: inventory digest and render digest values. Source, Apply and Prune behaviour are unchanged.

## Goals / Non-Goals

**Goals:**

- The operator computes its stale set, inventory digest and render digest only through `opm/k8s/inventory` and keeps no copy (0012:D6, 0012:D7, 0012:D3:R6 for this package).
- One CUE export per rendered resource per reconcile.
- The stored digests change exactly once. The upgrade costs one apply per object and then converges, and a test pins that.

**Non-Goals:**

- The cli's adoption.
- `opm/k8s/ownership`, `opm/k8s/health` and `opm/k8s/lifecycle` adoption.
- Any CRD, status-field, condition or reason change.
- A lint rule for this package (D5).
- Claiming 0012 decisions in the delivery log (proposal, Impact).

## Research & Decisions

### D1. The API entry stays the wire shape; two conversions sit at the boundary

**Context**: `inventory.Entry` carries no struct tags on purpose: the library owns no wire shape. The status stores `releasesv1alpha1.InventoryEntry` (JSON keys `group`, `kind`, `namespace`, `name`, `v`, `component`), and `apply.Prune` takes that type.
**Explored**: (a) Change `apply.Prune` and the status code to the library type: this means more churn, and the CRD type still has to be produced for status. (b) Keep API entries everywhere and convert at the two library calls.
**Decision**: (b). `internal/inventory` gains `ToEntry(releasesv1alpha1.InventoryEntry) inventory.Entry` and `FromEntry(inventory.Entry) releasesv1alpha1.InventoryEntry`, plus slice forms (`ToEntries`, `FromEntries`; a nil slice stays nil). The six fields map one to one, so the round trip is lossless both ways, pinned by a test. The reconcilers import the library package as `k8sinventory` beside the operator's `internal/inventory`.

```go
// converted.go (sketch)
exported, err := object.Export(result.Resources)
result.Resources = nil // the build is released here; everything below reads exported data
...
digest, err := k8sinventory.RenderDigest(exported)
entries := make([]releasesv1alpha1.InventoryEntry, len(exported))
for i := range exported {
	resources[i] = exported[i].Object
	entries[i] = inventory.FromEntry(k8sinventory.NewEntry(exported[i].Object))
}
return &convertedRender{result: result, digest: digest, resources: resources, entries: entries}, nil

// moduleinstance.go
digests.Inventory = k8sinventory.Digest(inventory.ToEntries(converted.entries))
stale := inventory.FromEntries(k8sinventory.StaleSet(inventory.ToEntries(previousEntries), inventory.ToEntries(converted.entries)))
```

**Rationale**: This is the smallest change that keeps the CRD type the only wire shape. The conversions are the operator's half of "each frontend maps it to its own CRD fields" (the library package doc).

### D2. Entries come from the one export, built after the CUE values are released

**Context**: opm-operator#253 left `buildInventoryEntries` exporting every resource a second time inside `resultFromRender`. The render slot is held across the render and the conversion, and the export is where the heap peaks (a library-side memory measurement puts cert-manager's peak in this conversion; `converted.go` says the same).
**Decision**: `RenderResult.InventoryEntries` and `buildInventoryEntries` are deleted. `convertRender` builds the entries from each `Exported.Object` of the full rendered set (before `withholdRefused` splits the apply list) and carries them on `convertedRender.entries`. It drops `result.Resources` right after `object.Export` returns, before it computes the digest and the entries. The defer stays for the failure exits. Both digests and the entries then read exported data only: `RenderDigest` reads `Exported.JSON`, and `NewEntry` reads `Exported.Object`.
**Rationale**: One export per resource is what the memory window was sized for. The exported object holds the same apiVersion, kind, namespace, name and labels as `ToUnstructured` of the same value (both decode the same JSON), so the entries are identical to today's. Section 1 keeps the old digest functions so that this equality is checked on unchanged digests.

### D3. The withhold invariant moves from a stub shape to the conversion

**Context**: `test/integration/reconcile/withhold_invariant_test.go` (the spike from the shrink-refusal change) builds a stub `RenderResult` whose `Resources` leave out B while its `InventoryEntries` include B. It then shows that the committed inventory and the stale set keep B. Once the entries derive from the converted resources, a renderer can no longer return that shape: the property moves into the code. `withholdRefused` takes the converted set and returns a subset as the apply list, and the entries are built from the full set before it runs. The refusal's own path is pinned in `shrink_refusal_test.go` ("keeps the claim in the inventory and unpruned across a refusal"). There the reconcile returns before both the inventory commit and the prune.
**Decision**: Delete `withhold_invariant_test.go`. Add a unit test in `internal/reconcile`: when a decider refuses one of N converted resources, `convertRender` still yields N entries for the full set, and `withholdRefused` returns N-1 apply objects without touching them. Update the pointers to the deleted test in `internal/reconcile/withhold.go:57` and `test/integration/reconcile/shrink_refusal_test.go:175`.
**Rationale**: The old test pinned the assumption that "the inventory is built from the full rendered set". After this change that assumption is a single line in `convertRender`, and a unit test pins it directly. A stub that fakes an impossible renderer output would pin nothing real.

### D4. The upgrade renders, applies once, then converges, for both kinds

**Context**: Both stored digests change value. `IsNoOp` compares all four digests, so the first post-upgrade render is never a no-op. The render skip compares only the input key, which moves with the operator version (opm-operator#246). The one-time digest change must not confuse the skip: it must neither hide the apply behind a skip nor leave a loop of applies.
**Decision**: Add one controller test per kind (`internal/controller/render_skip_test.go`):
1. Apply once.
2. Overwrite the stored `status.inventory.digest` with the old encoding's value for the same entries, and `lastAppliedRenderDigest` with the old encoding's value for the same render. The old encoding is computed by a test-local copy of the deleted functions, kept in the test file and named as the pre-upgrade encoding. Also write a `lastAppliedInputs` key recorded under operator `v1.0.0-test`.
3. Reconcile with operator `v1.0.1-test`. It renders exactly once and applies: the history grows by one, `status.inventory.revision` advances, and the stored digests now equal `k8sinventory.Digest`/`RenderDigest` of the same entries and export.
4. Reconcile again under `v1.0.1-test`. The render is skipped and nothing is patched.
5. Move `renderedAt` past the interval and reconcile. It renders, and the outcome is `NoOp`, so no second apply happens.

A further case pins the documented boundary: with the old digests and a key recorded under the same operator version, the reconcile skips (the key holds no digest). Once the interval has passed, it renders and applies once.
**Rationale**: This is the whole interaction, stated as the `render-input-key` rule already promises. The boundary case documents why the release's version bump matters, without adding a digest to the key.

### D5. No lint rule for the inventory copy

**Context**: opm-operator#253's depguard rule refuses the old `pkg/core` path. Here `internal/inventory` stays as the conversion boundary, so a path rule cannot refuse a copy put back into it.
**Decision**: No lint rule. The `inventory-bridge` requirements say the package declares no stale-set, identity or digest function, and review keeps it that way.
**Rationale**: Principle VII. A source-scanning test for function names would catch a copy only under the names it guesses.

### D6. Render digest failure keeps the render-failure reason

**Decision**: An error from `k8sinventory.RenderDigest` maps to `RenderFailedReason` with the step "computing render digest", the reason and step an export failure already uses. It cannot fire after a successful `object.Export`, because Export only returns objects it decoded from that JSON. It is still handled rather than ignored.

## Risks / Trade-offs

- **One apply per object after the upgrade.** The renders are bounded by `--max-concurrent-renders`, and SSA leaves the objects unchanged. The migration note says so. A rollback costs one more apply.
- **Mixed frontends.** Until the cli's adoption ships, the cli and the operator digest one render differently. They already do today, so nothing regresses.
- **A dev image without a version bump keeps its key.** Such an image applies the digest change only after the drift interval (D4's boundary case). Release images always bump `version.Version`.
- **Memory.** The re-measurement is a library-side simulation (the probe mirrors the operator's path and does not import it). It shows the direction of the change, not an operator RSS figure.

## Open Questions

- Release pairing: this operator release should ship with the operator's `opm/k8s/ownership` and `opm/k8s/health` adoptions if they are ready, so the apply burst happens once. That is a release-timing choice and does not gate this change.
- An operator `feat!` release turns the `module/operator-image` PR into `fix(deps)!` (AGENTS.md, "This repository releases two units").

## Sections

1. Inventory entries from the one export (D1, D2, D3). Digests keep their current bytes.
2. Stale set and digests from `opm/k8s/inventory`, copies deleted, upgrade pinned (D4, D5, D6). Stored digests change once.
3. Verification: e2e on Kind under the lock with `LOCAL_REGISTRY`, memory re-measurement, docs bundle, OpenSpec validation.
