## Why

Enhancement 0015 D3 makes removal refusable: *"A finalizer blocks deletion while instances demand contracts the registration provides, naming the count."* D16 extends that to the other door — an update that shrinks `provides` is the same abandonment, refused the same way.

Neither exists. An active claim can be deleted, or have a contract dropped from `provides`, and every instance demanding that contract starts failing its render with no warning and no attribution to the change that caused it.

**This change closes the deletion door.** The shrinking door is its own change (see Not in this change): the two share a precondition — something has to know who depends on a contract — and that precondition is what makes this one worth doing first.

Two things stood between here and D3's guarantee, and both were design work rather than implementation:

**Nothing records who depends on a contract.** `ModuleInstanceStatus` carries generation, UUID, conditions, inventory and last-attempted digests; `ModulePackage` and `Platform` carry nothing contract-shaped either. The library's `ContractInventory.RequiredBy` looks like the answer and is not: it maps a contract to the **transformers** that require it, and a provider-fulfilled contract's requirer is the provider's own transformer — so when the provider is removed, the requirer disappears with it, and `RequiredBy` goes empty exactly when the damage is done. The fact that the finalizer needs exists only at render time, where the instance's components are matched to the transformers that serve them, and nothing carries it forward.

**A finalizer alone does not deliver it.** `PlatformReconciler.activeClaims` drops a claim the instant it carries a deletion timestamp, so a blocked claim's catalog leaves the next generated platform and its dependents are abandoned anyway — through the door the block was closing, with the claim still reporting that it held. Found by the spike; the two edits ship together.

## What Changes

- **The dependent question, settled by the section 1 spike** (see Scope). `ModuleInstance.status.requiredContracts` records the instance's DECLARED demand: the union of every component's `#resources` and `#traits` keys, which are already contract FQNs in the keyspace `spec.provides` carries. The finalizer intersects it with the claim's `provides`, which makes the count a list-and-filter and gives D16 the exact number it asks for. Recompute-at-deletion was measured and rejected: with a warm CUE cache an unreachable registry is invisible, with a cold one it fails as `module not found` — one cluster state, two answers.
- **The finalizer.** A finalizer on `TransformerRegistration` blocks deletion while dependents exist, naming the count, and releases when they are gone.
- **The active set stops dropping a blocked claim.** `PlatformReconciler.activeClaims` drops a claim the moment it carries a deletion timestamp. Under a finalizer that abandons the dependents anyway on the next regeneration, while the block still reports that it is holding them, so a claim whose deletion is blocked keeps contributing until the block releases. Found by the spike; folded into this change because a finalizer without it is cosmetic.

## Not in this change

- **The shrink refusal (D16).** It was section 4 and is now its own change. The count this change builds is its precondition, and with the count in hand the remaining question — where the refusal lands — turns out to be the expensive half: a validating webhook changes an install surface this operator does not have today, and D16's hold-last-good fallback gives a claim two answers to what it provides, which reaches into the whole acceptance pipeline. Either is more than a section. Until it lands, a shrinking `provides` is accepted, and the capability spec says so rather than promising a guarantee nothing enforces.
- **Activation** — `registration-activation`. This change assumes the active state exists and does not create it.
- **`Platform.status.registry`** and regeneration — `registration-driven-regeneration`.
- **Any change to acceptance's existing checks.** D8, D10, D12 are shipped and untouched.
- **A general readiness-aggregation wait**, and therefore D14's exclusion, which remains unreachable.

## Scope

**Section 1 was a spike, and it has landed (2026-09-15).** It selected persistence, so this change carries a `ModuleInstance` status field — a CRD addition on the busiest type in the repo, and it should be reviewed as one. It also found that the finalizer needs a second site, the Platform reconciler's active-claim set; both are written into tasks.md sections 2 and 3.

The refusal-site question was deliberately sequenced after the spike, on the reasoning that a pre-apply refusal that cannot name dependents is not worth webhook infrastructure. With the count built, the answer is that either door costs more than a section, so it left with D16 rather than being squeezed into this change.

## Impact

- **API types**: one status field on `ModuleInstance`, `status.requiredContracts`, derived and never authored. `TransformerRegistration` gains a finalizer, which is metadata rather than schema.
- **Controllers**: `TransformerRegistrationReconciler` gains deletion handling; the `ModuleInstance` reconciler gains a status write beside the existing `status.instanceUUID` one; `PlatformReconciler.activeClaims` stops dropping a claim whose deletion is blocked.
- **Render**: `render.RenderResult` gains `RequiredContracts`, computed from the instance the renderer has already synthesized. No extra acquisition, no extra build, no read of the platform.
- **Deletion behaviour**: a `TransformerRegistration` stops being freely deletable. An operator who wants one gone while dependents exist must remove the dependents first, which is the point.
- **SemVer**: MINOR. The install surface is unchanged; the webhook question left with D16.
- **Complexity (Principle VII)**: this is the most expensive guarantee in 0015's operator share, and it is justified by what it prevents — a provider upgrade silently breaking every consumer, with the failures attributed to the consumers rather than to the upgrade. That misattribution is the same one D8 refused for build-incompatible providers.

## Capabilities

### New Capabilities

- `registration-removal-guard`: what makes a claim undeletable, and what the refusal names. The capability is scoped to the deletion door; D16's shrinking door adds its requirement to the same capability when it lands.

### Modified Capabilities

- `reconcile-loop-assembly`: the `ModuleInstance` status patch gains `requiredContracts`, written on every successful render and left untouched on every path that does not render.
- `registration-driven-regeneration`: the active-claim set no longer drops a claim the instant it carries a deletion timestamp; a claim whose deletion is blocked keeps contributing until the block releases.

## Impact on existing behaviour

Deleting a `TransformerRegistration` can now block. A `ModuleInstance` gains one status field. Nothing else changes for a claim that no instance depends on.
