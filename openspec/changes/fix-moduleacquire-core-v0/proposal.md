# Proposal: fix-moduleacquire-core-v0

## Why

`internal/moduleacquire` cannot load **any** `opmodel.dev/core@v0` `#Module` from the registry. The shim writes a throwaway package that embeds the target module via `import mod "…/hello@v0"; mod`; embedding re-evaluates `#Module`, and core@v0's self-referential metadata (`modulePath: metadata.modulePath`, `version: metadata.version`) collapses to bottom, so the author-set values are rejected:

```
metadata.modulePath: field not allowed:
    …/core@v0.4.0/module.cue:14:3
    …/modules/hello@v0.0.2/module.cue:15:2
metadata.version: field not allowed:
    …/core@v0.4.0/module.cue:15:3
```

This is the exact admission failure `library/opm/helper/synth/release.go` documents and works around in Go (a `userModule` scope value compiled via `cue.Scope`, not a re-emitted source fragment). The acquisition shim has no equivalent workaround, and there is no kernel "load module from registry by path" API — the shim exists to bridge that gap.

Impact: `moduleacquire.Acquire` (and therefore the ModuleRelease render path via `KernelModuleRenderer.RenderModule`) fails for every real core@v0 module — `library/testdata/modules/web_app` would fail identically. It was masked until now by the old test fixture's parse error; the `modernize-test-fixtures` change replaced that fixture with a correct kernel-era one (which `cue eval . --concrete` proves valid standalone), surfacing this bug. Two integration specs fail because of it: `acquire_test.go` "acquires the module and decodes its metadata" and `kernel_module_renderer_test.go` "renders the fixture module's resources with provenance and inventory". `task dev:test:local` cannot go green until this is fixed.

## What Changes

- Fix `internal/moduleacquire` so it loads a registry core@v0 `#Module` without the self-reference collapse. Candidate approaches (to be chosen in design):
  - **(A) Scope-trick, mirroring `synth.Release`:** load the module as a value bound to a non-hidden scope field and reference it via `cue.Scope`, so its type-embedding chain survives without re-admission. Keeps the fix local to `moduleacquire`.
  - **(B) Kernel API:** add a `Kernel.LoadModuleFromRegistry(path, version, opts)` (or equivalent) to the library so consumers resolve a registry module without an embedding shim, then have `moduleacquire` call it. Cleaner contract, but a library change with its own SemVer + tests.
- Unskip/confirm the registry-backed acquire and renderer happy-path specs pass against the local registry.
- **Rewrite `test/fixtures/releases/hello`** to the kernel-era core@v0 release-package shape (deferred here from `modernize-test-fixtures`). This depends on resolving the canonical hand-authored release-package form: `LoadReleasePackage` builds the package and the kernel applies `#ModuleRelease` internally, but a hand-authored `#module: hello` fails `cue vet` with the same self-reference. Determine whether the Release path tolerates it (via `LoadReleasePackage` → `Compile`, which has the scope-trick) or whether the fixture must omit the schema embed and carry only `kind`/`metadata`/`#module` data.
- Add Release-renderer happy-path coverage if the loading story is resolved (otherwise scope that to the existing Release-render-coverage follow-up).

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `module-acquisition`: the shim MUST bind the target module to a regular field rather than embedding it at the package root, so a registry-published core@v0 `#Module` decodes to `*module.Module` without the self-referential-metadata admission failure; the temp-dir/no-leak and per-call-registry guarantees stay unchanged.

> The `releases/hello` rewrite + Release happy-path verification is a tasks-level deliverable, not a requirements change. It is contingent on design D3 (does the kernel's `LoadReleasePackage` → `Compile` consume a hand-authored real-module release package?). If it needs a kernel-side accommodation, it splits out and only the acquisition fix lands here; the spec for the release path is touched only once the loadable shape is confirmed.

## Impact

- **Production Go code:** `internal/moduleacquire/shim.go` + `acquire.go` (approach A), or additionally a new `library/opm/kernel` API (approach B, library SemVer + `MIGRATIONS.md`).
- **Tests:** the two failing integration specs become green against the local registry; add a regression unit/integration test that loads a real core@v0 module (not a `{kind: "Module"}` stub). Release-path coverage as above.
- **Fixtures:** `test/fixtures/releases/hello` rewritten to core@v0.
- **SemVer:** PATCH for the operator (bug fix); MINOR for the library if approach B adds a public API.
- **Unblocks:** `task dev:test:local` fully green; the `modernize-test-fixtures` change's deferred acceptance.
- **Reference:** `library/opm/helper/synth/release.go` (the documented self-reference workaround); `modernize-test-fixtures` design D5/D6.
