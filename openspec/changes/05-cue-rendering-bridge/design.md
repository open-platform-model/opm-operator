## Context

The CLI rendering pipeline flow is:
1. `loader.LoadModulePackage(ctx, dir)` → `cue.Value` (raw module)
2. Extract module metadata and `#config` schema from the value
3. `module.ParseModuleRelease(ctx, spec, mod, values)` → `*module.Release`
4. `loader.LoadProvider(name, providers)` → `*provider.Provider`
5. `module.Process(ctx, rel, provider)` → `*render.ModuleResult`

The controller's inputs differ from the CLI's: the controller has a directory path (from artifact extraction) and CRD `spec.values` (JSON bytes), not a release file. The bridge must adapt these inputs.

Note: In the CLI, step 5 was `render.ProcessModuleRelease`. In change 1, this function was relocated to `pkg/module/process.go` and renamed to `Process` (design decision 6).

## Goals / Non-Goals

**Goals:**
- Provide a `RenderModule` function in `internal/render` that takes a directory path and CRD values, and returns rendered resources plus inventory entries.
- Reuse locally copied packages (`loader.LoadModulePackage`, `module.ParseModuleRelease`, `module.Process`) without modification.
- Convert CRD `RawValues` (JSON) to `cue.Value` for the CLI pipeline.
- Inject controller-specific `#runtimeLabels` (managed-by: `opm-controller`).

**Non-Goals:**
- Bundle rendering (deferred per scope doc).
- Provider loading — handled by change 4 (catalog-provider-loading). `RenderModule` receives a pre-loaded provider.
- CUE module caching across reconciliations.

## Decisions

### 1. Single entry point: `RenderModule`

`RenderModule(ctx, moduleDir string, values *v1alpha1.RawValues, prov *provider.Provider) (*RenderResult, error)` is the sole public function. It encapsulates the CLI rendering pipeline. The provider is supplied by the caller (loaded from the controller-owned catalog in change 4). Callers don't need to know about CUE internals beyond providing the provider.

### 2. Values conversion via cue.Context.CompileBytes

Convert `RawValues.Raw` (JSON bytes) to a `cue.Value` using `cueCtx.CompileBytes(raw, cue.Filename("values"))`. This preserves the CUE error reporting chain if values are invalid.

### 3. Provider received as parameter

In the CLI, providers come from user config files. In the controller, the provider is loaded from the controller-owned catalog (change 4) and passed to `RenderModule` as a parameter. The render bridge does not load or discover providers itself — it receives a `*provider.Provider` from the caller.

### 4. RenderResult carries both resources and inventory entries

`RenderResult` contains `Resources []*core.Resource` and `InventoryEntries []v1alpha1.InventoryEntry` (converted via the change-1 bridge). This gives the caller everything needed for apply + inventory in one call.

## Risks / Trade-offs

- **[Risk] CUE evaluation performance** — CUE evaluation can be slow for large modules. Mitigation: acceptable for v1alpha1 POC; profile and optimize if needed later.
- **[Risk] Provider compatibility** — The catalog provider version must be compatible with the module's component schemas. Mitigation: both use `opmodel.dev@v1`; version compatibility is enforced during catalog publishing.
- **[Risk] Runtime label injection** — The controller must inject `#runtimeLabels` with `managed-by: opm-controller` before rendering. This must happen at the right point in the pipeline. Mitigation: inject via `cue.Value.FillPath` before calling `module.Process`.
