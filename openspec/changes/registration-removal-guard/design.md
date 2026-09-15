# Design: registration-removal-guard

## Context

See `proposal.md`. Current state, read 2026-09-15 at alpha.19:

- `internal/controller/transformerregistration_controller.go` has no deletion handling: the reconciler returns early on a not-found claim and carries no finalizer.
- `internal/reconcile/moduleinstance.go` is the finalizer precedent — `controllerutil.ContainsFinalizer`, `addFinalizer`, `removeFinalizer`, and a documented note that finalizer patches do not bump generation.
- `ModuleInstanceStatus` carries `ObservedGeneration`, `InstanceUUID`, `Conditions`, `Inventory`, `LastAttempted*`. **No contract demand.** `ModulePackageStatus` and `PlatformStatus` carry none either.
- `platform.ContractInventory` (library alpha.31) exposes `DefinedBy`, `RequiredBy`, `Unfulfilled`, `OverSubscribed`, `Fulfilled`, `Routable`.
- `config/webhook` does not exist; `config/default` wires no webhook.
- The render knows the fact: `kernel.RenderResult` carries `Pairs []RenderPair` and `Compiled` entries with `Component` and `Transformer`, and a transformer's required contracts are on its value.

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

### The change is not committed to an approach until the spike lands

Everything below is a candidate with a stated preference, not a settled design. Section 1 measures; sections 2 and 3 are written against whatever it finds. This is deliberate: the two unknowns here are each capable of changing the change's API surface, and a design that pretends otherwise would be rewritten during implementation.

### Candidate: persist demand at render time

**Preferred, pending the spike.** The render already computes instance -> components -> transformers -> contracts. A status field on `ModuleInstance` recording the contract FQNs its components require, written where the render result is already consumed, turns the finalizer into a list-and-filter and gives D16 the exact count.

```
  render time     instance -> components -> transformers -> contracts
                                                              |
                        write status.requiredContracts <-------+

  finalizer time  list ModuleInstances
                  count those whose requiredContracts
                  intersect the claim's provides
```

Costs, all real: a CRD field on the busiest type in the repo; a status write on every instance reconcile; and a staleness window, since the count reflects the last render rather than the current spec.

**Alternative — recompute at deletion time.** No API change. Re-evaluate every instance when the finalizer fires. Rejected as the preference for one reason above cost: it makes deletion depend on the registry being reachable, so an outage blocks an unrelated delete. A delete path that can fail for reasons unconnected to the delete is a bad property, and the operator would have no way to distinguish "you have dependents" from "I could not tell".

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

## Risks / Trade-offs

- [The spike may find neither candidate affordable] -> then D16's guarantee is not achievable as stated and the honest outcome is to say so in the spike's commit, and re-scope with the enhancement rather than ship a guard that does not guard.
- [A persisted count is stale by construction — it reflects the last render] -> acceptable if the staleness window is bounded by normal reconcile cadence; the spike should measure it rather than assume.
- [The finalizer makes a claim undeletable while the operator is down] -> standard for any finalizer in this repo; the existing `AnnotationForceDeleteOrphan` escape hatch on releases is the precedent if an equivalent is wanted, and is out of scope until asked for.
- [Webhook infrastructure changes the install surface] -> if the spike selects it, the proposal's SemVer note is revisited before implementation, not after.

## Open Questions

1. **Persist or recompute?** Section 1 answers it. Everything downstream depends on the answer, which is why nothing else is designed in detail.
2. **Webhook or hold-last-good?** Deliberately deferred until question 1 is answered.
