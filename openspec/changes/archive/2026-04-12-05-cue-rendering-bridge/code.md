## Package: `internal/render`

### Files

| File | Purpose |
|------|---------|
| `module.go` | `RenderModule` entry point and `RenderResult` type (replaces `type ModuleRenderer any` stub). Design decision 1: single entry point. |
| `module_test.go` | Tests with minimal CUE module fixtures |
| `testdata/` | CUE module test fixtures |

### Imports

```go
import (
    "context"
    "fmt"

    "cuelang.org/go/cue"
    "cuelang.org/go/cue/cuecontext"

    "github.com/open-platform-model/poc-controller/pkg/core"
    "github.com/open-platform-model/poc-controller/pkg/loader"
    "github.com/open-platform-model/poc-controller/pkg/module"
    "github.com/open-platform-model/poc-controller/pkg/provider"
    pkgrender "github.com/open-platform-model/poc-controller/pkg/render"

    releasesv1alpha1 "github.com/open-platform-model/poc-controller/api/v1alpha1"
    "github.com/open-platform-model/poc-controller/internal/inventory"
)
```

### Types — `module.go`

```go
// RenderResult holds the output of a successful RenderModule call.
// Contains both the rendered resources and their inventory entries, giving the
// caller everything needed for apply + inventory in one call (design decision 4).
type RenderResult struct {
    // Resources is the ordered list of rendered Kubernetes resources.
    // Each resource carries Component and Transformer provenance from the CLI pipeline.
    Resources []*core.Resource

    // InventoryEntries are the CRD-typed inventory entries built from Resources,
    // converted via the inventory bridge (change 1).
    InventoryEntries []releasesv1alpha1.InventoryEntry

    // Warnings are non-fatal render warnings (e.g., unhandled traits).
    Warnings []string
}
```

### Package Types (read-only reference — used internally)

```go
// loader.LoadModulePackage(ctx *cue.Context, dirPath string) (cue.Value, error)
//   Step 1: loads CUE module from extracted artifact directory.

// module.Module — parsed module with Metadata, Config (#config schema), Raw (cue.Value).
// module.ParseModuleRelease(ctx, spec, mod, values) (*module.Release, error)
//   Step 5: validates values against #config, fills into spec, returns concrete release.

// pkgrender.ProcessModuleRelease(ctx, rel, provider) (*render.ModuleResult, error)
//   Step 6: runs match → execute pipeline, returns ModuleResult with []*core.Resource.
//   Provider comes from caller (loaded at startup via catalog.LoadProvider in change 4).

// render.ModuleResult.Resources []*core.Resource — the rendered output.
// render.ModuleResult.Warnings []string — non-fatal warnings.
```

### Functions — `module.go`

```go
// RenderModule is the single entry point for CUE module rendering in the controller
// (design decision 1). It encapsulates the full CLI pipeline:
//
//   1. loader.LoadModulePackage(cueCtx, moduleDir) → cue.Value
//   2. Extract module metadata and #config schema
//   3. Convert RawValues to cue.Value via cueCtx.CompileBytes (design decision 2)
//   4. Inject #runtimeLabels with managed-by: opm-controller via cue.Value.FillPath
//   5. module.ParseModuleRelease(ctx, spec, mod, []cue.Value{values})
//   6. pkgrender.ProcessModuleRelease(ctx, release, prov) — provider from caller (design decision 3)
//   7. Convert []*core.Resource to []v1alpha1.InventoryEntry via inventory bridge
//
// The provider is loaded from the controller-owned catalog at startup (change 4)
// and passed through the reconciler. RenderModule does not load providers itself.
// The values parameter may be nil if the module has no required config or has defaults.
// Returns *errors.ConfigError (from pkg/errors) if values fail schema validation.
func RenderModule(
    ctx context.Context,
    moduleDir string,
    values *releasesv1alpha1.RawValues,
    prov *provider.Provider,
) (*RenderResult, error)
```

### Runtime Labels Injection

```go
// Runtime labels injected before CUE evaluation (design decision from
// docs/design/runtime-owned-labels-and-ownership-metadata.md):
//
//   #runtimeLabels: {
//       "app.kubernetes.io/managed-by":              "opm-controller"   // core.LabelManagedByControllerValue
//       "module-release.opmodel.dev/namespace":      <release-namespace>
//   }
//
// Injected via: spec = spec.FillPath(cue.ParsePath("#runtimeLabels"), runtimeLabels)
// CUE unification enforces that no module/component label can conflict with these.
```
