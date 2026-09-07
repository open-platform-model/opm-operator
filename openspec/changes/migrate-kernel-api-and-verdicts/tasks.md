# Tasks: migrate-kernel-api-and-verdicts

Every task before 6.1 runs against a local `replace github.com/open-platform-model/library => ../library` (added in 1.1, removed in 6.1).

## 1. Acquire calls drop their load options

- [ ] 1.1 Add `replace github.com/open-platform-model/library => ../library` to `go.mod`, then `go build ./...`; verify the only failures reported are the `opm/helper/loader/file`, `opm/helper/synth` and `opm/core` import lines in the four files that name them.
- [ ] 1.2 `internal/controller/platform_controller.go`: drop the `loaderfile.LoadOptions{Registry: r.Registry}` argument from `AcquirePlatformFromDir` and the `loaderfile` import; remove the reconciler's `Registry` field if nothing else reads it; verify `make test` passes for the controller package and `grep -n 'r.Registry' internal/controller/platform_controller.go` returns only surviving uses.
- [ ] 1.3 `internal/render/kernel_package_renderer.go`: drop the load-options argument from `AcquireInstanceFromDir`, rename the `loaderfile.ErrWrongKind` comparison (and the comment above it) to `liberrors.ErrWrongKind`, drop the `loaderfile` import and the renderer's now-unused `Registry` field; verify the package's wrong-kind test still classifies a non-instance package the same way.

## 2. Values reach synthesis as a source stack

- [ ] 2.1 `internal/render/kernel_module_renderer.go`: replace the `CompileBytes(values.Raw, cue.Filename("values"))` block and `synth.InstanceInput` with `Kernel.LoadSourceFromBytes("spec.values", values.Raw)` feeding `kernel.InstanceInput{..., Values: []kernel.Source{src}}`, keeping the nil-values path as an empty stack; drop the `synth` import; verify a render with values applies them and a render without values still takes the module's `#config` defaults.
- [ ] 2.2 Add a renderer test asserting that a values payload violating the module's `#config` fails with an error naming `spec.values`; verify it fails when the origin is set to anything else.
- [ ] 2.3 Check the e2e and envtest suites for an assertion on the old `values` origin string in a values-validation failure message and update any hit; verify `grep -rn 'cue.Filename("values")\|"values"' --include=*_test.go test/ internal/ | grep -i 'origin\|filename'` is empty.

## 3. Compiled moves and the verdict types are renamed

- [ ] 3.1 `pkg/core/compiled_adapter.go`: change the import to `github.com/open-platform-model/library/opm/kernel` and the parameter type to `*kernel.Compiled`, leaving the field copy and nil guard unchanged; verify `go test ./pkg/core/...` passes and `grep -rn 'opm/core' --include=*.go .` is empty.
- [ ] 3.2 `internal/reconcile/resolution.go`: update the comment naming `oerrors.OverSubscribedContractError` to `*oerrors.OverSubscribedContractsError`; `resolution_test.go`: construct `&oerrors.OverSubscribedContractsError{Contracts: []oerrors.OverSubscribedContract{{Key: ..., Catalogs: ...}}}`; verify `go test ./internal/reconcile/...` passes and `renderFailureReason` still maps an over-subscription refusal to its existing reason.
- [ ] 3.3 `go build ./...` and `go vet ./...`; verify both are green and `grep -rn 'opm/helper/loader/file\|opm/helper/synth\|OverSubscribedContractError\|ComponentName\|TransformerFQN' --include=*.go .` is empty.

## 4. The operator words its own warnings

- [ ] 4.1 `internal/render/kernel_module_renderer.go`: in `resultFromRender`, replace `Warnings: out.Warnings` with an operator formatter over `out.Diagnostics.UnhandledTraits` and the `Newer` rows of `out.Diagnostics.ResolvedVersions`, keeping the current sentence for each (skew: path, module version, platform version; trait: component, trait); verify a warn-policy skew render produces the same warning string as before this change.
- [ ] 4.2 Add a formatter test over a diagnostics value carrying one unhandled optional trait and one newer resolved-versions row; verify it asserts both strings and fails when either row is dropped.
- [ ] 4.3 `internal/reconcile/warnings.go`: key `WarningTracker.Update` on the advisory facts rather than the formatted strings (skew: path plus both versions; trait: component plus trait), leaving the emitted event text, reason and action unchanged; verify `go test ./internal/reconcile/...` passes.
- [ ] 4.4 Add tracker tests for the two transition cases: same facts with different wording emits nothing, and a changed fact emits; verify each fails when the tracker is reverted to keying on the strings.

## 5. Gates and end-to-end

- [ ] 5.1 `make fmt`, `make vet`, `make lint`, `make test` green; verify `make build` produces the manager binary.
- [ ] 5.2 Run the envtest and e2e suites against the local `replace`; verify a ModuleInstance with a warn-policy skew reaches `Ready=True` and emits exactly one `RenderWarning` event naming the path and both versions, and that a second reconcile with unchanged facts emits none.

## 6. Pin the released library

- [ ] 6.1 Remove the `replace` directive and bump `github.com/open-platform-model/library` in `go.mod` to the published alpha carrying both `one-api-tier` and `cue-owned-verdicts`, then `go mod tidy`; verify `make test` and `make build` are green with no `replace` directive present and `openspec validate migrate-kernel-api-and-verdicts` passes.
