## Why

The poc-controller's `internal/inventory` package is a set of empty stubs. The CLI already provides production-quality inventory logic (`ComputeStaleSet`, `ComputeDigest`, `IdentityEqual`, `K8sIdentityEqual`, `NewEntryFromResource`) in `pkg/inventory`. Rather than importing the CLI as a Go module dependency and bridging between two structurally identical types, the controller should copy the inventory functions from the CLI and have them operate directly on the CRD's `v1alpha1.InventoryEntry` type. This avoids a transitive CUE dependency, eliminates type conversion boilerplate, and positions the controller repo as the authoritative source for these types going forward.

Additionally, several later changes (04, 05, 07, 10) depend on other CLI `pkg/` packages (`core`, `loader`, `module`, `render`, `provider`, `resourceorder`, etc.). Rather than adding the CLI as a Go module dependency, all relevant CLI packages are copied into the controller's `pkg/` directory. This keeps the controller self-contained and avoids coupling to the CLI's release cycle.

## What Changes

- Copy inventory functions from `cli/pkg/inventory` into `internal/inventory/`, rewritten to operate on `v1alpha1.InventoryEntry` directly.
- Inline the `LabelComponentName` constant from `cli/pkg/core` into the controller.
- Replace the stub `internal/inventory` package with real implementations of `ComputeStaleSet`, `ComputeDigest`, `IdentityEqual`, `K8sIdentityEqual`, and `NewEntryFromResource`.
- Copy CLI `pkg/` packages into the controller's `pkg/` directory: `core`, `errors`, `validate`, `provider`, `module`, `bundle`, `loader`, `render`, `resourceorder`. Update internal import paths from `github.com/opmodel/cli/pkg/` to `github.com/open-platform-model/poc-controller/pkg/`.

## Capabilities

### New Capabilities

- `inventory-operations`: Pure inventory operations (`ComputeStaleSet`, `ComputeDigest`, `IdentityEqual`, `K8sIdentityEqual`, `NewEntryFromResource`) operating directly on CRD types, copied from the CLI.
- `cli-packages`: Locally copied CLI packages in `pkg/` providing CUE rendering, resource ordering, and related types for use by later changes.

### Modified Capabilities

## Impact

- `internal/inventory/` — stubs replaced with real implementations copied from the CLI.
- `pkg/` — new directory containing copied CLI packages (core, errors, validate, provider, module, bundle, loader, render, resourceorder).
- New transitive Go module dependencies: `cuelang.org/go` (required by copied CUE packages).
- SemVer: MINOR — new capability, no breaking changes.
