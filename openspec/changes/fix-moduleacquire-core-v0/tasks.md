# Tasks: fix-moduleacquire-core-v0

## 1. Fix the acquisition shim (embed → field binding)

- [ ] 1.1 `internal/moduleacquire/shim.go`: change `packageTmpl` so `acquire.cue` binds the import to a regular field instead of embedding at root — `import mod "<path>"` then `out: mod` (introduce a `shimField = "out"` const). Add a comment explaining the binding is load-bearing vs core@v0 `#Module`'s self-reference (cite `field not allowed`), so it is not "simplified" back to a root embed.
- [ ] 1.2 `internal/moduleacquire/acquire.go`: after `Kernel.LoadModulePackage`, look up the bound field (`val.LookupPath(cue.ParsePath("out"))`, check `.Err()`/`.Exists()`) and pass that value to `Kernel.NewModuleFromValue`. Keep the temp-dir cleanup and error-wrapping unchanged.

## 2. Regression coverage for module acquisition

- [ ] 2.1 `internal/moduleacquire/shim_test.go`: assert the generated `acquire.cue` binds to a regular field (contains `out:` / no bare root `mod` embed; not a `#`-prefixed field). Keep the existing "single dependency, no catalog pin" assertions.
- [ ] 2.2 Confirm the registry-gated integration specs pass against the local registry: `test/integration/reconcile/acquire_test.go` "acquires the module and decodes its metadata" and the metadata assertions (name `hello`, version `0.0.2`). Add an explicit assertion that `metadata.modulePath` equals the author-set value (the field that regressed), so the self-reference fix is pinned.

## 3. Verify the renderer happy-path

- [ ] 3.1 Run `test/integration/reconcile/kernel_module_renderer_test.go` "renders the fixture module's resources with provenance and inventory" against the local registry; confirm it passes (it calls `Acquire` internally) — resources carry release/component/transformer provenance and the runtime-identity labels.

## 4. Release path — verify, then rewrite the fixture (design D3)

- [ ] 4.1 Spike: write a Go test (or `cmd/flow-inspect`-style probe) that loads a candidate core@v0 `releases/hello` package (`kind`/`metadata`/`#module: hello`, no `#ModuleRelease` embed) through `Kernel.LoadReleasePackage` → `NewReleaseFromValue` → `Compile`. Determine whether the kernel's scope-trick absorbs the `#module` self-reference or whether a kernel-side accommodation is needed.
- [ ] 4.2 If 4.1 succeeds: rewrite `test/fixtures/releases/hello/{release.cue,values.cue,cue.mod/module.cue}` to the confirmed core@v0 shape (pin `core@v0`, `catalogs/opm@v0`, `hello@v0 v0.0.2`); add a Go test asserting the kernel loads+compiles it to at least one resource. Update the `test-registry-lifecycle` spec (the release-fixture requirement deferred from `modernize-test-fixtures`).
- [ ] 4.3 If 4.1 needs a kernel change: leave `releases/hello` old-era, record the finding in this change's design Open Questions, and split the release-fixture rewrite into a kernel-side follow-up. The acquisition fix (tasks 1–3) still ships.

## 5. Validation gates

- [ ] 5.1 `task dev:test` (ghcr/CI mapping) — green; registry-gated specs still skip (CI parity preserved).
- [ ] 5.2 `task dev:test:local` — **green end to end**: acquire + kernel-renderer happy-path pass; this is the acceptance `modernize-test-fixtures` deferred.
- [ ] 5.3 `task dev:fmt dev:vet dev:lint` — clean.
