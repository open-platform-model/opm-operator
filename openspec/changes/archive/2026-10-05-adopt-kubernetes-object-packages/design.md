## Context

Line numbers are at `eeba6f5` (opm-operator#250, after #249, #251 and #252); re-check them before editing.

**pkg/core.** `pkg/core/{labels,resource,convert,compiled_adapter}.go` (223 lines, plus three test files) are the operator's copy of what library `v1.0.0-beta.6` ships as `opm/k8s/labels` and `opm/k8s/object`. Method sets and behaviour match: `Resource{Value, Instance, Component, Transformer}` with `Kind`, `Name`, `Namespace`, `APIVersion`, `GVK`, `Labels`, `Annotations`, `String`, `MarshalJSON` and `ToUnstructured`; `IsOPMManagedBy` accepts `opm-cli`, `opm-controller` and the legacy `open-platform-model`. `ResourceFromCompiled(c)` is `object.NewResource(c)` (nil in, nil out in both). Importers: `cmd/main.go` (`RuntimeName: core.LabelManagedByControllerValue` at :341 and :366), `internal/apply/prune.go:99-107`, `internal/inventory/entry.go:15`, `internal/reconcile/moduleinstance.go:1285`, `internal/render/kernel_module_renderer.go:239-242`, `internal/render/module.go:79-82`, `internal/status/digests.go:79-88`; 20 test files outside `pkg/core` (`grep -rln opm-operator/pkg/core`). opm-operator#248 already swapped `opm/helper/objectset` for `opm/k8s/object` in `resolution.go` and `kernel_module_renderer.go`, and the duplicate-identity check already calls `object.Duplicates` (`kernel_module_renderer.go:235`, `resolution.go:84`); no `objectset` import is left and nothing of the duplicate check is left to move.

**Conversion.** `convertRender` (`internal/reconcile/converted.go:40-55`) calls `status.RenderDigestJSON(result.Resources)`, which exports each resource once, then decodes those bytes to unstructured; it runs inside the render slot and drops `result.Resources` on every exit (the memory bound of `library-kernel-runtime`). A marshal failure is `RenderFailedReason` with step "computing render digest"; a decode failure is `ApplyFailedReason` with step "converting resources". `object.Export` does the same single export and decode and reports which step failed (`ExportMarshal`, `ExportDecode`) on an `*object.ExportError`.

`buildInventoryEntries` (`internal/render/module.go:79`) exports each resource a second time through `ToUnstructured`, inside `resultFromRender`. That second export is the inventory change's to remove (it builds entries from the exported objects); this change only renames its types.

**Apply.** `apply.Apply` (`internal/apply/apply.go:64-77`) runs `rm.ApplyAllStaged(ctx, resources, opts)` under `applyWithDiscoveryRetry`, with one whole-set dry-run per Flux stage. This change leaves it exactly as it is: the apply order does not change.

The other half of the adoption, applying in the library's kind-class order (0012:D4:R3, 0012:D5:R1), follows in a later change. Library `v1.0.0-beta.6`'s weight table disagrees with Flux v0.77.0's on several kinds (the library's `opm/k8s/object/flux_order_test.go` lists them): it applies webhook configurations before custom resources, which breaks a first install of a module whose webhook has `failurePolicy: Fail` (cert-manager), and it places LimitRange, ResourceQuota, PriorityClass, RuntimeClass, the other class kinds, StorageClass, PodDisruptionBudget and CronJob differently. A library change first aligns its table with Flux wherever Flux defines an order. The operator then keeps `ApplyAllStaged` and pins the rule "an engine's staging may refine the library order and never contradict it" with a test that Flux's staged order never contradicts the library's.

Reconcile phase impact: Render (types only), Status (digest computed from the one export, bytes unchanged), Prune (label import only). Source, Apply and Inventory are untouched.

## Goals / Non-Goals

**Goals:**

- No copy of the library's object or label decisions in the operator, and a lint rule that refuses importing one back (0012:D3:R6).
- No change to the apply order, the render digest, the inventory, any condition, reason or message.

**Non-Goals:**

- Apply order (0012:D4:R3): follows after the library weight change described in Context.
- Delete and prune order (moves with the deletion protocol).
- `opm/k8s/inventory`, `opm/k8s/ownership`, `opm/k8s/health` adoption.

## Research & Decisions

### D1. Digest bytes and conversion messages stay the same

**Context**: `lastAppliedRenderDigest` gates no-op detection; a changed digest makes every instance re-apply once after the upgrade, and the cli compares digests for handoff.
**Decision**: `status.RenderDigest` becomes a function over `[]object.Exported`: it sorts by the exported object's group, kind, namespace and name (the order `RenderDigestJSON` used, read then from the CUE value) and hashes each `JSON` in that order. The key reads `GetKind`, `GetNamespace` and `GetName`, and the group by the old split of `GetAPIVersion` at its last `/`, not through `GroupVersionKind()`, which returns an empty GVK (kind included) for an apiVersion it cannot parse. A golden test, written and green on the unchanged code first, pins the digest of a fixed resource set to a literal. `convertRender` maps an `*object.ExportError` back to today's reasons and messages: `ExportMarshal` is `RenderFailedReason`, "computing render digest: render digest: <cause>"; `ExportDecode` is `ApplyFailedReason`, "converting resources: converting <resource> to unstructured: <cause>".
One intended change: a value that exports to JSON `null` used to become an `Unstructured` with a nil `Object` and fail later; `object.Export` now fails it at `ExportDecode` with the cause "the exported JSON is not an object", under the same reason and message prefix.
**Rationale**: The digest reads the same bytes in the same order; only where the sort key comes from changes, and the exported object holds the same apiVersion, kind, namespace and name as the CUE value. Keeping the messages keeps the conditions users see unchanged.

### D2. Lint refuses the old path

**Decision**: `.golangci.yml` enables `depguard` with one rule, `no-local-kubernetes-tier-copy`, `list-mode: lax`, over all files, denying `github.com/open-platform-model/opm-operator/pkg/core` with a description naming `opm/k8s/object` and `opm/k8s/labels`.
**Rationale**: 0012:D3:R6 asks the frontend's own checks to refuse a reintroduced copy. The package path is what a copy would be reintroduced under; a copy under a new path is left to review.

## Risks / Trade-offs

- **Go API removal.** An importer of `pkg/core` outside this repository breaks at compile time; none is known, and the migration note names the replacements.
- **Digest bytes.** A changed digest would re-apply every instance once after the upgrade; the golden test recorded on the old code pins them.

## Open Questions

- Release: an operator `feat!` release makes the `module/operator-image` PR `fix(deps)!`, a breaking (0.x minor) `opm_operator` module release (AGENTS.md, "This repository releases two units").

## Sections

1. Library object and label packages replace `pkg/core` (types, conversion through `object.Export`, digest, tests, lint). No behaviour change.
2. Verification: e2e on Kind under the lock with `LOCAL_REGISTRY`, the docs bundle, OpenSpec validation.
