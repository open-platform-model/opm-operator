## Why

Library `v1.0.0-beta.6` ships the first packages of the Kubernetes tier (library ADR-011, 0012:D3): `opm/k8s/labels` (the OPM label vocabulary and `IsOPMManagedBy`) and `opm/k8s/object` (the `Resource` wrapper over the kernel's compiled output, its JSON and unstructured conversion, one bulk `Export`, the duplicate-identity check, and the kind-class weight table with `Sort` and `Stages`). The operator still carries its own copy of the first two in its exported `pkg/core` (`labels.go`, `resource.go`, `convert.go`, `compiled_adapter.go`), byte-identical in content to what the library now owns. 0012:D3:R6 says a Kubernetes frontend that adopts a tier package carries no copy of its decisions and no alias from that release on, and its checks refuse a reintroduced copy. The cli deletes its copy in the same library release (its own change, `adopt-kubernetes-object-packages` in the cli).

This change ships only that deletion; the apply order does not change. The other half, applying in the library's kind-class order (0012:D4:R3, 0012:D5:R1), follows after a library change aligns its weight table with Flux's wherever Flux defines an order: library `v1.0.0-beta.6` applies webhook configurations before custom resources and places LimitRange, ResourceQuota, the class kinds, StorageClass, PodDisruptionBudget and CronJob differently from Flux v0.77.0 (the library's `opm/k8s/object/flux_order_test.go`). The operator then keeps Flux's `ApplyAllStaged` and pins, with a test, that Flux's staged order never contradicts the library's.

## What Changes

- **`pkg/core` is deleted** (all four files and their tests). The seven non-test importers (`cmd/main.go`, `internal/apply/prune.go`, `internal/inventory/entry.go`, `internal/reconcile/moduleinstance.go`, `internal/render/kernel_module_renderer.go`, `internal/render/module.go`, `internal/status/digests.go`), the unit tests under `internal/` and every importer under `test/integration/` switch to `opm/k8s/labels` and `opm/k8s/object`. `core.ResourceFromCompiled` becomes `object.NewResource` (or `object.Resources` for the whole set). Label constants map one to one (`LabelManagedBy` to `labels.ManagedBy`, `LabelManagedByControllerValue` to `labels.ManagedByController`, `LabelComponentName` to `labels.ComponentName`, `LabelModuleInstanceUUID` to `labels.ModuleInstanceUUID`, and so on).
- **One export per resource through the library.** `convertRender` (`internal/reconcile/converted.go`) calls `object.Export` once over the rendered set and takes both the render digest and the unstructured apply copies from its result; `status.RenderDigestJSON` over `[]*core.Resource` is replaced by a digest over the exported set. The digest's bytes do not change (a golden test pins it on the unchanged code first), so no instance re-applies because of the move. Both failure reasons and their messages stay as they are.
- **Duplicate check: nothing left.** The renderer's duplicate-identity check already uses `object.Duplicates` (opm-operator#248, `kernel_module_renderer.go:235` and `resolution.go:84`).
- **Lint.** `.golangci.yml` enables `depguard` with a rule that refuses `github.com/open-platform-model/opm-operator/pkg/core`, so a reintroduced copy cannot be imported.

Out of scope: the apply order (above), the operator's delete and prune order (the deletion-protocol change moves both onto the library), the inventory and digest packages of the tier (`opm/k8s/inventory`, its own change), the apply guard (`opm/k8s/ownership`), and removing the library's deprecated `opm/helper/objectset` (a later library release).

## Migration note

The PR body carries this paragraph, and the squash commit body carries it as its `BREAKING CHANGE:` footer.

**BREAKING CHANGE:** the exported package `github.com/open-platform-model/opm-operator/pkg/core` is deleted. No importer outside this repository is known. An importer moves to the library: `Resource`, `ResourceFromCompiled` (now `object.NewResource`), `MarshalJSON` and `ToUnstructured` are in `github.com/open-platform-model/library/opm/k8s/object`; the label constants and `IsOPMManagedBy` are in `github.com/open-platform-model/library/opm/k8s/labels`, with the `Label` prefix and `Value` suffix dropped (`LabelManagedByControllerValue` is `labels.ManagedByController`, `LabelManagedByValue` is `labels.ManagedByCLI`, `LabelManagedByLegacyValue` is `labels.ManagedByLegacy`).

## Release notes (user-visible changes)

None beyond the Go API removal. No CRD, status field, condition, reason, message or apply order changes.

## Classification

**MAJOR** after GA (an exported package is removed); pre-GA it ships as the next `-beta.N` under a `feat!:` title. Complexity (Principle VII): about 400 lines of operator code and tests go, and nothing is added beyond one lint rule.

## Capabilities

### New Capabilities

- `kubernetes-tier-adoption`: the operator uses the library's Kubernetes object and label packages, keeps no copy of them, and its lint refuses a reintroduced one.

### Modified Capabilities

- `kernel-module-renderer`: "Adapt compiled output to operator resources" names the library's `object.Resource`.
- `prune-stale-resources`: "Live-state UUID-based ownership guard" names the library's `labels.IsOPMManagedBy`.
- `digest-computation`: "Render digest computation" is computed from the one export, with its bytes pinned.
- `inventory-bridge`: "Component label constant", "CLI packages copied to `pkg/`" and "Process file remains in pkg/render (revised)" are removed (the key and the types come from the library, and no `pkg/` tree is left); "Inventory type alias preserved" gains a scenario.

## Impact

- Code: `pkg/core/` (deleted), `cmd/main.go`, `internal/apply/prune.go`, `internal/inventory/entry.go`, `internal/reconcile/{converted,moduleinstance}.go`, `internal/render/{kernel_module_renderer,module}.go`, `internal/status/digests.go`, `.golangci.yml`.
- Tests: about twenty files under `internal/` and `test/integration/` change their imports; new tests pin the digest bytes and the conversion messages.
- Dependencies: none. opm-operator#248 already moved to library `v1.0.0-beta.6` and swapped `opm/helper/objectset` for `opm/k8s/object`; this change builds on opm-operator#249, #251 and #252 and changes no pin.
- Downstream: the cli deletes its own `pkg/core` and `pkg/resourceorder` in its adoption change for the same library release. No cli code imports the operator's `pkg/core`.
- Enhancement: `enhancement.yaml` declares 0012 with no decision claimed. 0012:D3:R6 holds only once both frontends have adopted; 0012:D4:R3 and 0012:D5:R1 wait for the apply-order change, and the operator's delete order is still its own.
