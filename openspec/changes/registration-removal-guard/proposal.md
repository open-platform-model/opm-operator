## Why

Enhancement 0015 D3 makes removal refusable: *"A finalizer blocks deletion while instances demand contracts the registration provides, naming the count."* D16 extends that to the other door — an update that shrinks `provides` is the same abandonment, and is refused the same way, **before** the new spec replaces the accepted claim.

Neither exists. An active claim can be deleted, or have a contract dropped from `provides`, and every instance demanding that contract starts failing its render with no warning and no attribution to the change that caused it.

Two things stand between here and that guarantee, and both are design work rather than implementation:

**Nothing records who depends on a contract.** `ModuleInstanceStatus` carries generation, UUID, conditions, inventory and last-attempted digests; `ModulePackage` and `Platform` carry nothing contract-shaped either. The library's `ContractInventory.RequiredBy` looks like the answer and is not: it maps a contract to the **transformers** that require it, and a provider-fulfilled contract's requirer is the provider's own transformer — so when the provider is removed, the requirer disappears with it, and `RequiredBy` goes empty exactly when the damage is done. The fact that the finalizer needs exists only at render time, where the instance's components are matched to the transformers that serve them, and nothing carries it forward.

**There is no door to refuse the shrink at.** D16 is explicit that a post-overwrite rejection protects nobody, and this repo ships no webhook: `config/webhook` does not exist and `config/default` wires none.

## What Changes

- **The dependent question, settled by a spike** (see Scope). The candidate is persisting demand at render time — a status field on `ModuleInstance` recording the contracts its components require, written where the render result is already consumed — which makes the finalizer a cheap list-and-filter and gives D16 the exact count it asks for. The spike confirms or rejects it before anything is built on it.
- **The finalizer.** A finalizer on `TransformerRegistration` blocks deletion while dependents exist, naming the count, and releases when they are gone.
- **The shrink refusal.** An update dropping a contract from `provides` that dependents still demand is refused with the same shape and diagnostic as the blocked delete, at a site the spike's outcome selects.

## Not in this change

- **Activation** — `registration-activation`. This change assumes the active state exists and does not create it.
- **`Platform.status.registry`** and regeneration — `registration-driven-regeneration`.
- **Any change to acceptance's existing checks.** D8, D10, D12 are shipped and untouched.
- **A general readiness-aggregation wait**, and therefore D14's exclusion, which remains unreachable.

## Scope

**Section 1 is a spike, and the change is not committed to an approach until it lands.** design.md names the candidate and the alternatives; the spike measures which is affordable. If it finds that persisting demand is the answer, this change grows a `ModuleInstance` status field, which is a CRD addition on the busiest type in the repo and should be reviewed as one. If it finds recomputation is affordable, no API changes at all.

The refusal-site question is deliberately sequenced **after** the spike: a pre-apply refusal that cannot name dependents is not worth webhook infrastructure, so the door is chosen once the count is known to exist.

## Impact

- **API types**: possibly one status field on `ModuleInstance`, decided by the spike. `TransformerRegistration` gains a finalizer, which is metadata rather than schema.
- **Controllers**: `TransformerRegistrationReconciler` gains deletion handling. The `ModuleInstance` reconciler may gain a status write, decided by the spike.
- **Deletion behaviour**: a `TransformerRegistration` stops being freely deletable. An operator who wants one gone while dependents exist must remove the dependents first, which is the point.
- **SemVer**: MINOR, unless the spike selects a webhook, which changes the install surface and should be called out again at that point.
- **Complexity (Principle VII)**: this is the most expensive guarantee in 0015's operator share, and it is justified by what it prevents — a provider upgrade silently breaking every consumer, with the failures attributed to the consumers rather than to the upgrade. That misattribution is the same one D8 refused for build-incompatible providers.

## Capabilities

### New Capabilities

- `registration-removal-guard`: what makes a claim undeletable, what makes a `provides` shrink refusable, and what each refusal names.

### Modified Capabilities

None expected. If the spike selects a persisted demand field, the capability that owns `ModuleInstance` status gains a requirement, and this list is updated then rather than guessed now.

## Impact on existing behaviour

Deleting a `TransformerRegistration` can now block. Nothing else changes for a claim that no instance depends on.
