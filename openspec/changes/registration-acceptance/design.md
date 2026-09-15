# Design: registration-acceptance

## Context

See `proposal.md` for motivation and the scope split. Current state, read 2026-09-15:

- `api/v1alpha1/transformerregistration_types.go` ships the kind; `status` carries `conditions`, `accepted` and `active`, all unset because nothing watches it.
- `internal/controller/` holds three reconcilers. Each patches status through a `patch.SerialPatcher`, sets `[]metav1.Condition` and `ObservedGeneration`, and wires itself in `SetupWithManager`. The Platform reconciler's RBAC markers already grant `get;list;watch` and the status verbs on `transformerregistrations`.
- `internal/platform/store.go` holds one `Generated{Generation, Dir, Platform *platform.Platform, Skew}` — the built platform value the render path leases. `Generated.Platform` is what D8's comparison and any contract lookup read.
- `library` v1.0.0-alpha.31 is pinned: `Kernel.AcquireCatalogFromRegistry`, `Catalog.Provides()`, `Catalog.Requires()`, and a `Catalog` shape gate wrapping `ErrWrongKind`.
- `internal/reconcile/moduleinstance.go` is the finalizer precedent (`controllerutil.ContainsFinalizer`, `addFinalizer`, `removeFinalizer`) — for the *next* change, not this one.
- `pkg/core/labels.go` defines `LabelModuleInstanceUUID = "module-instance.opmodel.dev/uuid"`. The operator only **reads** it (`internal/apply/prune.go:107`, `internal/reconcile/moduleinstance.go:919`); nothing stamps it.

## Goals / Non-Goals

**Goals**

- Every claim carries a verdict, and every refusal names what failed and the value that failed it.
- Acceptance re-derives every fact it judges; nothing on the CR is trusted.
- Refusals land here, where the diagnostic can name the provider, rather than at render, where it would name an unrelated module instance.

**Non-Goals**

- Activation, the finalizer, the shrink refusal, D2's active-set arm — `registration-activation-and-lifecycle`.
- `Platform.status.registry` and regeneration on the accepted set — `registration-driven-regeneration`.
- Any change to the CRD's shape. `status.active` is written by the next change, not this one.

## Decisions

### A reconciler of its own, not a branch of the Platform reconciler

The kind gets `internal/controller/transformerregistration_controller.go`, watching `TransformerRegistration` and patching its status. Acceptance is per-claim and its verdict lives on the claim, so a claim is the natural reconcile unit; folding it into the Platform reconciler would make one object's failure a platform-level failure, which is precisely the misattribution D8 exists to avoid.

It reads the built platform through the existing `internal/platform.Store` rather than building anything. A claim arriving before the platform has been generated is **requeued, not refused**: the platform's absence says nothing about the claim, and refusing on it would make the verdict depend on reconcile order.

### D11's deferred check is grounded in the inventory, not in labels

D11 hands this slice a candidate check and does not mandate its mechanism: *"require the CR's instance-identity owner labels (0010 D41, present on all rendered output) to exist and match `spec.providerRef`"*. **Measured, that check cannot be written as described.**

The rendered claim's labels are fixed by `catalog_opm`'s golden fixture, which asserts their count is exactly four:

```cue
labels: {
	"app.kubernetes.io/managed-by":     "opm-test"
	"app.kubernetes.io/name":           "k8up"
	"app.kubernetes.io/instance":       "k8up"
	"module-instance.opmodel.dev/name": "k8up"
}
```

There is no uuid label and **no namespace label**, even though the fixture supplies a uuid on `#moduleInstance.metadata`. `providerRef` carries `{namespace, name}`; the labels carry a name alone. A label-only check would therefore accept a stray claim placed by any instance sharing the provider's name in a different namespace — exactly the spoof the check exists to stop.

**Decision:** verify against the operator's authoritative inventory instead. Look up the `ModuleInstance` named by `spec.providerRef`; refuse unless it exists and its `status.inventory` contains this claim (`Kind: TransformerRegistration`, the claim's name, no namespace). CONSTITUTION Principle III already makes the inventory the ownership record — *"pruning decisions rely on inventory, not labels; labels are supportive hints for humans"* — so this reuses the repo's existing answer to "who owns this object" rather than inventing a second one from a weaker signal.

**Alternatives considered:**

- **Check the name label only, and document the namespace hole.** Rejected: a check that admits the spoof it was written to stop is worse than no check, because it reads as protection.
- **Add a namespace or uuid label to the rendered claim first.** A `catalog_opm` change plus a fixture bump, blocking this one, to reach a weaker guarantee than the inventory already gives. Worth doing if the inventory route proves infeasible; recorded as the fallback.

### D12's holder is the oldest claim, decided by creation timestamp then name

D12 requires the second claim to be refused *naming the claimant*, and the spec requires the holder to be stable across reconciles. The holder is the claim with the earliest `metadata.creationTimestamp`, ties broken by name. Both fields are immutable, so the decision does not move when either claim is re-reconciled, and it does not depend on which claim the informer happens to deliver first.

**Alternative considered — first-accepted-wins, recorded in status.** Makes the holder depend on reconcile order, so a manager restart can hand acceptance to the other claim. Rejected on the stability requirement.

### D8 compares against the built platform's resolution, not the Platform CR's spec

`spec.registry` records what the platform *asked for*; `Generated.Platform` is what it *resolved to*, which is what a provider's transformers will actually run against. D8's discipline is a committed-resolution comparison (0019 D18), so the resolved side is the correct operand. Per shared OPM-namespace path, refuse when the catalog's requirement exceeds the platform's within a major, and refuse unconditionally across majors.

The refusal message states the comparison is conservative — a `cue.mod` requirement records what the provider was tidied against, not what it uses — and that lowering it is a one-line fix, both of which D8 requires verbatim.

## Research & Decisions

### The rendered claim carries no namespace-bearing identity label

**Context**: whether D11's deferred owner-label check is implementable as written.
**Explored**: `catalog_opm` `opm/transformers/transformer_registration_transformer.cue`'s golden fixture and its `_testTransformerRegistrationLabelCount` guard; `pkg/core/labels.go`; every reader of `LabelModuleInstanceUUID` in this repo.
**Decision**: ground the check in `status.inventory` instead (above).
**Rationale**: the fixture asserts exactly four labels and none of them carries a namespace or a uuid, while the operator only ever reads the uuid label and never stamps one. So the label set available on a claim identifies an instance *name*, which is not unique across namespaces. The inventory is unambiguous and is already this repo's ownership record by constitutional principle.

### A claim arriving before the platform is generated is a requeue

**Context**: D8 and any contract lookup need `Generated.Platform`, which is absent until the Platform reconciler has run.
**Explored**: `internal/platform/store.go` — `Store.Generated()` returns `(Generated, bool)`.
**Decision**: requeue on `false`; do not write a verdict.
**Rationale**: a verdict that depends on reconcile order is not a verdict. The claim is unchanged and the platform will appear; refusing and later accepting would flap `status.accepted` and any condition watching it.

## Risks / Trade-offs

- [The inventory check couples acceptance to `ModuleInstance` reconcile timing — a claim can be applied before its instance's inventory is written] -> requeue, exactly as for the absent platform; the claim is not refused for a race. The refusal fires only when the instance is absent or its inventory settled without this claim.
- [Refusing an unresolvable coordinate and a wrong-kind artifact differently costs a distinguishing branch] -> required by the spec: one is a registry or coordinate problem and one is an authoring problem, and collapsing them sends the claimant to the wrong fix.
- [Acceptance fetches a catalog on every reconcile] -> CUE caches the module zip on disk, and a claim reconciles on generation change. If this proves hot, the resolved verdict is already on status and `observedGeneration` gates recomputation.
- [D12's oldest-wins can hand acceptance to a claim whose instance is being deleted] -> out of scope here; the finalizer and the lifecycle edges are the next change, which is where a holder handoff belongs.

## Open Questions

None blocking. The fallback if the inventory route proves infeasible is recorded under D11's decision above.
