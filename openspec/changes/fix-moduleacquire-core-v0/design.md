# Design: fix-moduleacquire-core-v0

## Context

`internal/moduleacquire` bridges "module published in an OCI registry by `path@version`" to the library's `Kernel.LoadModulePackage(dir)`, which only loads from a local directory. The shim (`shim.go`) writes a throwaway CUE module that depends on the target and an `acquire.cue` that **embeds the module at the package root**:

```cue
package acquire
import mod "testing.opmodel.dev/modules/hello@v0"
mod
```

`Acquire` then loads that package and calls `Kernel.NewModuleFromValue(val)` to decode `*module.Module`.

Embedding `mod` at the root re-evaluates `#Module` under the `acquire` package's struct. core@v0's `#Module` declares self-referential metadata (`modulePath: metadata.modulePath`, `version: metadata.version`); on re-evaluation the self-reference resolves to bottom and the author-set values are rejected:

```
metadata.modulePath: field not allowed:
    …/core@v0.4.0/module.cue:14:3
    …/modules/hello@v0.0.2/module.cue:15:2
metadata.version: field not allowed: …
```

This breaks `Acquire` for **every** real core@v0 module (`library/testdata/modules/web_app` fails identically). It was masked until `modernize-test-fixtures` replaced the old-era fixture (which failed earlier with a parse error) with a correct kernel-era one. `library/opm/helper/synth/release.go` documents the same constraint for the release path and works around it in Go by referencing the module as a scope-bound value rather than re-emitting it as source.

**Empirically confirmed during design:** the collapse is caused specifically by **root embedding**. Binding the import to a regular field instead —

```cue
package acquire
import mod "testing.opmodel.dev/modules/hello@v0"
out: mod
```

— concrete-evaluates the full module with correct `metadata` (`fqn`, `uuid` all resolve). The module value is preserved; only `field not allowed` from root re-admission disappears.

The release fixture (`test/fixtures/releases/hello`) hits a related but distinct form: `#module: hello` (assigning a `#Module`-embedding value to the `#module` **definition** field) re-admits and fails even without the `#ModuleRelease` schema embed. That path is consumed by the kernel's `LoadReleasePackage` → `Compile` (which already carries the scope-trick), not by `cue vet`, so it needs verification through the kernel rather than a standalone vet gate.

## Goals / Non-Goals

**Goals:**

- `moduleacquire.Acquire` loads a registry-published core@v0 `#Module` and decodes it to `*module.Module` with correct metadata. No self-reference collapse.
- The two failing integration specs pass against the local registry: `acquire_test.go` "acquires the module and decodes its metadata" and `kernel_module_renderer_test.go` "renders the fixture module's resources with provenance and inventory".
- A regression test loads a **real** core@v0 module (not a `{kind: "Module"}` stub), so this can't silently regress.
- `task dev:test:local` goes green (the `modernize-test-fixtures` deferred acceptance).
- Rewrite `test/fixtures/releases/hello` to a kernel-loadable core@v0 shape and verify it through `Kernel.LoadReleasePackage` → `Compile`.

**Non-Goals:**

- Adding a library/kernel public API (approach B) — confirmed unnecessary (see D2); avoid the cross-repo SemVer/MIGRATIONS cost.
- Changing core@v0's `#Module` self-reference — that is a published-schema contract; the consumer adapts, per `core/` evolution rules.
- Broad Release-renderer coverage beyond one happy-path proving the fixture loads — deeper Release coverage stays in its own follow-up if it balloons.
- Re-publishing core/catalog (already local from `modernize-test-fixtures`).

## Decisions

### D1 — Fix acquisition by binding the import to a field, not embedding at root

Change the shim's `acquire.cue` template from `mod` to `out: mod` (a regular, non-definition field), and have `Acquire` look up the `out` path on the loaded value before `NewModuleFromValue`:

```go
val, err := k.LoadModulePackage(ctx, dir, loaderfile.LoadOptions{Registry: registry})
// …
modVal := val.LookupPath(cue.ParsePath("out"))
mod, err := k.NewModuleFromValue(modVal)
```

This is the minimal contained fix, empirically confirmed: binding preserves the module value's embedding chain; root re-admission is what collapses the self-reference. `shim.go`'s `packageTmpl` gains the `out:` field; `acquire.go` adds one `LookupPath`. The temp-dir lifecycle, per-call registry, and concurrency guarantees are unchanged.

*Field name:* use a stable, unambiguous key (`out`, or `module`). Avoid a `#`-prefixed (definition) field — that re-closes and re-admits, reproducing the bug (the release path's `#module: hello` failure confirms `#`-fields re-admit).

### D2 — Approach A (contained shim fix), not B (kernel API)

The proposal floated a `Kernel.LoadModuleFromRegistry` library API. D1's confirmation makes it unnecessary: the operator-side shim fix fully resolves acquisition with no library change, no new public surface, no library SemVer bump, no `MIGRATIONS.md` entry, no cross-repo coordination. If a future consumer (CLI, Crossplane fn) needs the same bridge, promoting a shared helper into the library is a separate, additive decision — out of scope here.

*Alternative considered:* the full `synth.Release` scope-trick (build a `userModule` scope value in Go, compile a referencing source). Rejected for acquisition: it is heavier than needed — acquisition only needs the loaded module value, and a CUE-source field binding achieves that without Go-side scope construction. (The scope-trick remains the right tool for the *release* path, which must also unify `#ModuleRelease` — see D3.)

### D3 — Rewrite the release fixture to a kernel-loadable shape; verify through the kernel, not `cue vet`

The operator's Release path is `LoadReleasePackage(dir)` → `NewReleaseFromValue` → `Compile`, using the library's CUE (v0.17). `.tasks/release.yaml` ships the fixture via `flux push artifact` (no `cue vet` gate). So the release fixture's acceptance is **"the kernel loads and compiles it"**, not "cue vet passes".

The canonical loadable shape carries release **data** without embedding the closed `#ModuleRelease` schema (the kernel applies it internally via its scope-trick in `Compile`):

```cue
package hello
import hello "testing.opmodel.dev/modules/hello@v0"
kind: "ModuleRelease"
metadata: { name: "hello", namespace: "default" }
#module: hello
```

The open question is whether `#module: hello` survives `LoadReleasePackage` → `Compile` (the kernel's scope-trick may absorb the self-reference the way it does for synth) or whether the fixture must bind the module differently. **This is verified empirically in the tasks** (a Go test loading the fixture through the kernel), not assumed. If the kernel path cannot consume a hand-authored real-module release package without a kernel change, the release-fixture rewrite is split out to a kernel-side change and only the acquisition fix lands here — keeping this change shippable.

### D4 — Regression coverage that would have caught this

Add a `moduleacquire` test that acquires a real core@v0 module and asserts decoded metadata — the existing `acquire_test.go` (integration, registry-gated) does this but skips in CI. Add a hermetic-ish unit/integration assertion where feasible so the "real module, not a stub" gap that hid this bug is closed at a tier that runs more often. At minimum, ensure the registry-gated specs are documented as the canonical guard and run in `dev:test:local`.

## Risks / Trade-offs

- [The `out:`-binding fix works for `cue eval` but `Kernel.LoadModulePackage`/`NewModuleFromValue` reads a different path or forces re-admission] → confirmed mechanism at the CUE level; the tasks verify it end-to-end through the actual kernel calls before claiming done. If `NewModuleFromValue` needs the value at root, adjust to fill/extract accordingly.
- [Release path (D3) needs a kernel change after all] → de-risked by making the release-fixture verification a discrete task with a clean split point; the acquisition fix (D1) stands alone and ships regardless.
- [Field name `out` collides with something in a module] → it is a field on the *shim* package, not the module; the module is the value of `out`. No collision with module contents.
- [Future regression: someone "simplifies" the shim back to root embedding] → the regression test (D4) fails loudly; add a comment in `shim.go` explaining why the binding (not embed) is load-bearing, citing the self-reference.

## Migration Plan

1. Land the acquisition fix (D1) + regression (D4): `shim.go` template + `acquire.go` lookup. Verify `acquire_test.go` and `kernel_module_renderer_test.go` pass under `task dev:test:local`; `task dev:test` (CI) stays green (gated specs still skip).
2. Verify the release path (D3): Go test loading `releases/hello` through `Kernel.LoadReleasePackage` → `Compile`. If green, rewrite the fixture to core@v0 and keep the test; if it needs a kernel change, split that out and note it here.
3. Rollback: revert the shim/acquire edits; behavior returns to the (broken-for-core@v0) embedding. Fixtures are test-only.

## Open Questions

- Does `Kernel.LoadReleasePackage` → `Compile` consume a hand-authored `#module: hello` release package, or does the `#module` definition-field re-admission require a kernel-side accommodation? Resolved empirically in tasks before the release-fixture rewrite is committed.
- Should the field-binding shim helper eventually move into the library for CLI/Crossplane reuse? Deferred — additive, not needed now.
