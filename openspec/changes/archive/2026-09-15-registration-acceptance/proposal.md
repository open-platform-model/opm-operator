## Why

`opm-operator` installs the `TransformerRegistration` CRD and ships the RBAC that gates who may create one (change `transformer-registration-crd`, PR 133), and `opm` 4.3.1 renders a claim into the group that CRD serves. Nothing reconciles a claim: it is stored with empty status and is inert, which is the state `transformer-registration-crd` deliberately left behind.

This change gives the claim a verdict. Enhancement 0015 D3 defines three states — not yet judged, accepted but inactive, active — and this change decides the first transition: whether a claim is accepted at all.

The prerequisite landed. Every acceptance check needs the *claimed* catalog fetched and evaluated, and the kernel could not read a catalog until `library` v1.0.0-alpha.31 added `opm/catalog`, `Kernel.AcquireCatalogFromRegistry` and the `Catalog` shape gate (library ADR-009). This repo is pinned to alpha.31 on `main`. The boundary that ADR draws is the one this change sits on: **the library reads, the operator judges.**

## What Changes

- `internal/controller/transformerregistration_controller.go` (new): a reconciler for the kind, watching it and patching its status, following the three existing controllers' shape (serial patcher, `[]metav1.Condition`, `ObservedGeneration`).
- **D10 — the claim names a catalog.** Acceptance acquires `spec.catalog` at `spec.version` through `Kernel.AcquireCatalogFromRegistry` and refuses anything that is not a `#Catalog`. A module artifact fails the library's shape gate, so the refusal is structural rather than a maintained rule.
- **D11 — `provides` is re-derived, not trusted.** `Catalog.Provides()` yields the provider-fulfilled contract set; acceptance compares it to the claim for **exact equality** and refuses drift in either direction naming both lists. Plus D11's deferred check, which that decision hands to this slice: the claim's instance-identity owner labels must exist and match `spec.providerRef`, so a hand-applied stray gets a named refusal instead of pointing the later health gate at the wrong package.
- **D12 — a duplicate claim is refused naming the claimant.** Two instances of one provider module produce two distinct CRs (the dot-joined name guarantees it); the second is refused here, which is the refusal site D12 designates.
- **D8 — a build-incompatible provider is refused at admission, not at render.** Per shared OPM-namespace path, the catalog's `cue.mod` requirement (`Catalog.Requires()`) must be ≤ the platform's resolved version within the same major; a different major is refused unconditionally. The rejection message says the comparison is conservative and that lowering the requirement is the author's one-line fix, as D8 requires.
- `status.accepted`, `conditions` and `observedGeneration` are set for the first time. `status.active` stays false: nothing activates a claim in this change.

## Scope warning and the proposed split

🛑 **Scope Warning**: This request does not cut into a handful of mergeable sections. The claim-to-verdict path plus activation and lifecycle is six sections, and the last carries an unresolved design question. I suggest we split it into the following changes:

1. **`registration-acceptance`** (this change): the reconciler and the four refusals — D10, D11, D12, D8. Ends with every claim accepted or refused, with a diagnostic naming what failed. Releasable alone: an accepted claim still does nothing, exactly as today, so nothing downstream changes behaviour.
2. **`registration-activation-and-lifecycle`**: D3's activation health gate (an accepted claim goes active when its `ModulePackage` reports `Ready=True`, which needs a second watch), D2's one-provider arm against the *active* set, D16's finalizer and its shrink refusal, and D14's readiness exclusion if a general-stage wait exists by then. This change completes D3, D2 and D16.

Should we start with the first? This proposal describes change 1; change 2 is not yet written.

**Why D16 is not in change 1.** D16 requires the refusal to land "**before** the new spec replaces the accepted claim", because "a post-overwrite rejection protects nobody". This repo ships **no webhook** — `config/webhook` does not exist and `config/default` wires none — so a genuine pre-apply refusal means introducing admission-webhook infrastructure (certificates, a service, a failure policy), or taking D16's named fallback of holding the last accepted claim in status. That is a real design decision with its own blast radius, and bolting it onto the end of a four-refusal change would make the last section the largest and the least reviewable.

## Impact

- **API types**: one additive status field. `TransformerRegistrationStatus` already carries `conditions`, `accepted` and `active`, and this change writes the first two — but unlike the other three CRDs it carried no `observedGeneration`, which the verdict has to record, so this change adds it (optional, additive) and regenerates the CRD.
- **Platform reconciler**: untouched. Regeneration keyed on the accepted set is change 3 (`registration-driven-regeneration`), not this one.
- **RBAC**: the manager role already carries `get;list;watch` and the status verbs on the kind (shipped in PR 133). No marker changes expected; if one is, it regenerates.
- **Downstream consumers**: none in-cluster. No provider catalog exists, so no module renders a claim; every claim reaching a cluster today is hand-applied by a platform admin.
- **SemVer**: MINOR. New controller behaviour, no existing behaviour changed.
- **Complexity (Principle VII)**: one reconciler and four refusals. The alternative — accepting claims unverified and letting failures surface at render — is what D8 and D11 explicitly reject, because the render names an unrelated module instance rather than the provider.

## Capabilities

### New Capabilities

- `registration-acceptance`: the claim-to-verdict path — what acceptance fetches, what it compares, which claims it refuses, and what each refusal names.

### Modified Capabilities

- `transformer-registration-crd`: its "Claim status separates acceptance from activation" requirement currently states that **no controller watches the kind** and a stored claim is inert. That stops being true here, so the requirement changes rather than being bolted onto the new capability.

## Impact on existing behaviour

A `TransformerRegistration` that is stored and ignored today starts receiving a verdict. Nothing else changes: an accepted claim has no effect on rendering, on `Platform`, or on any workload until change 2 activates it and change 3 regenerates on it.
