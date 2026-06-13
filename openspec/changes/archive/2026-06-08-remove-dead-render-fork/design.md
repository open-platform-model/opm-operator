## Context

Stage 5a of fork removal. Both cut-overs are archived; `main.go` wires only `KernelModuleRenderer`/`KernelReleaseRenderer`. An importer scan confirms what is now dead vs. shared:

- The new kernel renderers reuse, from the old files: `RenderResult` + `buildInventoryEntries` (`internal/render/module.go`), `KindModuleRelease`/`KindBundleRelease` + `ErrUnsupportedKind` (`internal/render/release.go`), and the `ModuleRenderer`/`ReleaseRenderer` interfaces (`internal/render/renderer.go`). These three files are mixed — keep those pieces, delete the rest.
- `pkg/render`, `pkg/module`, `pkg/validate` are imported only by the deleted impls and each other → orphaned once the impls go.
- `internal/synthesis` is imported only by the deleted `RenderModuleFromRegistry` → orphaned (removes the `v1.3.4` pin).
- Live references that must be fixed in the same slice: `internal/reconcile/release.go:368` falls back to `render.PackageReleaseRenderer{}` when the renderer is nil; three `test/integration/reconcile` tests construct `&render.RegistryRenderer{}` or use `internal/synthesis`.
- Deferred to 5b (still live): `pkg/provider` (14 importers), `pkg/loader` (used by `internal/catalog`), `internal/catalog` (the provider load in `main.go`), and the `prov` parameter / `Provider` fields.

## Goals / Non-Goals

**Goals:**

- Delete the dead render fork (old impls + `pkg/render`/`pkg/module`/`pkg/validate` + `internal/synthesis`), preserving the four shared pieces.
- Remove the `v1.3.4` catalog pin.
- Keep the tree compiling and green: fix the one nil-renderer fallback and drop the fork-based integration tests.

**Non-Goals:**

- Any provider-plumbing removal (`prov` param, `Provider` fields, `pkg/provider`/`pkg/loader`/`internal/catalog`, provider load) — that is 5b, a signature refactor.
- BundleRelease.

## Decisions

### Pure deletion; preserve the four shared pieces in place

**Decision:** Trim the old funcs/structs out of `internal/render/{module,release,renderer}.go`, leaving `RenderResult`, `buildInventoryEntries`, the kind consts, `ErrUnsupportedKind`, and the interfaces where they are; fix the resulting imports.

**Rationale:** The kernel renderers already import these by their current paths, so leaving them in place avoids touching the kernel renderers (beyond stale comments) and keeps the diff a deletion. Moving them to new files would be churn with no benefit this slice.

**Alternatives considered:** relocate the shared pieces to a new `internal/render/shared.go` — unnecessary movement; deferred (could happen naturally when 5b reshapes the interfaces).

### Remove the nil-renderer fallback rather than keep a default

**Decision:** In `internal/reconcile/release.go`, delete the `if renderer == nil { renderer = render.PackageReleaseRenderer{} }` fallback; treat a nil renderer as a programming error (production always injects `KernelReleaseRenderer`; tests inject their own).

**Rationale:** The fallback exists only to reference a now-deleted type. Production wiring is explicit; a silent default renderer is exactly the kind of hidden behavior the cut-over removed. Tests that relied on the default now pass an explicit stub.

**Alternatives considered:** default to `KernelReleaseRenderer` — can't, it needs Kernel/Store/Registry the reconcile layer doesn't hold; the renderer belongs injected from `main.go`.

### Delete the fork-based integration tests

**Decision:** Remove `test/integration/reconcile/{e2e_registry_test.go, runtime_identity_test.go, synthesis_test.go}`.

**Rationale:** They assert behavior of deleted mechanisms (`RegistryRenderer`, `internal/synthesis`). Keeping them is impossible (they reference deleted types). The kernel renderers' own tests cover the live paths; any uniquely-valuable runtime-identity assertion can be re-added against `KernelModuleRenderer` (noted, not required here).

**Alternatives considered:** rewrite them against the kernel renderers in this slice — scope creep; the kernel renderers already have tests, and the cut-over slices added envtests.

### Retire the obsolete spec capabilities

**Decision:** REMOVE `cue-rendering` (whole), the `module-release-synthesis` "Release synthesis" requirement, and the `module-renderer-interface` "Production wiring" requirement.

**Rationale:** These describe deleted mechanisms or now-false wiring; their surviving behavior is re-specified by `kernel-module-renderer`/`platform-gated-rendering`/`release-kernel-rendering`/`module-acquisition`. Leaving them is spec drift. The non-obsolete requirements of `module-release-synthesis` and `module-renderer-interface` are preserved.

## Risks / Trade-offs

- **Hidden importer of a "dead" symbol** → the importer scan was explicit; the build is the backstop (deleting a still-used symbol fails to compile, caught by `task dev:vet`/`dev:test`).
- **Losing a uniquely-valuable test assertion** (e.g., runtime-identity end-to-end) → noted; re-add against the kernel renderer if a gap surfaces. The cut-over envtests already exercise the live path.
- **Leftover stale prose** in the `module-renderer-interface` free-floating `## Scenarios` block (references `RegistryRenderer`) → minor; the normative "Production wiring" requirement is removed; full prose cleanup is cosmetic.

## Migration Plan

1. Trim `internal/render/{module,release,renderer}.go` to the preserved pieces; fix imports.
2. Remove the nil-renderer fallback in `internal/reconcile/release.go`.
3. Delete `pkg/render/`, `pkg/module/`, `pkg/validate/`, `internal/synthesis/` (incl. their tests).
4. Delete the three fork-based integration tests.
5. Update stale doc comments in the kernel renderers.
6. `task dev:fmt dev:vet dev:lint dev:test` — confirm green; confirm no remaining import of the deleted packages (`grep`).

**Rollback:** revert the commit; the deleted code returns. Since nothing live depended on it (post-cut-over), there is no behavioral rollback concern.

## Open Questions

- Whether any assertion in the deleted `runtime_identity_test.go` is not already covered by a kernel-renderer test — confirm during implementation; re-add against `KernelModuleRenderer` if there is a genuine gap.
