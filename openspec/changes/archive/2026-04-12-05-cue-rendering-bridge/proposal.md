## Why

The controller has extracted a CUE module to a temp directory (change 3) and has the user's values from `spec.values`. It now needs to evaluate the CUE module, validate values, and produce rendered Kubernetes resources. The locally copied `pkg/loader`, `pkg/module`, and `pkg/render` packages (from the CLI, copied in change 1) provide this entire pipeline. This change bridges from the controller's inputs (directory path + CRD values) to those entry points.

## What Changes

- Replace the `internal/render` stubs with a `RenderModule` function that orchestrates the CLI rendering pipeline.
- Load the CUE module from the extracted directory via `loader.LoadModulePackage`.
- Convert CRD `spec.values` into a `cue.Value`.
- Call `module.ParseModuleRelease` and `module.Process` (relocated from `render.ProcessModuleRelease` in change 1).
- Return rendered `[]*core.Resource` and build `[]v1alpha1.InventoryEntry` from the result.

## Capabilities

### New Capabilities
- `cue-rendering`: Load a CUE module from a directory, inject values, evaluate via the CLI render pipeline, and produce rendered Kubernetes resources with inventory entries.

### Modified Capabilities

## Impact

- `internal/render/` — stubs replaced with real rendering bridge.
- Heavy use of locally copied packages: `pkg/loader`, `pkg/module`, `pkg/render`, `pkg/core`, `pkg/provider` (copied from CLI in change 1).
- This is where `cuelang.org/go` becomes actively used at runtime.
- Depends on: change 1 (copied CLI packages in `pkg/`), change 3 (artifact extraction provides the directory), change 4 (provider loaded from controller-owned catalog).
- SemVer: MINOR — new capability.
