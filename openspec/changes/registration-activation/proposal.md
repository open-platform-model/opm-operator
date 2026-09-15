## Why

`registration-acceptance` (alpha.19) gives every `TransformerRegistration` a verdict, but an accepted claim still does nothing: `status.active` is hard-coded false and no watch can flip it. Enhancement 0015 D3 defines three states — not yet judged, accepted but inactive, active — and only the first transition exists.

The missing transition is the one a static `spec.registry` subscription structurally cannot express. A subscription is live the moment it is written, but the CRDs a provider's transformers render against exist only once the provider is running. D3's answer is a health gate: an accepted claim stays inactive until its provider is Ready.

Activation also unblocks D2. The one-provider rule's second enforcement arm refuses a claim naming a contract "already provided by an active subscription or registration" — a check that cannot be written while nothing is ever active.

## What Changes

- **D3 — the activation gate.** A watch on `ModuleInstance` re-enqueues claims when a provider's readiness changes. An accepted claim whose provider reports `Ready=True` sets `status.active`.
- **D3 — activation latches.** Once active, a claim SHALL NOT deactivate because its provider's health regressed. The decision rejects live-tracking by name, and for a concrete reason: a flapping provider would toggle the active-claim set, which regenerates the platform package without the provider catalog (D13 keys on that set) and fails every dependent instance's render for the duration. The gate exists for install ordering — "the CRDs do not exist *yet*" — not for steady-state health.
- **D2 — the one-provider arm.** Acceptance refuses a claim whose `provides` names a contract an enabled `Platform.spec.registry` subscription or another **active** claim already provides, naming the holder. This is distinct from the existing D12 refusal, which is about two claims naming the same *catalog*; this one is about two providers of the same *contract*.

## Not in this change

- **D16's finalizer and shrink refusal** — `registration-removal-guard`. It needs a dependent count that nothing currently records and a refusal site this repo has no door for; both are design work, not a trailing section here.
- **`Platform.status.registry`** and regeneration keyed on the active set — `registration-driven-regeneration` (D13, D17).
- **D14's readiness exclusion.** Still nothing to exclude from: `ModulePackage.Ready` is set from the reconcile outcome alone, and the only waiting is Flux's `WaitForSet` on the cluster-definition and class stages, never the general resource stage. The deadlock D14 predicts becomes reachable when someone adds a general-stage wait, and the exclusion belongs to that change.
- Any change to what acceptance already checks. D8, D10 and D12 are shipped and untouched.

## Impact

- **API types**: none. `status.active` already exists; this change is the first to write it.
- **Controllers**: `TransformerRegistrationReconciler` gains one watch and one transition. The `ModuleInstance` and `ModulePackage` reconcilers are untouched.
- **RBAC**: the manager already reads `ModuleInstance`; no new marker expected.
- **Downstream consumers**: none in-cluster. An active claim still has no effect on rendering until `registration-driven-regeneration`, so activation is observable in status and nowhere else.
- **SemVer**: MINOR. New behaviour on an existing field, nothing removed or narrowed.
- **Complexity (Principle VII)**: one watch, one latch, one refusal. The alternative for the gate — mirroring `Ready` continuously — is simpler to write and is the thing D3 rejects, because the failure mode is fleet-wide render churn from a transient condition.

## Capabilities

### New Capabilities

- `registration-activation`: when an accepted claim becomes active, what keeps it active, and what does not.

### Modified Capabilities

- `registration-acceptance`: gains the one-provider-per-contract refusal (D2's arm), which is a new requirement on the existing claim-to-verdict capability rather than a separate one.
- `transformer-registration-crd`: its "Claim status separates acceptance from activation" requirement states that `active` SHALL remain false because nothing activates a claim. That stops being true.

## Impact on existing behaviour

An accepted claim whose provider is Ready reports `status.active: true` where it previously reported false. Nothing reads that field yet, so no rendering, no `Platform` field and no workload changes as a result.
