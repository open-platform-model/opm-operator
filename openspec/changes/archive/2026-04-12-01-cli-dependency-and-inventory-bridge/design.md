## Context

The controller's `internal/inventory` package contains empty stubs (`type Digest struct{}`, `type StaleSet struct{}`). The CLI's `pkg/inventory` package already implements the exact operations the controller needs: `InventoryEntry` type, `IdentityEqual`, `K8sIdentityEqual`, `ComputeStaleSet`, `ComputeDigest`, `NewEntryFromResource`.

The CRD defines its own `v1alpha1.InventoryEntry` with identical fields. The CLI type and CRD type are structurally the same — the only difference is `omitempty` on `Group` and `Namespace` in the CRD type, which affects JSON serialization but not Go-level behavior.

## Goals / Non-Goals

**Goals:**

- Copy inventory functions from the CLI into `internal/inventory/`, rewritten to operate on `v1alpha1.InventoryEntry` directly.
- Inline the `LabelComponentName` constant from the CLI.
- Replace stubs with real implementations of `ComputeStaleSet`, `ComputeDigest`, `IdentityEqual`, `K8sIdentityEqual`, and `NewEntryFromResource`.

**Non-Goals:**

- Adding the CLI as a Go module dependency.
- Wiring inventory operations into the reconcile loop (that's change 11).
- Implementing digest computation beyond inventory digest (that's change 6).

## Decisions

### 1. Copy, don't bridge

Copy the inventory functions from `cli/pkg/inventory` into `internal/inventory/` and rewrite them to operate directly on `v1alpha1.InventoryEntry`. This eliminates the CLI module dependency, avoids the transitive CUE dependency, and removes the need for type conversion functions.

**Alternative considered:** Adding the CLI as a Go dependency and writing a thin bridge layer with `EntryFromCLI` / `EntryToCLI` conversion functions. Rejected because the two types are structurally identical, conversion is pure boilerplate, and the CLI dependency pulls in `cuelang.org/go` transitively. Copying ~100 lines of pure logic is simpler.

**Alternative considered:** Having the CLI import the controller's CRD type. Rejected because it inverts the dependency direction — the CLI is the upstream tool.

### 2. CRD types are authoritative

The copied functions operate on `v1alpha1.InventoryEntry` directly. There is no separate "inventory type" in `internal/inventory`. The controller repo is intended to become the authoritative source for these types going forward.

### 3. Keep existing type alias

The existing `type Current = releasesv1alpha1.Inventory` alias stays — it's used as a semantic marker in other internal packages. New functions are additive.

### 4. Label constant placement

The `LabelComponentName` constant (`component.opmodel.dev/name`) is defined in `pkg/core/labels.go` as the single source of truth for OPM label constants. `internal/inventory/entry.go` imports it from `pkg/core` rather than duplicating it locally.

### 5. Copy all relevant CLI packages to `pkg/`

Several later changes (04, 05, 07, 10) need CLI packages for CUE rendering, resource ordering, and type definitions. Rather than adding the CLI as a Go module dependency, copy the packages into the controller's `pkg/` directory and update internal import paths. This keeps the controller self-contained.

Packages copied (with transitive dependencies):

| Package | Purpose | Internal deps |
|---------|---------|---------------|
| `pkg/core` | `Resource` type, labels, constants | (none) |
| `pkg/errors` | `ConfigError`, `TransformError`, sentinel errors | (none) |
| `pkg/validate` | CUE config validation | `pkg/errors` |
| `pkg/provider` | `Provider` type | (none) |
| `pkg/module` | `Module`, `Release`, `ParseModuleRelease` | `pkg/validate` |
| `pkg/loader` | `LoadModulePackage`, `LoadProvider` | `pkg/provider` |
| `pkg/render` | `ModuleResult`, match/execute/finalize pipeline | `pkg/core`, `pkg/errors`, `pkg/module`, `pkg/provider`, `pkg/validate` |
| `pkg/resourceorder` | `GetWeight` for apply staging | (none) |

`pkg/bundle` was excluded — bundle support is not yet implemented in OPM.

### 6. ProcessModuleRelease remains in `pkg/render/` (revised)

The original plan was to relocate `process_modulerelease.go` to `pkg/module/process.go`. This was **not feasible** due to an import cycle: `pkg/render` already imports `pkg/module` (for `module.Release` in `Execute()`). Moving the process file into `pkg/module` would create a `module → render → module` cycle.

`ProcessModuleRelease` stays in `pkg/render/` with its original name. `finalizeValue` was exported as `FinalizeValue` for use by this orchestrator function and any future external callers. Bundle processing was excluded — not yet implemented in OPM.

All internal `github.com/opmodel/cli/pkg/` imports are rewritten to `github.com/open-platform-model/poc-controller/pkg/`. This adds `cuelang.org/go` as a transitive dependency (required by CUE packages), but avoids coupling to the CLI module itself.

## Risks / Trade-offs

- **[Risk] Code drift from CLI** — The copied packages may diverge from the CLI over time. Mitigation: the controller repo is intended to become the driving repo, so drift is acceptable. For inventory functions specifically, these are small (~100 lines total), pure, and unlikely to change.
- **[Trade-off] `cuelang.org/go` transitive dependency** — The copied CUE packages (loader, module, render, validate, errors) require `cuelang.org/go`. This is acceptable because change 05 needs CUE evaluation at runtime regardless.
- **[Benefit] Self-contained controller** — No coupling to the CLI's release cycle or module structure. The controller can evolve its copies independently.
