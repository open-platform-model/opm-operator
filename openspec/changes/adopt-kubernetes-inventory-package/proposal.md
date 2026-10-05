## Why

Library `v1.0.0-beta.6` ships `opm/k8s/inventory` (library#203): one `Entry`, one component-blind `StaleSet`, the inventory `Digest` and the render `RenderDigest`, so the cli and the operator compare and digest an inventory with the same code (0012:D6, 0012:D7, library ADR-011). The operator still carries its own copies: `internal/inventory/{entry,stale,digest}.go` (an entry constructor, two identity relations, a stale set and an inventory digest over the CRD entry type) and `status.RenderDigest` in `internal/status/digests.go`. Two of those copies disagree with the cli's today, which is the defect 0012 removes:

- The inventory digest hashes `json.Marshal` of the CRD entry type, so it depends on the type's JSON tags (`omitempty` on `group`, `namespace`, `v` and `component`). The cli's struct carries other tags, so a core-group or cluster-scoped entry digests differently in each frontend. 0012:D7 replaces it with a canonical field-by-field encoding.
- The render digest hashes each object's JSON with the `app.kubernetes.io/managed-by` value in it. The operator stamps `opm-controller` and the cli `opm-cli`, so one render never digests alike in both. 0012:D6 leaves that one value out.

The owner decided that both frontends change their stored digests once, with a migration note (0012:D7:R4). This change is the operator half; the cli adopts the same package in its own change.

The change also removes the second export of every rendered resource. opm-operator#253 moved the digest and the apply copies onto one `object.Export`, but `resultFromRender` still runs `ToUnstructured` on every resource for the inventory entries (`internal/render/module.go` `buildInventoryEntries`). So each resource is exported twice inside the render slot, and the export is where the heap peaks. With the library's `NewEntry`, the entries come from the objects the one export already decoded.

## What Changes

- **Inventory entries come from the one export.** `convertRender` builds the inventory entries from the exported objects (`inventory.NewEntry` over each `Exported.Object`, the full rendered set, before shrink withholding). `RenderResult.InventoryEntries` and `buildInventoryEntries` are deleted, so a render exports each resource from CUE once. The rendered CUE values are dropped right after the export, and the digests and entries read only the exported data.
- **The stale set is the library's.** Both reconcilers call `inventory.StaleSet`. It is component-blind like the operator's own (0012:D7:R1), so no prune decision changes.
- **The inventory digest is the library's.** `status.inventory.digest` is `inventory.Digest` over the entries: a versioned canonical encoding, independent of entry order and of any JSON tag (0012:D7:R2/R3). The stored value changes once.
- **The render digest is the library's.** `lastAttemptedRenderDigest` and `lastAppliedRenderDigest` (and the render digest in new history entries) are `inventory.RenderDigest` over the one export. It leaves out the managed-by label value, so the cli and the operator digest one render alike (0012:D6:R2). The stored value changes once.
- **Copies deleted.** `internal/inventory/digest.go`, `entry.go` and `stale.go` and their tests go (`NewEntryFromResource`, `IdentityEqual`, `K8sIdentityEqual`, `ComputeStaleSet`, `ComputeDigest`), and so do `status.RenderDigest`, its `apiGroup` helper and its golden test (the library now owns and pins the bytes). `internal/inventory` keeps the `Current` alias and gains the two lossless conversions between the API `InventoryEntry` and `inventory.Entry`. `DigestSet`, `ConfigDigest`, `ModuleSourceDigest`, `IsNoOp` and `RenderInputKey` stay.
- **The upgrade applies once and then converges.** The first reconcile after the upgrade renders, because the operator version is part of the render input key (opm-operator#246). It finds the stored digests out of date and applies once. The next reconcile skips the render, or is a `NoOp` when it renders. A test pins this for both a ModuleInstance and a ModulePackage, starting from digests written in the old encoding.

Out of scope: the cli's adoption (its own change); the apply guard (`opm/k8s/ownership`), health (`opm/k8s/health`) and the deletion protocol (`opm/k8s/lifecycle`), each its own adoption change; any CRD or status field change.

## Migration note

The PR body carries the paragraph below. The squash commit body carries the plain block after it, unchanged, as its footer (a conventional-commit footer takes no markdown).

The operator now computes `status.inventory.digest`, `status.lastAppliedRenderDigest` and `status.lastAttemptedRenderDigest` with the library's `opm/k8s/inventory` (0012:D6, 0012:D7), so each stored value changes once. The inventory digest hashes a canonical field-by-field encoding of the entries instead of their JSON. The render digest no longer includes the value of the `app.kubernetes.io/managed-by` label. On its first reconcile after the upgrade, every operator-managed ModuleInstance and ModulePackage renders, finds its stored digests out of date, and applies once. Server-side apply is idempotent, so no object changes. `--max-concurrent-renders` bounds how many of these renders run at once. Each object gains one history entry and its `status.inventory.revision` advances once. The next reconcile is a no-op again. Each TransformerRegistration whose provider instance is operator-managed is judged once more when that provider's inventory digest moves; it re-acquires its catalog from the registry, outside the render slots, and its verdict does not change. A rendered value that cannot be exported is now reported by the reconciler's conversion instead of the renderer, so its first report changes: an export failure stays `RenderFailed` with the message "computing render digest", a decode failure is `ApplyFailed` with "converting resources", and neither emits a Render warning event. No CRD field and no prune decision changes. History entries written before the upgrade keep their old digests. CLI-owned instances are not reconciled by the operator, so their status is untouched. A tool that compares these digests with values it computes itself must compute them with `opm/k8s/inventory` from library `v1.0.0-beta.6` or later. Rolling back to an earlier operator applies every object once more, the same way.

```text
BREAKING CHANGE: the operator now computes status.inventory.digest, status.lastAppliedRenderDigest and status.lastAttemptedRenderDigest with the library's opm/k8s/inventory (0012:D6, 0012:D7), so each stored value changes once. The inventory digest hashes a canonical field-by-field encoding of the entries instead of their JSON; the render digest no longer includes the value of the app.kubernetes.io/managed-by label. On its first reconcile after the upgrade, every operator-managed ModuleInstance and ModulePackage renders, finds its stored digests out of date and applies once: server-side apply is idempotent, so no object changes, and --max-concurrent-renders bounds how many of these renders run at once. Each object gains one history entry and its status.inventory.revision advances once; the next reconcile is a no-op again. Each TransformerRegistration whose provider instance is operator-managed is judged once more when that provider's inventory digest moves, re-acquiring its catalog from the registry outside the render slots, with an unchanged verdict. A rendered value that cannot be exported is now reported by the reconciler's conversion instead of the renderer: an export failure stays RenderFailed ("computing render digest"), a decode failure is ApplyFailed ("converting resources"), and neither emits a Render warning event. No CRD field and no prune decision changes. History entries written before the upgrade keep their old digests, and CLI-owned instances are untouched. A tool that compares these digests with its own must compute them with opm/k8s/inventory from library v1.0.0-beta.6 or later. Rolling back to an earlier operator applies every object once more.
```

## Release notes (user-visible changes)

- One extra apply per operator-managed object after the upgrade (above).
- One CUE export per rendered resource instead of two. The memory effect is a library-side simulation (design.md, Risks), not a measured operator figure.

## Classification

The owner classified this as breaking (0012:D7:R4 asks for a migration note in the release that first records the new digest): MAJOR after GA. Before GA it ships as the next `-beta.N` under a `feat!:` title. Complexity (Principle VII): about 300 lines of operator code and tests go; the API-entry conversions and the tests that pin the upgrade are added.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `inventory-bridge`: the identity, stale-set, digest and entry-construction requirements over the operator's own copies are removed. Requirements are added for the library's stale set and inventory digest, for the conversions between the API entry and `inventory.Entry`, and for building the entries from the one export.
- `digest-computation`: "Render digest computation" (bytes pinned to the operator's old digest) is removed. "The render digest is the library's" is added.
- `kubernetes-tier-adoption`: "The reconciler converts a render with one export" now covers the inventory entries too: the renderer's own export is gone.
- `kernel-module-renderer`: the render result no longer carries inventory entries ("Render a ModuleRelease through the kernel", "Adapt compiled output to operator resources").
- `library-kernel-runtime`: "Rendered values are dropped after conversion" names the entries as data read from the export.
- `render-input-key`: "An operator or library upgrade renders every object once" gains a scenario for this change's digest change: one apply, then convergence.
- `test-registry-lifecycle`: "End-to-end integration tests" no longer says the renderer's result carries inventory entries; the real-renderer scenario asserts the runtime-identity labels only.

## Impact

- Code: `internal/inventory/` (three files and their tests deleted, conversions added), `internal/status/digests.go` (render digest deleted), `internal/render/{module,kernel_module_renderer}.go` (entries removed from the result), `internal/reconcile/{converted,moduleinstance,modulepackage,withhold}.go`, `internal/apply/prune.go` (comment).
- Tests: the stub render results under `internal/controller` and `test/integration/reconcile` stop setting `InventoryEntries`. `withhold_invariant_test.go` is replaced (design.md D3) and its `configMapResource` helper moves to `shrink_refusal_test.go`. New tests pin the one export, the conversions, the closed surface of `internal/inventory` and the upgrade.
- Docs: `docs/RENDERING.md` (one export per resource, digests from the library).
- Dependencies: none. The operator already pins library `v1.0.0-beta.6` (opm-operator#248).
- Downstream: the cli's adoption change records the same digests. Until both frontends run their adopting releases, the two digest one render differently, as they do today.
- Enhancement: `enhancement.yaml` declares 0012 and claims no decision. 0012:D6 and 0012:D7 hold only once both frontends compute through `opm/k8s/inventory` (0012:D1).
