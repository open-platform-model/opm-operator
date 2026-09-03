## Why

`operator-platform-module-generation` (PR 118) writes the platform CUE module per Platform generation and records it in the store, but the render paths still read the materialize-era slot nothing writes any more, so every ModuleInstance and ModulePackage reports `PlatformNotReady`. The library's `library-render-cutover` deletes `Compile`, `Materialize` and `SynthesizePlatform` and re-pins core to `alpha.7`; the operator cannot take that release without switching its render path. This change is `operator-render-switch`, the operator's last 0019 Phase B slice: renders go through `Kernel.Render` against the generated module (D9), the held `*MaterializedPlatform` slot and the process-wide kernel serialisation retire (D8), the catalog-skew policy gets its operator surface (D7/D18), and renders may run concurrently under a memory-sized pool.

## What Changes

- **Render path**: `KernelModuleRenderer` and `KernelPackageRenderer` render through `Kernel.Render` with the instance's staged source and the store's generated platform. The package renderer acquires the instance with the source-carrying loader (`AcquireInstanceFromDir`); kind detection keeps riding on the loader's wrong-kind sentinel.
- **Store**: the `*MaterializedPlatform` slot (`Set`/`Get`) is removed; the generated-module record is the only platform record and the `PlatformNotReady` gate reads it. Readers take a **lease** on the record for the duration of a render; the platform reconciler prunes superseded module directories only when no lease holds them. The kernel gate (`AcquireKernel`) narrows to the context-owning kernel calls (module acquisition, instance synthesis, on-disk acquisition, the platform build); `Render` runs outside it.
- **Catalog skew policy (D7/D18)**: `Platform.spec.skewPolicy` (`Warn` default, `Refuse`) is the operator's surface; the Platform reconciler records it beside the generated module and every render passes it. Under `Refuse`, a module requiring a newer catalog build than the platform carries fails with `Ready=False`, reason `SkewRefused`, naming the path and both versions. Under `Warn` the render proceeds and the skew is reported (below). **API change**: one optional enum field on `PlatformSpec`; CRD regenerated.
- **Render warnings**: warnings the build reports (skew under `Warn`, unhandled optional traits) are emitted as Warning events on the ModuleInstance or ModulePackage when they first appear or change; they no longer disappear. The resolved-versions rows (D18, plain data) are logged at debug level.
- **Concurrency**: a `--max-concurrent-renders` manager flag (default 1) sets `MaxConcurrentReconciles` on the ModuleInstance and ModulePackage controllers. Renders share nothing (D8), so the bound is memory, documented from 0019's measurements.
- **Error classification**: `RenderError`'s typed causes map onto the existing reasons: unresolved demands and unmatched components stay `ResolutionFailed`; transform failures and over-subscribed providers are `RenderFailed`; a refused skew is `SkewRefused`. `compile.UnmatchedComponentsError` becomes `oerrors.UnmatchedComponentsError`.
- **Library pin**: bumps to the `library-render-cutover` release (the first with `Render` as the sole path and core `alpha.7` as the default schema). The interim materialize-based integration specs and their alpha.6 schema pin are deleted; they re-seed the store from a generated module built through the kernel.
- **Retired**: the materialize-era reasons and prose that `operator-platform-module-generation` deferred here (`platform-reconciler`, `platform-gated-rendering`, `platform-crd` live specs; the `internal/render` and `internal/reconcile` comments naming materialization as the recovery trigger).

Out of scope: a `status.warnings` ledger on the workload CRs (events first; a bounded status list is a follow-up if events prove insufficient); relocating the single-provider guard to platform-package generation (0015); any change to the generated module's content or lifecycle beyond the lease.

## Sequencing

Gates on `library-render-cutover` PR 2 being released: `Render` exists since alpha.24, but pinning that release would leave the deleted symbols compiling for one release and break on the next bump, and the cutover's shape gate (a subscription-shaped platform is refused at acquisition) is the contract these specs assume. Proposal and design are written against the cutover's stated public surface; implementation starts on the release. e2e turns green with this change and is its exit criterion.

## SemVer classification

Operator releases by image under conventional commits; `feat` (a new CR field, a new manager flag) with one user-visible contract change: what `Ready` means for a ModuleInstance under catalog skew. No breaking change: the new field is optional with the permissive default, existing CRs reconcile unchanged.

## Affected API types and controllers

- API: `PlatformSpec.SkewPolicy` (optional enum, default `Warn`); CRD regenerated.
- Controllers: `ModuleInstanceReconciler` and `ModulePackageReconciler` (concurrency option, warning events, reason mapping); `PlatformReconciler` (records the policy, prunes under leases).
- Internal: `internal/render` (both renderers), `internal/platform` (store reshaped), `internal/platformmodule` (prune honours leases), `internal/reconcile/resolution.go` (classification), `internal/status` (new reason), `cmd/main.go` (flag, controller options).
- Fixtures/tests: integration specs re-seed the store from a generated platform module; e2e podinfo and lifecycle specs become the acceptance run.

## Complexity justification

The change removes more than it adds: the materialized slot, the process-wide render serialisation and one whole render pipeline leave; what arrives is one enum field, one flag, one lease counter and a reason mapping, each named by a 0019 decision (D7, D8, D18) rather than invented here.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `platform-reconciler`: the reconciler records the generated module (not a materialized platform) plus the skew policy; the materialize-era requirements are replaced by the generate-and-build contract `operator-platform-module-generation` established; superseded directories are pruned only when unleased.
- `platform-gated-rendering`: the gate reads the generated-module record; renders lease it; the platform is consumed as a module through the single-build render.
- `platform-crd`: `spec.skewPolicy` is added; the status prose names the generate-and-build reasons.
- `kernel-module-renderer`: renders through the single-build render with the instance's staged source and the leased platform; warnings are returned, not dropped.
- `modulepackage-kernel-rendering`: the package renderer acquires a source-carrying instance and renders through the single-build render; classification gains the skew-refusal and over-subscription causes.
- `library-kernel-runtime`: the concurrency contract becomes shares-nothing renders behind a narrowed kernel gate; `--max-concurrent-renders` bounds concurrent reconciles; the compile-semantics requirement is restated against `Render`.
- `status-conditions`: the `SkewRefused` reason is added.
- `events-emission`: render warnings are emitted as Warning events on transition.

## Impact

- `internal/render/kernel_module_renderer.go`, `internal/render/kernel_package_renderer.go`, `internal/render/renderer.go` (warnings on the result stay; the reconcilers now read them), `internal/platform/store.go` (slot removed, lease added), `internal/platformmodule/layout.go` (prune with a keep set from leases), `internal/controller/platform_controller.go`, `internal/controller/moduleinstance_controller.go`, `internal/controller/modulepackage_controller.go`, `internal/reconcile/resolution.go`, `internal/reconcile/moduleinstance.go`, `internal/reconcile/modulepackage.go`, `internal/status/conditions.go`, `api/v1alpha1/platform_types.go` (+ generated CRD and deepcopy), `cmd/main.go`, `go.mod`.
- Tests: `test/integration/reconcile/{kernel_module_renderer,kernel_package_renderer,example_modules,platform_gate,registry_helpers}_test.go`, `internal/platform/store_test.go`, controller unit specs for the new reason and events, e2e as the acceptance run.
- Docs: `docs/` operator notes on render concurrency and memory sizing; `CLAUDE.md` registry section's mention of the kernel gate.
- `enhancement.yaml` declares 0019 D7, D8 and D18.
