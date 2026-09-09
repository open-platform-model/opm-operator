# Tasks: migrate-kernel-api-and-verdicts

Every task before 7.1 was planned against a local `replace github.com/open-platform-model/library => ../library` (added in 1.1, removed in 7.1). As landed: `v1.0.0-alpha.27` was published on 2026-09-08 carrying `one-api-tier` and `cue-owned-verdicts` (library `main` had no code past it), so groups 1 to 4 were verified against that pin in `go.mod` directly, with no `replace`. Groups 5 and 7 wait for the alpha carrying `kernel-owns-no-build-context`, which was still only planned in `library` (slice 6a).

## 1. Acquire calls drop their load options

- [x] 1.1 Add `replace github.com/open-platform-model/library => ../library` to `go.mod`, then `go build ./...`; verify the only failures reported are the `opm/helper/loader/file`, `opm/helper/synth` and `opm/core` import lines in the four files that name them and the two `CueContext()` calls (`cmd/main.go`, `internal/render/kernel_module_renderer.go`). Landed as: pinned `v1.0.0-alpha.27`; the three import lines failed as expected, the `CueContext()` calls did not (alpha.27 still has it), and two test constructors also broke: `*oerrors.UnmatchedComponentsError.Components` is `[]oerrors.UnmatchedComponent`, `*oerrors.TransformError` fields are `Component`/`Transformer`.
- [x] 1.2 `internal/controller/platform_controller.go`: drop the `loaderfile.LoadOptions{Registry: r.Registry}` argument from `AcquirePlatformFromDir` and the `loaderfile` import; remove the reconciler's `Registry` field if nothing else reads it; verify `make test` passes for the controller package and `grep -n 'r.Registry' internal/controller/platform_controller.go` returns only surviving uses.
- [x] 1.3 `internal/render/kernel_package_renderer.go`: drop the load-options argument from `AcquireInstanceFromDir`, rename the `loaderfile.ErrWrongKind` comparison (and the comment above it) to `liberrors.ErrWrongKind`, drop the `loaderfile` import and the renderer's now-unused `Registry` field; verify the package's wrong-kind test still classifies a non-instance package the same way.

## 2. Values reach synthesis as a source stack

- [x] 2.1 `internal/render/kernel_module_renderer.go`: replace the `CompileBytes(values.Raw, cue.Filename("values"))` block and `synth.InstanceInput` with `Kernel.LoadSourceFromBytes("spec.values", values.Raw)` feeding `kernel.InstanceInput{..., Values: []kernel.Source{src}}`, keeping the nil-values path as an empty stack; drop the `synth` import; verify a render with values applies them and a render without values still takes the module's `#config` defaults. Landed as: nil values become the `{}` document under the same origin, not an empty stack; an empty stack leaves `#ModuleInstance.values` as the open `_` and synthesis refuses it as non-concrete (design § Decisions). Both renders are asserted on the ConfigMap's `data.message`.
- [x] 2.2 Add a renderer test asserting that a values payload violating the module's `#config` fails with an error naming `spec.values`; verify it fails when the origin is set to anything else. Landed as: the kernel's own per-source check runs only after the instance build succeeds, and a module whose component consumes the value fails that build first (the error names the component path, no origin), so the renderer checks the source against `mod.ConfigSchema()` through `Kernel.ValidateConfigDetailed` before synthesis and words the CUE findings with their positions (design § Decisions).
- [x] 2.3 Check the e2e and envtest suites for an assertion on the old `values` origin string in a values-validation failure message and update any hit; verify `grep -rn 'cue.Filename("values")\|"values"' --include=*_test.go test/ internal/ | grep -i 'origin\|filename'` is empty.

## 3. Compiled moves and the verdict types are renamed

- [x] 3.1 `pkg/core/compiled_adapter.go`: change the import to `github.com/open-platform-model/library/opm/kernel` and the parameter type to `*kernel.Compiled`, leaving the field copy and nil guard unchanged; verify `go test ./pkg/core/...` passes and `grep -rn 'opm/core' --include=*.go .` is empty.
- [x] 3.2 `internal/reconcile/resolution.go`: update the comment naming `oerrors.OverSubscribedContractError` to `*oerrors.OverSubscribedContractsError`; `resolution_test.go`: construct `&oerrors.OverSubscribedContractsError{Contracts: []oerrors.OverSubscribedContract{{Key: ..., Catalogs: ...}}}`; verify `go test ./internal/reconcile/...` passes and `renderFailureReason` still maps an over-subscription refusal to its existing reason.
- [x] 3.3 `go build ./...` and `go vet ./...`; verify both are green and `grep -rn 'opm/helper/loader/file\|opm/helper/synth\|OverSubscribedContractError\|ComponentName\|TransformerFQN' --include=*.go .` is empty. Landed as: the only remaining hits are the operator's own `pkg/errors.TransformError` fields and `core.LabelComponentName`, not library references.

## 4. The operator words its own warnings

- [x] 4.1 `internal/render/kernel_module_renderer.go`: in `resultFromRender`, replace `Warnings: out.Warnings` with an operator formatter over `out.Diagnostics.UnhandledTraits` and the `Newer` rows of `out.Diagnostics.ResolvedVersions`, keeping the current sentence for each (skew: path, module version, platform version; trait: component, trait); verify a warn-policy skew render produces the same warning string as before this change.
- [x] 4.2 Add a formatter test over a diagnostics value carrying one unhandled optional trait and one newer resolved-versions row; verify it asserts both strings and fails when either row is dropped.
- [x] 4.3 `internal/reconcile/warnings.go`: key `WarningTracker.Update` on the advisory facts rather than the formatted strings (skew: path plus both versions; trait: component plus trait), leaving the emitted event text, reason and action unchanged; verify `go test ./internal/reconcile/...` passes.
- [x] 4.4 Add tracker tests for the two transition cases: same facts with different wording emits nothing, and a changed fact emits; verify each fails when the tracker is reverted to keying on the strings.

## 5. The kernel gate goes

Unblocked 2026-09-09: `kernel-owns-no-build-context` landed as library PR 123 and shipped in `v1.0.0-alpha.28` (library ADR-007, shares-nothing verbs); `go.mod` pins it.

- [x] 5.1 `internal/platform/store.go`: delete `kernelMu`, `AcquireKernel` and the doc comment describing the gate; `internal/controller/platform_controller.go`, `internal/render/kernel_package_renderer.go`, `internal/render/kernel_module_renderer.go`: delete the `release := r.Store.AcquireKernel()` and `defer release()` lines and every comment naming the gate; verify `go build ./...` is green and `grep -rn 'AcquireKernel\|kernelMu' --include=*.go .` is empty.
- [x] 5.2 `cmd/main.go`: `verifyCoreSchema` calls `k.SchemaCache().Get()`; verify the test covering `verifyCoreSchema` passes and `grep -rn 'CueContext()' --include=*.go .` is empty.
- [x] 5.3 `CLAUDE.md` (the "Renders share nothing; the kernel gate is narrow" bullet becomes "every kernel call shares nothing": one Kernel, no gate, concurrency bounded by `--max-concurrent-renders` and the Platform reconciler's one-generation-at-a-time construction) and `docs/RENDERING.md` (steps 1 and 2 no longer serialise); verify `grep -rn -i 'kernel gate\|AcquireKernel\|serialised behind' CLAUDE.md docs` is empty.
- [x] 5.4 Envtest: with `--max-concurrent-renders=2`, two ModuleInstances against one Platform reconcile at the same time; verify both reach `Ready=True` and `go test -race ./internal/render/... ./internal/controller/... ./internal/platform/...` is green. Landed as: a registry-backed sibling of the existing manager-driven concurrent-render spec (`test/integration/reconcile/concurrent_render_test.go`) that wraps the real `KernelModuleRenderer` in the rendezvous barrier, so both renders of the fixture module are in flight through one Kernel at once.

## 6. Gates and end-to-end

- [x] 6.1 `make fmt`, `make vet`, `make lint`, `make test` green; verify `make build` produces the manager binary. Landed as the Taskfile equivalents: `task dev:fmt dev:vet dev:lint dev:test` green against alpha.28, `go build ./cmd` produces the manager.
- [x] 6.2 Run the envtest and e2e suites against the local `replace`; verify a ModuleInstance with a warn-policy skew reaches `Ready=True` and emits exactly one `RenderWarning` event naming the path and both versions, and that a second reconcile with unchanged facts emits none. Landed as: against the alpha.28 pin; the three skew specs in `test/integration/reconcile/skew_test.go` pass (Warn: one RenderWarning naming the path and both versions, none on the second reconcile), and `task dev:e2e` on a fresh Kind cluster passes 5 of 13 specs with 8 skipped as before this change (two TODO stubs, six podinfo specs that need GHCR credentials or a local registry).

## 7. Pin the released library

`go.mod` pinned `v1.0.0-alpha.27` for groups 1 to 4 and moved to `v1.0.0-alpha.28` (all three library changes) for group 5; no `replace` was ever committed.

- [x] 7.1 Remove the `replace` directive and bump `github.com/open-platform-model/library` in `go.mod` to the published alpha carrying `one-api-tier`, `cue-owned-verdicts` and `kernel-owns-no-build-context`, then `go mod tidy`; verify `make test` and `make build` are green with no `replace` directive present and `openspec validate migrate-kernel-api-and-verdicts` passes.
