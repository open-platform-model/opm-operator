## Context

See `proposal.md` for the motivation. The facts the design rests on, all read at the pins of `go.mod` (`github.com/fluxcd/pkg/ssa v0.77.0`, `github.com/open-platform-model/library v1.0.0-beta.7`):

- `ApplyAllStaged` (ssa `manager_apply.go:347`) puts every object in the first stage whose rule it meets: `utils.IsClusterDefinition`, then `utils.IsClassDefinition`, then `utils.IsCustomStage(o, opts.CustomStageKinds)`, then the rest. It applies the stages in that order, each with `ApplyAll`. The three rules are exported. The precedence between them is written inline in `ApplyAllStaged` and is not exported.
- `ApplyAll` (ssa `manager_apply.go:206`) sorts its objects with `sort.Sort(SortableUnstructureds(objects))`, dry-runs them concurrently and then writes them one after the other in the sorted order. `SortableUnstructureds`, `IsLessThan` and the `ReconcileOrder` variable are exported (ssa `sort.go`). The sort ignores the API version.
- The operator calls `rm.ApplyAllStaged(ctx, resources, fluxOpts)` with `fluxssa.DefaultApplyOptions()` and sets only `Force` on it (`internal/apply/apply.go:97`). `CustomStageKinds` is empty.
- The library exports `object.Weight(gvk)` and the `Weight*` constants. Its two tables (`gvkWeights`, `kindWeights`) are not exported, so the operator cannot list the kinds they name.
- No non-test operator code calls `object.Sort`, `object.Stages`, `object.Weight` or the `lifecycle` package (grep over the tree at `0f75ccb`). The change stays test-only.

## Goals / Non-Goals

**Goals:**

- A Flux bump or a library bump that makes the two orders disagree on any pair of kinds fails `task dev:test`, with a message that says what to do.
- Flux's order comes from calls into the pinned Flux module. The test holds no copy of `ReconcileOrder`.
- The test proves that it can fail.

**Non-Goals:**

- No pre-sort by library weights and no change to how the operator applies. The owner decided that the operator keeps `ApplyAllStaged` (walkthrough decision SD48).
- No check of the prune and deletion order. The operator walks the inventory there and uses neither order.
- No pin of the weight values. That is the CLI's `internal/kubernetes/order_parity_test.go`; the operator does not sort by the weights, so the values themselves are not its contract.

## Decisions

### 1. Two tests: an exhaustive comparison without a cluster, and one observed apply

The comparison test (`internal/apply/flux_order_test.go`) runs without an API server and compares the whole universe of kinds. It MUST take Flux's order from the exported stage rules and the exported sorter. It has to assume one thing that Flux does not export: the precedence of the stages.

The observed test (`test/integration/apply`, the existing envtest suite) MUST call the operator's real `apply.Apply` and record the order of the writes. It checks that assumption, and it also sees what the operator itself passes to Flux. It cannot cover the whole universe, because the test API server serves only built-in kinds and the CRDs the test installs.

Alternatives considered:

- Only the comparison test. Simpler, and it satisfies the brief. It would miss a Flux release that adds or reorders a stage, and an operator change that sets `CustomStageKinds` or stops calling `ApplyAllStaged`.
- Only the observed test. It cannot apply kinds the server does not serve (`ClusterClass`, `GatewayClass`, `VolumeSnapshotClass`, `VerticalPodAutoscaler`, the same-named kinds of other groups), so most of the pairs that can go wrong would not be compared.
- Drive the real `ApplyAllStaged` over the whole universe with a hand-written in-memory client. It needs a client that fakes dry-run results and readiness for the stage waits. That couples the test to Flux internals that are not the subject, and it was not proven to work.

### 2. The universe of the comparison test

The universe is a list of `*unstructured.Unstructured`, one per group, version and kind, all with the same name and no namespace, so that only the kind decides the order. It MUST hold:

1. Every kind of `fluxssa.ReconcileOrder.First` and `.Last`, read from the variable at run time.
2. The kinds the library's table names and Flux's order does not: `PersistentVolume`, `PersistentVolumeClaim`, `DaemonSet`, `ReplicaSet`, `Job`, `Ingress`, `NetworkPolicy`, `HorizontalPodAutoscaler`, `VerticalPodAutoscaler`. This is a literal list, because the library's tables are not exported.
3. For each kind of 1 and 2: its object in its own API group, from a literal map of kind to group and version (the same map as `canonicalGVK` of the library's guard), and a same-named object in the two groups `a.example` and `zz.example`, which sort before and after every built-in group.
4. `CustomResourceDefinition` and `ClusterRole` at `v1beta1` of their own groups, a custom kind `Widget` in both example groups, a custom class kind `WidgetClass`, and `Widgetclass`, which is not a class kind (both sides match the suffix case-sensitively).

A kind of Flux's order with no entry in the literal map is still compared in the two example groups. The test logs its name and does not fail for it. The core `Namespace` is in the universe only at `v1`: Flux treats only `v1` as a cluster definition, the library treats every version as one, and no other version exists.

### 3. The comparison

```go
// stagedOrder returns objs in the order Flux's staged apply writes them.
func stagedOrder(objs []*unstructured.Unstructured, opts fluxssa.ApplyOptions) []*unstructured.Unstructured {
	var defs, classes, custom, rest []*unstructured.Unstructured
	for _, o := range objs {
		switch {
		case ssautils.IsClusterDefinition(o):
			defs = append(defs, o)
		case ssautils.IsClassDefinition(o):
			classes = append(classes, o)
		case ssautils.IsCustomStage(o, opts.CustomStageKinds):
			custom = append(custom, o)
		default:
			rest = append(rest, o)
		}
	}
	var out []*unstructured.Unstructured
	for _, stage := range [][]*unstructured.Unstructured{defs, classes, custom, rest} {
		sort.Sort(fluxssa.SortableUnstructureds(stage))
		out = append(out, stage...)
	}
	return out
}

// contradictions returns every pair that Flux applies first-then-second
// while weight puts the second strictly before the first.
func contradictions(order []*unstructured.Unstructured, weight func(schema.GroupVersionKind) int) []pair
```

The test calls `stagedOrder(universe, fluxssa.DefaultApplyOptions())` and then `contradictions(order, object.Weight)`. For every `i < j` in the order whose group and kind differ, the pair is a contradiction when `weight(order[i]) > weight(order[j])`. The switch above is the one part that mirrors Flux instead of calling it; decision 5 covers it.

What is not a failure, and how the test states it:

- Equal library weights that Flux orders (Flux refines). The test logs the count with `t.Logf`.
- A kind that one side does not name. `object.Weight` and Flux's sorter are both total, so the pair is compared by the fallbacks (`WeightDefault` or the `Class` suffix; rank 0, then group, then kind). It fails only when a pair is in opposite order. Kinds of Flux's order without an entry in the literal map are logged by name.
- Two objects of the same group and kind (two versions). Flux orders them by namespace and name only. The test does not compare them and says so in a comment.

### 4. The test MUST prove that it can fail

Three sub-tests, none of them parallel:

- An injected weight function that weighs `apps/Deployment` below `Service` MUST be reported, and so MUST one that breaks a pair Flux orders only by its group tie-break (as the library's `TestFluxComparisonCatchesAContradiction` does).
- A changed Flux order MUST be reported: the sub-test swaps two entries of the exported variable `fluxssa.ReconcileOrder.First` (`Service` and `Deployment`), restores the variable with `t.Cleanup`, and expects that pair. Flux computes the rank from the variable on every call, so this is the same effect as a Flux bump, and it proves that the test follows the live order and not a copy.
- A changed custom stage MUST be reported: `stagedOrder` with `CustomStageKinds` holding a kind of the default weight puts it before the kinds of the rest stage, and the comparison reports it.

### 5. The observed test

A new spec in `test/integration/apply` wraps the suite's client in a recorder and passes it to `apply.NewResourceManager`:

```go
// recordingClient records the kind and name of every write that is not a dry run.
type recordingClient struct {
	client.Client
	mu      sync.Mutex
	applied []*unstructured.Unstructured
}

func (r *recordingClient) Patch(ctx context.Context, obj client.Object, p client.Patch, opts ...client.PatchOption) error {
	po := &client.PatchOptions{}
	po.ApplyOptions(opts)
	if len(po.DryRun) == 0 { /* append a copy of obj under r.mu */ }
	return r.Client.Patch(ctx, obj, p, opts...)
}
```

The set is new on every run (a generated Namespace name, generated names for the cluster-scoped objects), so every object is created and therefore written. It holds one object of each kind class the server can serve: a CustomResourceDefinition and a custom resource of it, the Namespace that the namespaced objects live in, a ClusterRole, a ClusterRoleBinding, a StorageClass or PriorityClass, a ResourceQuota, a ServiceAccount, a Role, a RoleBinding, a ConfigMap, a Secret, a Service, a LimitRange, a Deployment, a StatefulSet, a CronJob, a PodDisruptionBudget, a DaemonSet, a Job, an Ingress, a NetworkPolicy, a PersistentVolumeClaim, and a ValidatingWebhookConfiguration with no webhooks. The spec deletes the cluster-scoped objects and the Namespace afterwards.

It asserts on the recorded order of the first write of each object:

- every object for which `ssautils.IsClusterDefinition` holds comes before every object for which `ssautils.IsClassDefinition` holds, and those come before all others (the precedence that decision 3 mirrors);
- `object.Weight` never decreases along the order.

If `Apply` retries because discovery served the new kind late, a retried attempt writes nothing for objects that are already there, so the first write of each object is still the order of the stages.

### 6. The failure message

Both tests fail through one message builder per test, with this content:

```text
Flux's staged apply order contradicts the library's kind weights (0012:D5:R1).
Flux:    github.com/fluxcd/pkg/ssa <version>
Library: github.com/open-platform-model/library <version>

Flux applies the first kind before the second; the library weighs the first above the second:
  /Service (50) before apps/Deployment (40)
  ...

What to do:
  - Do not edit this test or its list of kinds to make it pass.
  - The library's weight table follows Flux, not the other way round. Change
    opm/k8s/object/weights.go in the library, and the Flux list in its
    flux_order_test.go, so that the table agrees with this Flux version. A changed
    weight value is a breaking change of the library.
  - Hold this pin bump until that library release exists, then bump Flux and the
    library here in one PR.
  - If a library bump alone caused this, the library's table left Flux's order:
    fix it in the library and hold the bump.
```

The versions come from `debug.ReadBuildInfo()` when the test binary carries them; otherwise the line says `version not in the build info, see go.mod`. The reference `0012:D5:R1` is in a test message that only maintainers read, never in a string a user sees.

### 7. Where the rule is specified

The rule is one added requirement in `ssa-apply`, beside "Staged apply ordering". The library specifies its half in its `kubernetes-tier` spec; without a line here the operator's half would exist only as a test.

### Reconcile phase impact

Source, Render, Apply, Prune and Status: none. The tests call `internal/apply` and change nothing in it.

## Research & Decisions

### Where Flux's order can be read from
**Context**: The test must fail on a Flux bump that reorders kinds, so it cannot hold Flux's list.
**Explored**: ssa v0.77.0 in the module cache: `sort.go` (`ReconcileOrder`, `SortableUnstructureds`, `IsLessThan` exported; `less`, `computeKind2index` not), `utils/is.go` (the three stage rules exported), `manager_apply.go:206` and `:347` (the sort and the stage precedence, inline).
**Decision**: Call the sorter and the three stage rules. Mirror only the four-way precedence, and check it against a real apply.
**Rationale**: Everything Flux exports is called. The one thing it does not export is observed.

### How the operator can list the library's kinds
**Context**: "Every kind class the library weights name" needs the library's table, which is not exported.
**Explored**: library v1.0.0-beta.7 `opm/k8s/object/weights.go` (`gvkWeights`, `kindWeights` unexported), its guard `flux_order_test.go` (its universe is built from those tables).
**Decision**: A literal list of the nine kinds the library names beyond Flux's order, and the literal map of kind to group.
**Rationale**: A table row the library adds is already compared by the library's own guard, against its Flux literals. The operator's test adds what that guard cannot do: the live Flux order. The gap that stays is listed under Risks.

### Whether a production path sorts by the library weights
**Context**: The brief stops the change if one does.
**Explored**: grep for `object.Sort`, `object.Stages`, `object.Weight` and `k8s/lifecycle` over the operator tree at `0f75ccb`: no hit outside the change. Report T9.32 of the swarm found the same for the bump to beta.7.
**Decision**: Test-only, as planned.
**Rationale**: Flux's sort is the operator's only apply order.

## Risks / Trade-offs

- [The comparison test has never run] Section 1 of the tasks is the spike: if the test is red at the current pins, the apply run stops and reports the pairs instead of adjusting the universe. The expected result is green, because the library's guard passes against literals that are equal to `ReconcileOrder` of ssa v0.77.0.
- [The library adds a table row for a kind that neither Flux nor the operator's literal list names, and a Flux bump moves that kind in the same period] The operator's test does not compare the kind in its own group. The library's guard compares it against the library's Flux literals, and the failure message here sends the maintainer to update those literals, which closes the gap one release later. An exported list of the table's kinds in the library would close it fully; that is a library change and not part of this one.
- [The stage precedence is mirrored in four lines] The observed test fails when Flux changes it. It cannot see a new stage that only holds kinds the test server does not serve.
- [The operator starts to pass `CustomStageKinds`] The comparison test uses `DefaultApplyOptions()` and would not see it, because the options are built inline in `Apply`. The observed test sees it for the kinds in its set. Making the options reachable from the test would be a production change, so it is left out.
- [Mutating `fluxssa.ReconcileOrder` in a sub-test] It is a package variable of another module. The sub-test restores it in `t.Cleanup`, and no test of the package that touches it runs in parallel.
- [A Flux bump now can fail for a reason the bump author did not cause] That is intended. The message says where the fix goes.
- [Envtest time] One more apply of about 25 small objects in an existing suite.
