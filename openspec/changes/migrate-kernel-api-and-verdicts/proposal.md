## Why

The operator pins `library v1.0.0-alpha.26` and cannot build against `library` main. Three breaking library changes land there in the same alpha:

1. **`one-api-tier`** (merged, unreleased) folded the loader and synth helpers into one API tier. `opm/helper/loader/file` and `opm/helper/synth` no longer exist, no acquire verb takes a per-call `LoadOptions`, `InstanceInput.Values` is a `[]kernel.Source` stack rather than a single `cue.Value`, and the shape-gate sentinels moved to `opm/errors`. Three operator files import a deleted helper package; `go build ./...` against `../library` stops at the import line.
2. **`cue-owned-verdicts`** (in flight in `library`) makes the render build own every verdict and takes the kernel out of the prose business (library Principle IV). `RenderResult.Warnings` is removed, `opm/core` is deleted with `Compiled` moving to `opm/kernel`, and the reshaped `opm/errors` types rename `OverSubscribedContractError` (a value type joined into the gate once per row) to the `OverSubscribedContract` row plus one `*OverSubscribedContractsError` aggregate.

3. **`kernel-owns-no-build-context`** (planned in `library`, slice 6a of the kernel surface cut) takes the `cue.Context` out of the Kernel: every verb builds in a context it creates and releases, `Kernel.CueContext()` is removed, `kernel.Source` carries bytes, `schema.Cache.Get()` takes no argument, and one Kernel is safe for concurrent use across its methods. The operator's kernel gate (`Store.kernelMu` and `AcquireKernel`, `internal/platform/store.go:67-71,162-168`) exists only because that context was shared, and `verifyCoreSchema` (`cmd/main.go:358`) calls the removed accessor.

All three land in the same library alpha, and the operator cannot compile between them: `internal/render/kernel_module_renderer.go` is edited by all three. That is why this is one change rather than three.

The second change is also what the operator's own warning path wanted. `internal/reconcile/warnings.go` emits one Warning event per distinct kernel-authored string, so the event text, the dedup key and the tracker's change detection are all a library string the operator does not control — a wording change in the library silently re-emits every event. With advisory facts arriving as rows (an unhandled-trait table, a resolved-versions row marked newer), the operator words its own events and can key the tracker on the facts instead of on the sentence.

## What Changes

**Acquire surface (`one-api-tier`):**

- **BREAKING (internal)** `internal/controller/platform_controller.go` and `internal/render/kernel_package_renderer.go` drop the `loaderfile.LoadOptions{Registry: ...}` argument from `AcquirePlatformFromDir` and `AcquireInstanceFromDir`. The registry mapping already reaches the kernel once through `kernel.WithRegistry`, so these are pure deletions; the renderers' own `Registry` fields become unused and are removed with them.
- `loaderfile.ErrWrongKind` becomes `liberrors.ErrWrongKind` at the one classification site.
- **BREAKING (internal)** `internal/render/kernel_module_renderer.go` builds a `[]kernel.Source` for `kernel.InstanceInput.Values` instead of a bare `cue.Value`. The operator's raw values become one source whose `Origin` names the CR field they came from, so a values error reports that origin instead of the bare filename `values`.

**Compiled and verdicts (`cue-owned-verdicts`):**

- **BREAKING (internal)** `pkg/core/compiled_adapter.go` reads `*kernel.Compiled` instead of `*librarycore.Compiled`; the field copy is unchanged and the `opm/core` import is dropped.
- **BREAKING (internal)** `render.RenderResult.Warnings` is filled by an operator formatter over `out.Diagnostics.UnhandledTraits` and the `Newer` rows of `out.Diagnostics.ResolvedVersions`, replacing the copy from the deleted `RenderResult.Warnings`. The field keeps its `[]string` shape and every reader is unchanged.
- `internal/reconcile/resolution.go`'s comment naming `oerrors.OverSubscribedContractError` reads `*oerrors.OverSubscribedContractsError`, and `resolution_test.go` constructs the pointer aggregate. `isTypedResolutionError` and `renderFailureReason` keep their behavior: `*UnresolvedDemandsError` and `*UnmatchedComponentsError` keep their names and pointer receivers.

**Kernel context (`kernel-owns-no-build-context`):**

- **BREAKING (internal)** `internal/platform/store.go` loses `kernelMu` and `AcquireKernel`; the three call sites (`internal/controller/platform_controller.go:193`, `internal/render/kernel_package_renderer.go:99`, `internal/render/kernel_module_renderer.go:99`) drop their acquire-and-release. Module acquisition, instance synthesis, on-disk acquisition and the platform build now overlap with each other and with renders, bounded only by controller concurrency (`--max-concurrent-renders` on the render paths).
- `verifyCoreSchema` (`cmd/main.go`) calls `k.SchemaCache().Get()`; the startup smoke check is otherwise unchanged.
- `CLAUDE.md` (the "kernel gate is narrow" bullet) and `docs/RENDERING.md` (steps 1 and 2) describe the new shape: every kernel call shares nothing.

**Dependency:** `go.mod` moves `github.com/open-platform-model/library` to the alpha carrying all three changes. That alpha does not exist yet, so this change is blocked on the `library` release.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `kernel-module-renderer`: the render requirement states that values reach synthesis as a source stack carrying their origin rather than as one compiled value; the adaptation requirement names `kernel.Compiled` as the type adapted and states that the render result's warnings are composed by the operator from the diagnostics' advisory rows rather than returned by the build.
- `library-kernel-runtime`: the single-Kernel requirement drops the kernel gate: acquisition, synthesis and the platform build are no longer serialised, since the Kernel is safe for concurrent use; the overlapping-renders scenario states that acquisition steps overlap too.
- `events-emission`: the render-warning requirement states that the operator words the event text from the advisory rows and keys the transition check on the facts those rows carry, so a library wording change cannot re-emit an unchanged set.

## Impact

**SemVer:** PATCH on the operator's own surface. No CRD field, condition reason, event reason, metric or flag changes. Event *text* for render warnings becomes operator-authored; the facts named are unchanged and pinned by the `events-emission` delta. The library dependency bump is a MAJOR bump of a pre-GA dependency, absorbed here.

**Affected files (3 import a deleted helper package, 4 more read a moved or reshaped type, 7 more lose the kernel gate or its description):**

- `internal/controller/platform_controller.go` — platform acquire.
- `internal/render/kernel_package_renderer.go` — instance acquire, the `ErrWrongKind` classification.
- `internal/render/kernel_module_renderer.go` — `synth.InstanceInput` -> `kernel.InstanceInput`, the values source stack, the `Warnings` producer in `resultFromRender`.
- `pkg/core/compiled_adapter.go` — the `Compiled` import and parameter type.
- `internal/reconcile/warnings.go` — unchanged if the renderer keeps producing strings; the tracker's key is revisited so it is not a library sentence.
- `internal/reconcile/resolution.go` — one comment; `resolution_test.go` — one constructor.
- `internal/platform/store.go` — `kernelMu` and `AcquireKernel` deleted; `internal/controller/platform_controller.go`, `internal/render/kernel_package_renderer.go`, `internal/render/kernel_module_renderer.go` — the acquire-and-release lines.
- `cmd/main.go` — `verifyCoreSchema`.
- `CLAUDE.md`, `docs/RENDERING.md` — the gate's description.

**Tests:** `internal/reconcile/resolution_test.go` constructs the over-subscription cause. Any test asserting the kernel's exact warning wording moves to the operator's wording. The e2e and Flux fixture suites exercise render output, which this change does not alter. An envtest run with `--max-concurrent-renders=2` proves two ModuleInstances acquire and synthesize concurrently once the gate is gone.

**Blocked on:** a `library` release carrying `one-api-tier`, `cue-owned-verdicts` and `kernel-owns-no-build-context`. Until then the work is verifiable only against a local `replace` directive, which is how the tasks stage it.

**Sibling change:** `cli` carries the same migration for its own consumer surface, as a separate change in that repo.
