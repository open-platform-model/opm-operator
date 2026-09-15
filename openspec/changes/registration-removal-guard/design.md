# Design: registration-removal-guard

## Context

See `proposal.md`. Current state, read 2026-09-15 at alpha.19:

- `internal/controller/transformerregistration_controller.go` has no deletion handling: the reconciler returns early on a not-found claim and carries no finalizer.
- `internal/reconcile/moduleinstance.go` is the finalizer precedent — `controllerutil.ContainsFinalizer`, `addFinalizer`, `removeFinalizer`, and a documented note that finalizer patches do not bump generation.
- `ModuleInstanceStatus` carries `ObservedGeneration`, `InstanceUUID`, `Conditions`, `Inventory`, `LastAttempted*`. **No contract demand.** `ModulePackageStatus` and `PlatformStatus` carry none either.
- `platform.ContractInventory` (library alpha.31) exposes `DefinedBy`, `RequiredBy`, `Unfulfilled`, `OverSubscribed`, `Fulfilled`, `Routable`.
- `config/webhook` does not exist; `config/default` wires no webhook.
- The render knows the fact: `kernel.RenderResult` carries `Pairs []RenderPair` and `Compiled` entries with `Component` and `Transformer`, and a transformer's required contracts are on its value. The spike found a nearer source — the instance's own `components[*].#resources` and `#traits` — and the decision below takes it instead.

## Goals / Non-Goals

**Goals**

- A provider cannot abandon its dependents through either door.
- Every refusal names the dropped contracts and the dependent count, so the operator's next action is obvious.
- The approach is chosen by measurement, not by the first idea that fits.

**Non-Goals**

- Activation — `registration-activation`.
- Regeneration and `Platform.status.registry` — `registration-driven-regeneration`.
- A general readiness-aggregation wait, and therefore D14's exclusion.

## Decisions

### Decision: persist the instance's DECLARED demand at render time

**Settled by the section 1 spike, measured 2026-09-15 against library alpha.31 and catalogs opm 4.0.1 and 4.3.0.** The evidence is in Research & Decisions below.

`ModuleInstance.status.requiredContracts` is the sorted, deduped union of every component's `#resources` and `#traits` keys, read off the synthesized instance. The finalizer lists instances and counts those whose `requiredContracts` intersect the claim's `spec.provides`.

```
  render time     instance -> components -> #resources ∪ #traits
                                                    |
                      write status.requiredContracts <-+

  finalizer time  list ModuleInstances
                  count those whose requiredContracts
                  intersect the claim's provides
```

Three things the spike changed about the shape the proposal guessed:

**Declared demand, not matched-transformer contracts.** The guess was to record the contracts the MATCHED transformers require, derived from `RenderResult.Diagnostics.Pairs`. A pair names a transformer FQN (`kubernetes#simple`), not a contract, so translating it needs the platform's `#composedTransformers` fold — which makes the field a function of the platform and stale on every platform change. A component's `#resources` and `#traits` keys are contract FQNs already, in the exact keyspace `spec.provides` carries, and they are a property of the instance alone. That is also the keyspace the render's own matching rungs compare (`comp.#resources[fqn]` against `tf.requiredResources[fqn]`), so the intersection is exact rather than approximate: the single-provider guard (0010 D32/D37) admits at most one supplier per provider-fulfilled contract, and a transformer only reaches a component through a contract that component declares, so there is no transitive demand for the field to miss.

**The field over-lists on purpose.** It carries every declared contract, not only the provider-fulfilled ones. Filtering to provider-fulfilled would need the platform's contract inventory and reintroduce exactly the coupling the previous paragraph removes. The intersection with `provides` is what makes it exact — a contract in a claim's `provides` is provider-fulfilled by construction — and the cost is a handful of extra strings per instance (1 for the `hello` fixture, 6 for `hello_web`).

**It is carried on the render result.** `render.RenderResult` gains `RequiredContracts []string`, computed inside `KernelModuleRenderer.RenderModule` from the instance it has already synthesized. No second build, no second acquisition.

**Alternative — recompute at deletion time. Rejected on measurement.** The failure the proposal reasoned about is real and worse than stated: it is nondeterministic. With a warm CUE module cache an unreachable registry is invisible and the recompute succeeds; with a cold cache it fails in 435ms with `module not found` — textually identical to "the module was deleted from the registry". A recompute finalizer therefore cannot distinguish "no dependents" from "I could not tell", and which of the two it hits depends on what that controller pod's CUE cache happens to hold. Fail-closed makes an unrelated registry outage block every claim deletion; fail-open lets the abandonment through. Neither is acceptable for a delete path.

### A finalizer alone does not preserve the guarantee

`PlatformReconciler.activeClaims` (`internal/controller/platform_controller.go:291`) drops any claim carrying a deletion timestamp: *"A claim being deleted is dropped: its provider is on its way out, so its catalog should not enter the next package."* That is correct when deletion proceeds, and wrong when a finalizer holds it: the object stays, the finalizer reports the block, and the catalog nonetheless leaves the next generated platform — so the abandonment the guard exists to prevent happens anyway, on the next Platform regeneration, with the claim still sitting there reporting that it is protecting its dependents.

It is not immediate. `claimContributionPredicate` reads only `Accepted`, `Active` and the catalog coordinate, so stamping a deletion timestamp wakes no Platform reconcile; the divergence only lands when some other claim event or a Platform generation change triggers one. That makes it a latent, event-ordering-dependent hole rather than a guaranteed one, which is the harder kind to notice.

**Section 3 therefore carries a second edit the tasks did not name:** a claim whose deletion is blocked keeps contributing to the active set, and `activeClaims` drops a terminating claim only once the block has released. Section 2.1 is where this is written into tasks.md before anything is built on it.

**Alternative — derive from `ContractInventory.RequiredBy`.** Looks like the cheap answer and is measuring the wrong side. `RequiredBy` maps a contract to the enabled **transformers** whose `requiredResources` or `requiredTraits` name it. A provider-fulfilled contract's requirer is the provider's own transformer, so:

```
  before removal              after removal
  -------------               -------------
  contract C                  contract C
    ^        ^                  ^        ^
    |        |                  |        |
  provider   instances        (GONE)   instances
  transformer                          still demanding,
    ^                                  now failing
    |
  RequiredBy sees this        RequiredBy is now EMPTY
```

The requirer vanishes with the provider, so the signal goes quiet exactly when the damage is done. This alternative is recorded because it is the trap a later reader will fall into, not because it is viable.

### The refusal site is chosen after the count, not before

D16 fixes one constraint — the refusal must land while the previously accepted claim is still effective — and leaves the mechanics to this slice. Two doors:

| | How | Cost |
| --- | --- | --- |
| Validating webhook | Reject the shrinking update at admission | Certificates, a service, a failure policy, and a new outage mode where the webhook being down blocks claim writes in a repo that ships none today |
| Hold-last-good | Keep the last accepted claim in status; it stays effective while the refused update sits on the CR | No new infrastructure. The effective set diverges from the CR's spec — a second answer to what a claim says, which is the shape D11's derivation discipline exists to avoid |

D16 names hold-last-good as the fallback "if a pre-apply refusal proves infeasible". The sequencing decision here is that **the door is chosen after the spike**, because a pre-apply refusal that cannot name dependents is not worth webhook infrastructure.

### The finalizer follows the repo's existing shape

Whatever the count's source, the block itself is `internal/reconcile/moduleinstance.go`'s pattern: add the finalizer when the claim is first accepted, check dependents on a deletion timestamp, remove the finalizer when the count reaches zero. That file's note that finalizer patches do not bump generation matters here, because the claim reconciler filters on `GenerationChangedPredicate`.

## Research & Decisions

### Nothing in the cluster records which contracts an instance demands

**Context**: whether D3's "naming the count" is computable from existing state.
**Explored**: `ModuleInstanceStatus`, `ModulePackageStatus` and `PlatformStatus` field by field; `platform.ContractInventory`'s six fields and the doc comment on `RequiredBy`; `kernel.RenderResult`.
**Decision**: the fact exists only at render time; the spike decides whether to persist it or recompute it.
**Rationale**: no status field is contract-shaped, and the one inventory field that is maps contracts to transformers rather than to instances. The render computes the relation and discards it.

### `RequiredBy` cannot substitute for a dependent count

**Context**: it is the closest existing data and would need no new API.
**Explored**: the field's doc comment in library alpha.31 — *"maps each defined contract FQN to the implementation FQNs of every enabled transformer whose requiredResources or requiredTraits name it"*.
**Decision**: do not use it as the gate.
**Rationale**: recorded above with the diagram. Measuring transformers rather than instances means the signal empties at removal, which is the one moment it needed to be loud.

### A component's declared demand is readable from Go, and it is the contract keyspace

**Context**: section 1.1 — does a concrete field shape exist, and is there a concrete write site.
**Explored**: a probe against the published fixtures through the real render path (`test/integration/reconcile`, GHCR, library alpha.31). The render glue's own matching (`renderstage/render.cue.tmpl`) reads `comp.#resources` and `comp.#traits`; both are definitions, so Go reaches them with `cue.MakePath(cue.Def("resources"))` and `cue.Def("traits")` on each field of `instance.components`.
**Measured**:

| Fixture | `#resources` | `#traits` | acquire | synthesize |
| --- | --- | --- | --- | --- |
| `hello` | 1 (`…/opm/resources/config-maps@v1beta1`) | 0 | 13ms | 9ms |
| `hello_web` | 1 (`…/opm/resources/container@v1beta1`) | 5 (`init-containers`, `restart-policy`, `scaling`, `sidecar-containers`, `update-strategy`) | 16ms | 27ms |

**Decision**: the field shape is `status.requiredContracts []string`, the sorted union of those keys.
**Rationale**: the keys come out as full contract FQNs, the same strings `spec.provides` carries, so the finalizer's test is a set intersection with no parsing and no translation table. The demand is already computed before the render build runs, so the field costs a struct walk, not a build.

### The write site is the existing render-result consumption, and the staleness window is bounded by the events that move demand

**Context**: section 1.1 — where the write lands and how stale it gets.
**Explored**: `internal/reconcile/moduleinstance.go` (the deferred `patcher.Patch`, the `status.InstanceUUID` write at the same point, the no-op early return) and `ModuleInstanceReconciler.SetupWithManager`.
**Decision**: write it beside `status.InstanceUUID`, immediately after a successful `RenderModule`.
**Rationale**: three properties fall out of the existing loop rather than needing new machinery.

- **Every successful render refreshes it.** The write precedes the no-op early return, and the deferred patcher runs on every path out of the reconcile, so a Platform-triggered reconcile that changes no digest still refreshes the field.
- **A failed render leaves the previous value.** The error paths return before the write, so the count over-reports rather than under-reports. Over-reporting blocks a deletion that could have proceeded; under-reporting lets the abandonment through. Fail-closed is the correct direction for a guard.
- **There is no periodic resync to wait for.** A successful reconcile returns `ctrl.Result{}` with no `RequeueAfter`, so the field is refreshed only on events — and the two events that can move an instance's demand are exactly the two the controller already watches: the instance's own generation (`GenerationChangedPredicate` on `For()`) and any Platform change (`mapPlatformToModuleInstances` re-enqueues every instance in the cluster). The staleness window is therefore one reconcile, not one resync interval.

### Recomputing at deletion time fails nondeterministically, and says "not found" when it means "cannot tell"

**Context**: section 1.2 — measure the recompute candidate's cost and its behaviour under an unreachable registry.
**Explored**: the same probe, timing `KernelModuleRenderer.RenderModule` against the generated platform, then repeating it against an unreachable registry mapping with a warm and a cold CUE module cache.
**Measured**:

| | Result |
| --- | --- |
| Full render, warm cache, registry reachable | 89ms, 97ms, 92ms (three runs, `hello`) |
| Acquire + synthesize only (demand without rendering) | 22ms to 43ms |
| Render, registry unreachable, **warm** CUE cache | **succeeds** in 88 to 108ms — the outage is invisible |
| Render, registry unreachable, **cold** CUE cache | fails in 435ms: `acquiring module …: module not found` |

**Decision**: reject recompute.
**Rationale**: the per-instance cost (90ms, times every ModuleInstance in the cluster, on a delete) is the smaller problem. The real one is that the two outcomes above are both reachable from the same cluster state, decided by what that controller pod's CUE cache happens to hold, and the failure reports `module not found` — the same sentence the registry emits when a module genuinely no longer exists. A finalizer cannot tell a dependent-bearing claim from an unreadable one, so it must choose in advance between blocking every deletion during a registry outage and letting an abandonment through during one. The persisted field has no such state: it is read from the API server, which the controller needs anyway to see the claim at all.

### A blocked deletion still drops the claim from the next platform

**Context**: whether the finalizer as the tasks describe it delivers the spec's guarantee.
**Explored**: `PlatformReconciler.activeClaims` and `claimContributionPredicate` in `internal/controller/platform_controller.go`.
**Decision**: section 3 also changes `activeClaims`; recorded as a decision above and carried into tasks.md at 2.1.
**Rationale**: `activeClaims` treats a deletion timestamp as "on the way out" and drops the catalog from the next generated platform. Under a finalizer that assumption no longer holds, and the dependents the finalizer is protecting lose their provider while the block is still reported as holding. Nothing in the spike's chosen approach fixes this; it is a separate edit at a separate site.

## Risks / Trade-offs

- ~~[The spike may find neither candidate affordable]~~ -> it did not: persistence is affordable and the field is cheaper than the shape the proposal guessed.
- [A persisted count is stale by construction — it reflects the last render] -> measured and bounded: the field is refreshed on every successful render, and the only two events that can move an instance's demand (its own generation, any Platform change) are both already watched. A failed render leaves the previous value, which over-reports — the fail-closed direction.
- [A suspended or CLI-owned instance never re-renders, so its field freezes] -> it freezes at its last rendered demand, which over-reports for as long as the instance exists and stops mattering when it is deleted. Same fail-closed direction; no special handling.
- [The finalizer makes a claim undeletable while the operator is down] -> standard for any finalizer in this repo; the existing `AnnotationForceDeleteOrphan` escape hatch on releases is the precedent if an equivalent is wanted, and is out of scope until asked for.
- [Webhook infrastructure changes the install surface] -> if the spike selects it, the proposal's SemVer note is revisited before implementation, not after.

## Open Questions

1. ~~**Persist or recompute?**~~ **Answered by the section 1 spike: persist.** `ModuleInstance.status.requiredContracts` carries the instance's declared demand, written at render time; the finalizer intersects it with the claim's `provides`. Recompute was rejected because an unreachable registry makes it fail as `module not found` on a cold CUE cache and succeed silently on a warm one — the same cluster state, two answers. See the decision and its measurements above.
2. **Webhook or hold-last-good?** Still open; section 4.1 answers it now that the count exists.
