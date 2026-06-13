# Design: fix-moduleacquire-core-v0

## Context

`internal/moduleacquire` bridges "module published in an OCI registry by `path@version`" to the library's `Kernel.LoadModulePackage(dir)`, which only loads from a local directory. It does so by writing a throwaway *wrapper* CUE module that depends on the target and an `acquire.cue` that pulls the module in:

```cue
package acquire
import mod "testing.opmodel.dev/modules/hello@v0"
mod
```

`Acquire` then loads that wrapper and calls `Kernel.NewModuleFromValue(val)`. This wrapper technique is the source of the bug, and it fails in three compounding ways (proposal.md): self-reference collapse on re-embedding, unresolved transitive catalog/core deps, and a root shape-gate mismatch if the module is bound off-root. A plain `cue eval .` inside the module's own directory renders it correctly — proving the module and CUE are fine; only *wrapping* breaks.

**Spike (A/B), `experiments/moduleacquire-spike/`:** loading the module **as the main module** — fetched from the registry, its own `cue.mod/module.cue` present, `kind`/`metadata` at the package root — eliminates all three failures. Two staging strategies were verified end-to-end against the real `hello@v0.0.2` module on the local registry: (A) copy fetched files to a temp dir + reuse `LoadModulePackage`, and (B) in-memory `load.Config.Overlay`. Both decode `name=hello version=0.0.2 modulePath=testing.opmodel.dev/modules`; warm latency ~13 ms each. The obvious `load.Config.FS`-pin variant fails on transitive deps (recorded in the library design).

This is custom OCI-acquisition plumbing that the library's **Principle V** says belongs in the kernel, not the operator. So the fix is to **consume a library primitive**, not to repair the operator's wrapper. The library change `add-registry-module-loader` adds `Kernel.LoadModuleFromRegistry` (Path B: CUE-native fetch + in-memory Overlay load + shape gate). This change adopts it and deletes the wrapper.

## Goals / Non-Goals

**Goals:**

- `moduleacquire.Acquire` loads a registry-published core@v0 `#Module` and decodes it to `*module.Module` with correct metadata — by delegating to `Kernel.LoadModuleFromRegistry`, with no wrapper, no temp dir, no self-reference collapse.
- The two failing integration specs pass against the local registry; a regression asserts a **real** core@v0 module decodes (not a `{kind:"Module"}` stub), pinning `metadata.modulePath`.
- `task dev:test:local` goes green (the `modernize-test-fixtures` deferred acceptance).
- Rewrite `test/fixtures/releases/hello` to a kernel-loadable core@v0 shape and verify it through `Kernel.LoadReleasePackage` → `Compile`.

**Non-Goals:**

- Implementing the registry loader itself — that is the library change `add-registry-module-loader`. This change only consumes it.
- Repairing the wrapper-shim approach (field-binding, surfacing identity at root, reconstructing transitive deps). All abandoned — the wrapper is deleted.
- Changing core@v0's `#Module` self-reference (a published-schema contract).
- Broad Release-renderer coverage beyond one happy-path proving the fixture loads.

## Decisions

### D1 — Consume `Kernel.LoadModuleFromRegistry`; delete the wrapper shim

`acquire.go` becomes a thin delegation:

```go
func Acquire(ctx context.Context, k *kernel.Kernel, path, version, registry string) (*module.Module, error) {
    val, err := k.LoadModuleFromRegistry(ctx, path, version)
    if err != nil {
        return nil, fmt.Errorf("acquiring module %q@%q: %w", path, version, err)
    }
    mod, err := k.NewModuleFromValue(val)
    if err != nil {
        return nil, fmt.Errorf("acquiring module %q@%q: %w", path, version, err)
    }
    return mod, nil
}
```

`shim.go` and `shim_test.go` are deleted. The `registry` parameter is retained for signature stability and to configure the kernel's registry where `Acquire`'s caller constructs it (the kernel resolves the registry it was built with via `WithRegistry`); if the parameter becomes fully redundant once the kernel is always pre-configured, simplifying the signature is a follow-up, not part of this fix.

**Why this over the contained shim fix:** the wrapper cannot resolve transitive deps without a hand-rolled dependency walk or shelling to `cue` (absent at controller runtime), and even with deps it must satisfy the loader's root shape gate while dodging the self-reference — three patches on a fundamentally wrong technique. Loading the module as itself removes the entire class. Per Principle V the capability is the library's.

### D2 — Path B (in-memory) is realized in the library, not duplicated here

The spike's Path B (Overlay, no temp dir, no cleanup, concurrency-friendly) is the chosen technique and lives in the library (`add-registry-module-loader`, design D2). The operator does not reimplement fetch/overlay/shape-gate; it calls `k.LoadModuleFromRegistry`. The operator spike at `experiments/moduleacquire-spike/` stays as the evidence that motivated the library API and is referenced by both changes; it is throwaway and may be removed once the library primitive lands.

### D3 — Rewrite the release fixture to a kernel-loadable shape; verify through the kernel, not `cue vet`

Unchanged from the original design. The operator's Release path is `LoadReleasePackage(dir)` → `NewReleaseFromValue` → `Compile`. The canonical loadable shape carries release **data** without embedding the closed `#ModuleRelease` schema (the kernel applies it internally via its scope-trick in `Compile`):

```cue
package hello
import hello "testing.opmodel.dev/modules/hello@v0"
kind: "ModuleRelease"
metadata: { name: "hello", namespace: "default" }
#module: hello
```

Whether `#module: hello` survives `LoadReleasePackage` → `Compile` is verified empirically in the tasks (a Go test loading the fixture through the kernel), not assumed. If the kernel path cannot consume a hand-authored real-module release package without a kernel change, the release-fixture rewrite splits out to a kernel-side change and only the acquisition fix lands here.

> **Note:** D3's release-loading question is independent of acquisition. If it also turns out to need a registry-aware loader, that is a candidate to fold into a future `LoadReleaseFromRegistry` in the library (sibling of `LoadModuleFromRegistry`) — out of scope here; flagged for the library follow-up.

### D4 — Regression coverage that would have caught this

The registry-gated `acquire_test.go` acquires a real core@v0 module and asserts decoded metadata, now including `metadata.modulePath` (the field that regressed). It skips in CI (ghcr has no fixture module) and runs under `task dev:test:local`. Document it as the canonical guard. The unit-level `shim_test.go` is deleted with the shim (there is no longer a generated wrapper to assert on).

## Risks / Trade-offs

- **Sequencing dependency on the library change** → this change cannot merge before `add-registry-module-loader` ships and the operator's library pin is bumped. Mitigation: the two changes are explicitly sequenced; until the bump, the operator specs stay red (the existing known failure).
- **`LoadModuleFromRegistry` semantics differ subtly from the spike** → the library re-verifies Path B in-library (its own CUE pin + `modregistrytest`); the operator's integration specs are the end-to-end check that the delivered API actually decodes the real fixture. Keep both.
- **Release path (D3) needs a kernel change after all** → de-risked by a discrete verification task with a clean split point; the acquisition fix (D1) stands alone and ships regardless.
- **Regression: someone reintroduces a wrapper** → the wrapper code is deleted, not patched; `acquire.go` is a thin delegation, so there is nothing to "simplify" back into an embed.

## Migration Plan

1. Land library `add-registry-module-loader`; bump the operator's library dependency to that version.
2. Delete `shim.go` + `shim_test.go`; rewrite `acquire.go` to delegate (D1). Verify `acquire_test.go` and `kernel_module_renderer_test.go` pass under `task dev:test:local`; `task dev:test` (CI) stays green (gated specs still skip).
3. Verify the release path (D3): Go test loading `releases/hello` through `Kernel.LoadReleasePackage` → `Compile`. If green, rewrite the fixture to core@v0 and keep the test; if it needs a kernel change, split that out and note it here.
4. Rollback: revert the dependency bump + acquire rewrite (and restore the shim only if a rollback to the broken-for-core@v0 behavior is genuinely needed). Fixtures are test-only.

## Implementation Findings (2026-06-13)

**Acquisition fix landed and verified.** Library bumped to `v0.5.0` (ships `Kernel.LoadModuleFromRegistry`); `acquire.go` rewritten to delegate; `shim.go`/`shim_test.go` deleted. Both registry-gated acquire specs pass against the local registry, including the `metadata.modulePath` regression assertion. The self-reference collapse, the missing-transitive-deps failure, and the root shape-gate mismatch are all gone.

**Renderer + release happy-paths are blocked by a separate, pre-existing catalog gap — DESCOPED from this change.** With acquisition fixed, the renderer spec advances past `Acquire` and fails in `SynthesizeRelease`:

```
release "kernel-hello": not fully concrete:
  release.components.hello.spec.configMaps.hello.immutable: incomplete value bool
```

Root cause is in the **published `opmodel.dev/catalogs/opm@v0.5.0`** (`resources/configmap.cue`), not the operator or acquisition:

- `#ConfigMapSchema` declares `immutable: bool` with **no default** (line 38).
- `#ConfigMapDefaults: #ConfigMapSchema & { immutable: false }` exists (line 43) but is **never wired into** `#ConfigMapsResource`, which uses bare `#ConfigMapSchema` (line 23).

So any module using `#ConfigMaps` without explicitly setting `immutable` renders a non-concrete `immutable: bool`, which `SynthesizeRelease`/`Compile` rejects. The acquire bug had masked this (it failed first). It blocks the renderer happy-path (task 3) **and** the release-path verification + `releases/hello` rewrite (task 4, since `Compile` hits the same field), and therefore a fully-green `task dev:test:local` (task 5.2).

**Split-out — fix IMPLEMENTED in `catalog_opm` (2026-06-13), pending release.** `src/resources/configmap.cue` and `src/resources/secret.cue` now default the non-concrete fields directly in the schemas: `#ConfigMapSchema.immutable: bool | *false`, `#SecretSchema.immutable: bool | *false`, and `#SecretSchema.type: *"Opaque" | …` (the unused `#ConfigMapDefaults`/`#SecretDefaults` are left as published surface). `task vet` + INDEX freshness pass. **Verified end-to-end:** with the fixed catalog published to the local registry and a fresh `CUE_CACHE_DIR`, all three registry-gated operator specs pass — including this change's renderer happy-path (no more `incomplete value bool`).

Remaining to fully close tasks 3–5 here: the catalog fix ships via CI as a new version (a `fix:` → **patch `v0.5.1`** by the repo's commit convention), then re-pin `test/fixtures/modules/hello/cue.mod/module.cue` (and `releases/hello`) from `catalogs/opm@v0 v0.5.0` → `v0.5.1` and re-run tasks 3–5. The pin can only land once the real `v0.5.1` is on GHCR (local verification used a fresh cache against a locally-republished `v0.5.0`).

## Open Questions

- Does `Kernel.LoadReleasePackage` → `Compile` consume a hand-authored `#module: hello` release package, or does it require a kernel-side accommodation (possibly a future `LoadReleaseFromRegistry`)? **Cannot be answered here** until the catalog concreteness gap above is fixed — `Compile` fails on `immutable` before the `#module` self-reference question can be exercised. Carry into the post-catalog-fix verification.
- Once the kernel is always constructed with `WithRegistry`, is `Acquire`'s `registry` parameter redundant? It is, in production (`cmd/main.go` builds the kernel with `WithRegistry(registry)` and passes the same value). Kept for signature stability; drop it (and the renderer's `Registry` field) in a follow-up cleanup — out of scope here.
