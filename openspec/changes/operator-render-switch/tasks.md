# Tasks: operator-render-switch

Gate: task 1.1 (the library pin) needs the `library-render-cutover` release; nothing below builds before it.

## 1. Pin and API

- [ ] 1.1 `go.mod`: bump `github.com/open-platform-model/library` to the render-cutover release (`fix(deps)`); `go build ./...` lists every removed-symbol hit; keep the list for the PR description.
- [ ] 1.2 `api/v1alpha1/platform_types.go`: `SkewPolicy *string` with enum `Warn;Refuse`, doc comment stating the default and D7/D18; `task dev:manifests dev:generate`; a `crdvalidation` integration case for the rejected value and the unset default.
- [ ] 1.3 `internal/status/conditions.go`: `SkewRefusedReason`, `RenderWarningReason`; retire nothing else.

## 2. Store and lifecycle

- [ ] 2.1 `internal/platform/store.go`: remove the materialized slot (`Set`/`Get`, the `materialize` import); `Generated` gains `Skew`; add `Lease() (Generated, func(), bool)` with a per-generation counter and `Leased() []int64`; keep `AcquireKernel` with its doc narrowed to the context-owning calls. Unit tests: lease counting, release, leased generations across a swap.
- [ ] 2.2 `internal/controller/platform_controller.go`: record the resolved skew policy; build the prune keep set from the current generation plus `Store.Leased()`; unit spec: a leased previous generation survives the swap and is pruned on the next reconcile once released.

## 3. Render path

- [ ] 3.1 `internal/render/kernel_module_renderer.go`: lease the record, acquire + synthesize under the gate, release the gate, `Kernel.Render` with the instance, the leased platform, the runtime name and the record's skew; adapt `Compiled`; return `Warnings`.
- [ ] 3.2 `internal/render/kernel_package_renderer.go`: `AcquireInstanceFromDir` (kind detection via the wrong-kind sentinel), the same lease/gate/render shape as 3.1.
- [ ] 3.3 `internal/reconcile/resolution.go` and the two reconcile files: classification against `RenderError` (`ResolutionFailed`, `SkewRefused`, `RenderFailed`); `oerrors.UnmatchedComponentsError`; warning events on transition with an in-memory per-object set dropped on deletion; `ResolvedVersions` logged at `V(1)`.
- [ ] 3.4 `cmd/main.go`: `--max-concurrent-renders` (default 1) applied to the ModuleInstance and ModulePackage controllers via `WithOptions`; help text with the sizing rule.

## 4. Tests

- [ ] 4.1 `test/integration/reconcile/registry_helpers_test.go`: replace `materializeKernel`/`materializeSchemaModule` with a helper that generates, builds and records a platform module (`platformmodule.Generate` + `Closure` + `Layout`, `AcquirePlatformFromDir`, `SetGenerated`); migrate `kernel_module_renderer`, `kernel_package_renderer`, `example_modules` and `platform_gate` specs onto it.
- [ ] 4.2 Controller unit specs: `SkewRefused` classification (a fixture module pinning a newer catalog than the platform, policy `Refuse`), `Warn` renders with one `RenderWarning` event and no repeat on the next reconcile, over-subscription surfaces as `RenderFailed`.
- [ ] 4.3 Concurrency: an integration spec with `MaxConcurrentReconciles: 2` rendering two instances, both Ready; `task dev:test` under `-race` for `internal/render` and `internal/platform`.
- [ ] 4.4 `task dev:e2e` green: podinfo, lifecycle and redis specs pass against the generated platform (the acceptance run for the wave).

## 5. Docs and cleanup

- [ ] 5.1 Retire the deferred prose: `internal/render` and `internal/reconcile` comments naming materialization as the recovery trigger; `CLAUDE.md` kernel-gate sentence; `docs/` note on render concurrency and memory sizing (0019 `06-operational.md` numbers).
- [ ] 5.2 `task dev:fmt dev:vet dev:test dev:lint` green; `task operator:installer` regenerated; PR description lists the removed-symbol hits from 1.1 and the deleted specs with their replacements.
