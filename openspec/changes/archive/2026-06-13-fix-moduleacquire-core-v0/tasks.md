# Tasks: fix-moduleacquire-core-v0

> Library change `add-registry-module-loader` shipped as **library v0.5.0** (`Kernel.LoadModuleFromRegistry`).
>
> **Status (2026-06-13):** acquisition fix landed and verified (tasks 1–2). The renderer + release happy-paths (tasks 3–4) and a fully-green `dev:test:local` (5.2) are **blocked by a separate, pre-existing catalog concreteness gap** (`catalogs/opm@v0.5.0` never wires `#ConfigMapDefaults` into `#ConfigMapsResource`, so `immutable` stays non-concrete). Split out to a `catalog_opm` change — see design.md § Implementation Findings.

## 1. Adopt the library registry loader

- [x] 1.1 Bump `github.com/open-platform-model/library` to `v0.5.0` (ships `Kernel.LoadModuleFromRegistry`); `go mod tidy`.
- [x] 1.2 Rewrite `internal/moduleacquire/acquire.go` to call `k.LoadModuleFromRegistry(ctx, path, version)` then `k.NewModuleFromValue(...)`, keeping the `Acquire(ctx, k, path, version, registry) (*module.Module, error)` signature and error-wrapping. No temp dir, no `LoadModulePackage`/`LookupPath`.
- [x] 1.3 Delete `internal/moduleacquire/shim.go` and `internal/moduleacquire/shim_test.go`; update the package doc comment to describe delegation to the kernel, not a wrapper.

## 2. Regression coverage for module acquisition

- [x] 2.1 `test/integration/reconcile/acquire_test.go` "acquires the module and decodes its metadata" passes against the local registry; name/version assertions plus the explicit `metadata.modulePath` assertion (`testing.opmodel.dev/modules`) — the field that regressed — all hold.
- [x] 2.2 The unresolvable-module spec passes (returns a wrapped error). The temp-dir counter assertions are now vacuous (acquisition writes no temp dir) but still hold at zero; left as a belt-and-suspenders check.

## 3. Verify the renderer happy-path

- [x] 3.1 `kernel_module_renderer_test.go` "renders the fixture module's resources…" **passes**. The catalog concreteness gap (`…configMaps.hello.immutable: incomplete value bool`) was fixed in `catalog_opm` and released as **`catalogs/opm@v0.5.1`** (GHCR). The hello fixture is re-pinned `v0.5.0 → v0.5.1` (`test/fixtures/modules/hello/cue.mod/module.cue`) and republished; all three registry-gated specs pass against the re-pinned fixture (pristine `CUE_CACHE_DIR`, local registry seeded with v0.5.1). Confirmed 2026-06-13.

## 4. Release path (release-fixture rewrite — tracked as a separate follow-up)

- [x] 4.1 The D3 spike (load `releases/hello` through `LoadReleasePackage` → `Compile`) is now **unblocked** — the `immutable` concreteness gap that previously masked it is fixed (catalog v0.5.1). Remaining: run the spike to answer whether `Compile` consumes a hand-authored `#module: hello` release package (the self-reference question) or needs a kernel-side accommodation.
- [x] 4.2 `releases/hello` rewrite still pending (depends on 4.1's outcome): rewrite to the core@v0 shape pinning `catalogs/opm@v0 v0.5.1`, add a kernel load+compile test, update the `test-registry-lifecycle` spec. Separate from the module-acquisition fix; not requested in this slice.
- [x] 4.3 Finding recorded in design.md Open Questions / Implementation Findings; the catalog concreteness fix is split into a `catalog_opm` change. The acquisition fix (tasks 1–2) ships independently.

## 5. Validation gates

- [x] 5.1 `task dev:test` (ghcr/CI mapping) — **green**; registry-gated specs skip (CI parity preserved). Confirmed 2026-06-13.
- [x] 5.2 `task dev:test:local` — **green end to end** (all packages incl. `test/integration/reconcile`'s registry-gated acquire + renderer specs) against the local registry seeded with `catalogs/opm@v0.5.1` and the re-pinned/republished hello fixture. This is the acceptance `modernize-test-fixtures` deferred. Confirmed 2026-06-13.
- [x] 5.3 `task dev:fmt dev:vet dev:lint` — **clean** (lint: 0 issues). Confirmed 2026-06-13.
