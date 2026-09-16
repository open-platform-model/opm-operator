## Why

Enhancement 0015 D16: *"Taking a contract away from instances that depend on it is refused regardless of the door it comes through: deletion (D3's finalizer) or update."* `registration-removal-guard` shut the deletion door. The update door is still open.

A provider upgrade whose re-rendered claim drops a contract from `spec.provides` is accepted today, because acceptance checks that `provides` matches what the catalog implements — and after a genuine shrink it does. The platform regenerates without that contract's provider, and every instance demanding it starts failing its render, attributed to the consumer rather than to the upgrade that caused it. That is the misattribution D8 already refused for build-incompatible providers.

It is worth doing now because its precondition exists. `ModuleInstance.status.requiredContracts` was built by `registration-removal-guard` and already answers "who depends on this contract" from the API server alone, with no render and no registry read.

## What Changes

- **The refusal lands in the operator's own apply path.** A `TransformerRegistration` is rendered output: the operator renders it from the provider module and writes it with server-side apply. So the operator is the writer, and it can decline to write. Before applying a rendered claim whose `provides` has shrunk, the reconciler compares it against the stored claim and, when instances still demand a dropped contract, holds that one resource back and leaves the accepted claim untouched.
- **The refusal is reported on the provider's `ModuleInstance`**, naming the dropped contracts and the dependent count in the same shape as the blocked delete, since that is the object whose upgrade was refused.
- **Drift detection skips a held-back claim.** Drift compares live state against a desired state this change deliberately refuses to assert, so reporting it would flag a difference the operator created on purpose and will not close.

## Not in this change

- **A guarantee against writers other than the operator.** Creating or editing a `TransformerRegistration` by hand requires platform-admin RBAC, which the operator ships unbound. A cluster administrator who binds it and edits a claim directly is not stopped by this change. D16's text is scoped to *"a provider module upgrade re-renders its registration"*, which is exactly the path this covers; closing the direct-write path needs a validating webhook and is a separate decision, not a gap discovered later.
- **The deletion door** — shipped in `registration-removal-guard` and untouched here.
- **Any change to acceptance's existing checks.** D8, D10, D11, D12 are shipped and unchanged; acceptance still re-derives every fact it judges.
- **A general readiness-aggregation wait**, and therefore D14's exclusion, which remains unreachable.

## Scope

Section 1 is a spike, but a narrow one: the approach is settled and what it verifies is that the apply-path hold behaves as the design expects. Specifically, that holding one resource back leaves the inventory and the stale set untouched — `inventory.NewEntryFromResource` builds an entry from GVK, namespace, name and component label, so a shrink changes nothing about identity and the entry is the same whether or not the new claim is applied. If that holds, no prune is triggered and the accepted claim survives. If it does not, the approach needs revisiting before anything is built on it.

Three sections total: the spike, the refusal and its tests, the drift exclusion. Each ends green and leaves `main` releasable.

## Impact

- **Depends on `registration-removal-guard`** (archived `2026-09-16`). It supplies `status.requiredContracts` and the dependent count this change reuses.
- **API types**: none. Neither the count nor the refusal needs a new field; the refusal is a condition on an existing status.
- **Controllers**: the `ModuleInstance` reconcile grows a pre-apply check and a filtered apply list. The claim reconciler and the Platform reconciler are untouched.
- **Install surface**: unchanged. No webhook, no cert-manager, no new failure mode.
- **Separation of concerns (Principle II)**: the instance reconcile learns one fact about `TransformerRegistration` — that a shrinking `provides` is refusable. The check itself lives behind its own seam rather than inline in the reconcile, so the reconcile calls a decision it does not implement.
- **SemVer**: MINOR.
- **Complexity (Principle VII)**: the cheapest of the three doors considered. It adds no infrastructure and no second source of truth, and it reuses a count that already exists.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `registration-removal-guard`: gains the shrinking-door requirement and its scenarios. The capability was deliberately scoped to the deletion door and says so in its Purpose; this change replaces that paragraph with the guarantee, bounded to writes the operator makes.
- `reconcile-loop-assembly`: the reconcile gains a phase between render and apply that can withhold a resource, and the apply no longer necessarily carries every rendered object.
- `drift-detection`: a withheld resource is excluded from drift comparison.

## Impact on existing behaviour

A provider upgrade that drops a still-demanded contract stops being applied, and its instance reports why. Upgrades that keep their contract set — the common case D16 names explicitly — are untouched, as is every instance that renders no claim at all.
