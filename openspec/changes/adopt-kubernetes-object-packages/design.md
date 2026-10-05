## Context

Line numbers are at `eeba6f5` (opm-operator#250, after #249, #251 and #252); re-check them before editing.

**pkg/core.** `pkg/core/{labels,resource,convert,compiled_adapter}.go` (223 lines, plus three test files) are the operator's copy of what library `v1.0.0-beta.6` ships as `opm/k8s/labels` and `opm/k8s/object`. Method sets and behaviour match: `Resource{Value, Instance, Component, Transformer}` with `Kind`, `Name`, `Namespace`, `APIVersion`, `GVK`, `Labels`, `Annotations`, `String`, `MarshalJSON` and `ToUnstructured`; `IsOPMManagedBy` accepts `opm-cli`, `opm-controller` and the legacy `open-platform-model`. `ResourceFromCompiled(c)` is `object.NewResource(c)` (nil in, nil out in both). Importers: `cmd/main.go` (`RuntimeName: core.LabelManagedByControllerValue` at :341 and :366), `internal/apply/prune.go:99-107`, `internal/inventory/entry.go:15`, `internal/reconcile/moduleinstance.go:1285`, `internal/render/kernel_module_renderer.go:240-243`, `internal/render/module.go:79-82`, `internal/status/digests.go:79-88`; 22 test files (`grep -rln opm-operator/pkg/core`). opm-operator#248 already swapped `opm/helper/objectset` for `opm/k8s/object` in `resolution.go` and `kernel_module_renderer.go`; no `objectset` import is left.

**Conversion.** `convertRender` (`internal/reconcile/converted.go:40-55`) calls `status.RenderDigestJSON(result.Resources)`, which exports each resource once, then decodes those bytes to unstructured; it runs inside the render slot and drops `result.Resources` on every exit (the memory bound of `library-kernel-runtime`). A marshal failure is `RenderFailedReason` with step "computing render digest"; a decode failure is `ApplyFailedReason` with step "converting resources". `object.Export` does the same single export and decode and reports which step failed (`ExportMarshal`, `ExportDecode`) on an `*object.ExportError`.

`buildInventoryEntries` (`internal/render/module.go:79`) exports each resource a second time through `ToUnstructured`, inside `resultFromRender`. That second export is the inventory change's to remove (it builds entries from the exported objects); this change only renames its types.

**Apply.** `apply.Apply` (`internal/apply/apply.go:64-77`) runs `rm.ApplyAllStaged(ctx, resources, opts)` under `applyWithDiscoveryRetry`. Both reconcilers call it (`moduleinstance.go:558`, `modulepackage.go:686`). `ApplyAllStaged` (ssa v0.77.0 `manager_apply.go:337-420`) splits the set into cluster definitions (CRD, Namespace, ClusterRole), class definitions (kind suffix `Class`), custom-stage kinds (the operator sets none) and the rest; it waits with `WaitForSet` after the first two. Each stage goes through `ApplyAll`, which first calls `sort.Sort(SortableUnstructureds(objects))` on the slice it is given (`:207`, in place), then dry-runs and applies object by object (default concurrency 1).

Library `object.Stages(items, gvkOf)` (`opm/k8s/object/stages.go`) returns a sorted copy cut into stages: one `ClusterDefinitions` stage holding every CRD and core Namespace (weights -100 and 0), then one stage per distinct `object.Weight`, ascending; no empty stage, input not reordered. Each stage's `Items` is a capacity-limited subslice, so an in-place sort of one stage cannot reach another. Its doc comment states the operator rule this change implements. The library's `flux_order_test.go` records every pair the two tables order oppositely.

Reconcile phase impact: Apply (order and stage calls), Render (types only), Status (digest computed from the one export, bytes unchanged), Prune (label import only). Source and Inventory are untouched.

## Goals / Non-Goals

**Goals:**

- No copy of the library's object or label decisions in the operator, and a lint rule that refuses importing one back (0012:D3:R6).
- The operator submits objects in the library's order, and Flux can only refine it (0012:D4:R3, 0012:D5:R1).
- No change to the render digest, the inventory, any condition, reason or message.

**Non-Goals:**

- Delete and prune order (moves with the deletion protocol).
- `opm/k8s/inventory`, `opm/k8s/ownership`, `opm/k8s/health` adoption.
- Changing the library weight table. Where the operator now applies a kind later than Flux did, that is the library's order; changing it is a library change.

## Research & Decisions

### D1. One `ApplyAll` per library stage

**Context**: The owner rule is that the operator sorts by library weights and hands the set to Flux, whose staging only refines. Flux's `ApplyAll` re-sorts every call with its own table, so a library pre-sort followed by one `ApplyAllStaged` is overwritten inside each Flux stage: the literal reading ships a no-op and still applies Deployments before PersistentVolumeClaims.
**Explored**: (A) pre-sort and call `ApplyAllStaged` (no effect inside a stage); (B) `ApplyAllStaged` with `CustomStageKinds` (gives one extra stage, not one per weight); (C) one `ApplyAll` per `object.Stages` stage; (D) call `rm.Apply` per object in library order (drops Flux's set-level dry-run and change set).
**Decision**: (C). `Apply` builds `stages := object.Stages(resources, gvkOf)` and, for each stage in order, calls `rm.ApplyAll(ctx, stage.Items, opts)`, appends its entries to one `ChangeSet`, and after the `ClusterDefinitions` stage calls `rm.WaitForSet(cs.ToObjMetadataSet(), WaitOptions{Interval: opts.WaitInterval, Timeout: opts.WaitTimeout})`. A failure returns the change set so far and the error, as `ApplyAllStaged` does.
**Rationale**: Flux stays the engine: dry-run, drift skip, force recreate, field manager and change-set accounting are unchanged. Inside one call every object has the same library weight (or is a cluster definition), so Flux's sort can order them among themselves and never invert the library order. This is the owner's "Flux staging only refines" made literal.

```go
func applyStaged(ctx context.Context, rm *fluxssa.ResourceManager,
	resources []*unstructured.Unstructured, opts fluxssa.ApplyOptions) (*fluxssa.ChangeSet, error) {
	changeSet := fluxssa.NewChangeSet()
	for _, stage := range object.Stages(resources, (*unstructured.Unstructured).GroupVersionKind) {
		cs, err := rm.ApplyAll(ctx, stage.Items, opts)
		if cs != nil {
			changeSet.Append(cs.Entries)
		}
		if err != nil {
			return changeSet, err
		}
		if stage.ClusterDefinitions {
			if err := rm.WaitForSet(cs.ToObjMetadataSet(), fluxssa.WaitOptions{
				Interval: opts.WaitInterval, Timeout: opts.WaitTimeout}); err != nil {
				return changeSet, err
			}
		}
	}
	return changeSet, nil
}
```

### D2. Waits: the cluster-definition stage only

**Context**: `ApplyAllStaged` also waited for ClusterRoles (in its first stage) and for class kinds (its second stage). Under the library order a ClusterRole has weight 5 and most class kinds the default weight, so they land in later stages.
**Explored**: (A) wait after every stage that holds a kind Flux waited for; (B) wait only after the `ClusterDefinitions` stage, as the library's `Stage` contract describes.
**Decision**: (B).
**Rationale**: A ClusterRole and the built-in class kinds carry no status that kstatus waits on: `WaitForSet` reports them `Current` as soon as they exist, so those waits were immediate. Waiting after a later stage would add a poll round trip for nothing. CRDs (until `Established`) and Namespaces are what the following stages need.

### D3. The discovery retry wraps the whole staged sequence

**Context**: A CRD can be `Established` before discovery serves its kind. Today the retry re-runs `ApplyAllStaged`.
**Decision**: The `stagedApply` closure runs `applyStaged` (D1); `applyWithDiscoveryRetry` and `pendingCRDKind` are unchanged. A retry re-applies the earlier stages, which Flux skips as unchanged, and the action ledger keeps the first attempt's created and configured counts (`ssa-apply`, "Apply result counts across a discovery retry").
**Rationale**: Same semantics as today, with no second retry mechanism.

### D4. The input slice is not reordered

**Context**: `ApplyAll` sorts its argument in place. `applyList` is also read by drift detection and status after apply.
**Decision**: `Apply` passes only the stage slices `object.Stages` returns, which belong to its sorted copy. A test asserts the caller's slice order is unchanged after `Apply`.

### D5. Partial apply on a failure in a later stage

**Context**: `ApplyAll` dry-runs its whole call before applying any object of it. With one call per weight, a dry-run failure in a later stage (an invalid Deployment) now surfaces after the earlier stages (ConfigMaps, Services) are applied. Under `ApplyAllStaged` the same set failed with only the cluster and class definitions applied.
**Decision**: Accepted. The reconcile reports `ApplyFailed` exactly as before and retries; the objects applied earlier are the instance's own objects, applied again by the next attempt. Nothing is pruned on a failed apply, and the inventory is written only on success, as today.
**Rationale**: `ApplyAll` already applies object by object after its dry-run, so a failure part-way through a call (a webhook refusal, a conflict) already left earlier objects applied. The library order is what 0012:D4:R3 asks for; a whole-set dry-run before any apply is not something Flux's staged apply promised either, since it applies definitions first.

### D6. API calls per object do not change

**Context**: A review of the plan asked whether splitting the apply multiplies dry-run round trips on a large module.
**Decision**: Measure, do not assume. An integration test wraps the apply client with `controller-runtime`'s `interceptor` and counts Get and Patch (dry-run and real) calls for a set of about forty objects across eight weights, on `ApplyAllStaged` (recorded on the unchanged code) and on `applyStaged`. The numbers go into this section.
**Rationale**: `ApplyAll` issues one Get and one dry-run Patch per object, plus one apply Patch per changed object, whichever call it is in; the only extra cost expected is the per-call setup (no API traffic). The measurement confirms it.

### D7. Digest bytes and conversion messages stay the same

**Context**: `lastAppliedRenderDigest` gates no-op detection; a changed digest makes every instance re-apply once after the upgrade, and the cli compares digests for handoff.
**Decision**: `status.RenderDigest` becomes a function over `[]object.Exported`: it sorts by the exported object's group, kind, namespace and name (the order `RenderDigestJSON` used, read then from the CUE value) and hashes each `JSON` in that order. A golden test, written and green on the unchanged code first, pins the digest of a fixed resource set to a literal. `convertRender` maps an `*object.ExportError` back to today's reasons and messages: `ExportMarshal` is `RenderFailedReason`, "computing render digest: render digest: <cause>"; `ExportDecode` is `ApplyFailedReason`, "converting resources: converting <resource> to unstructured: <cause>".
**Rationale**: The digest reads the same bytes in the same order; only where the sort key comes from changes, and the exported object holds the same apiVersion, kind, namespace and name as the CUE value. Keeping the messages keeps the conditions users see unchanged.

### D8. Lint refuses the old path

**Decision**: `.golangci.yml` enables `depguard` with one rule, `no-local-kubernetes-tier-copy`, `list-mode: lax`, over all files, denying `github.com/open-platform-model/opm-operator/pkg/core` with a description naming `opm/k8s/object` and `opm/k8s/labels`.
**Rationale**: 0012:D3:R6 asks the frontend's own checks to refuse a reintroduced copy. The package path is what a copy would be reintroduced under; a copy under a new path is left to review.

## Risks / Trade-offs

- **Library order is later than Flux's for some kinds** (proposal, "Migration note"). PriorityClass and RuntimeClass named by a Pod in the same apply are the visible case: the Pod is refused until the class exists, and the ReplicaSet or StatefulSet controller retries. This is the library's order by owner decision; moving those kinds earlier is a library weight change, which then reaches both frontends.
- **Two tables remain.** A Flux upgrade can change Flux's table; under D1 that can only reorder objects of one library weight. The library's `flux_order_test.go` records the disagreements against ssa v0.77.0.
- **D5** widens the partial-apply window, as described there.

## Sections

1. Library object and label packages replace `pkg/core` (types, conversion through `object.Export`, digest, tests, lint). No behaviour change.
2. Apply in library order (D1-D6), with the order and call-count tests and the doc updates.
3. Verification: e2e on Kind under the lock with `LOCAL_REGISTRY`, the docs bundle, OpenSpec validation.
