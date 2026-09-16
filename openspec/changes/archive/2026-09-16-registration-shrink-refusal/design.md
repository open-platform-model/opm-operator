# Design: registration-shrink-refusal

## Context

See `proposal.md` for motivation. Current state, read 2026-09-16 on `main` at `4552255`:

- `TransformerRegistration` is **rendered output**, not an object a user applies. `checkProviderIdentity` (`internal/controller/transformerregistration_controller.go`) proves it structurally: it accepts a claim only when the provider instance's `status.inventory` owns it, so every legitimate claim arrived through the operator's own render and apply.
- `internal/reconcile/moduleinstance.go` renders to `[]*core.Resource`, converts with `toUnstructuredSlice`, and hands the whole slice to `apply.Apply`. There is no per-resource decision point today.
- `apply.Apply` calls Flux's `ApplyAllStaged` once for the whole slice; it has no per-object hook, so a withheld resource must be removed from the slice before the call.
- `inventory.NewEntryFromResource` (`internal/inventory/entry.go:12`) builds an entry from GVK, namespace, name and the component label. **No spec, no digest.**
- `ModuleInstance.status.requiredContracts` and the dependent count shipped with `registration-removal-guard` (archived `2026-09-16`).
- `config/default/kustomization.yaml` carries kubebuilder's `[WEBHOOK]` and `[CERTMANAGER]` scaffolding, commented out; `config/webhook` and `config/certmanager` do not exist. Cert-manager is already installed in the e2e path.

## Goals / Non-Goals

**Goals**

- The refusal lands while the previously accepted claim is still effective, which is D16's one fixed constraint.
- It adds no infrastructure and no second answer to what a claim provides.
- The operator's own upgrade path cannot abandon dependents.

**Non-Goals**

- Stopping a writer that is not the operator. See the decision below; this is a bounded guarantee, stated rather than discovered.
- Re-deciding the deletion door, shipped in `registration-removal-guard`.
- Changing what `requiredContracts` means or how the count is derived.

## Decisions

### The refusal goes in the operator's apply path, not at admission

D16 fixes one constraint: the refusal must take effect while the previously accepted claim is still effective, because *"a post-overwrite rejection protects nobody"*. That splits the write path at the point where the old claim dies:

```
   provider module upgrade (new catalog version, contract dropped)
              |
              v
   +---------------------------+
   |  ModuleInstance reconcile |
   |  render -> resources[]    |
   +------------+--------------+
                |
   [DOOR 3] ----+  hold the one resource back, before apply
                |
                v
   +---------------------------+
   |  apply.Apply (SSA)        |
   +------------+--------------+
                |
   [DOOR 1] ----+  validating webhook: reject at admission
                |
                v
   +===========================+
   |  etcd: claim spec         |  <-- the old claim dies HERE
   +============+==============+
                |
   [DOOR 2] ----+  hold-last-good: too late, compensate in status
                |
                v
   +---------------------------+
   |  claim reconciler         |
   +---------------------------+
```

Doors 1 and 3 are both above the line. Door 2 is below it, which is why D16 names it only as a fallback.

**Door 3 is chosen.** The insight that makes it available is that the operator is the writer. A claim is not something a user applies; it is rendered from the provider module and written by the operator's own server-side apply. A writer does not need an admission hook to decline to write.

**Alternative — door 1, a validating webhook.** Catches every writer, including a direct `kubectl` edit. Rejected on cost against benefit: it makes cert-manager a hard install dependency, adds a Service and a failure policy, and introduces an outage mode the operator does not have today — webhook unavailable, no claim can be written at all. The scaffolding being present and commented out makes it cheaper than it first appears, but none of that removes the new dependency or the new failure mode. What it buys over door 3 is only the direct-write path, which the decision below scopes out.

**Alternative — door 2, hold-last-good.** D16's own named fallback, and the reason it is a fallback is that the effective claim would diverge from `spec`: the platform's `ClaimCoordinate` and acceptance's re-derivation both read `spec` today, so an "effective claim" notion has to be threaded through acceptance, the platform's active set and D11's derivation discipline at once. Door 3 keeps `spec` authoritative by never letting it become wrong.

### Withholding one resource perturbs nothing else

The obvious objection to door 3 is that the apply list and the inventory are the same list, so withholding a resource drops it from the inventory, `ComputeStaleSet` sees it as stale, and the prune deletes the claim — the abandonment itself, arrived at through the guard.

That does not happen, for a specific reason: `inventory.NewEntryFromResource` records GVK, namespace, name and component label and nothing else. A shrink changes `spec.provides`; it changes nothing about identity. The rendered entry is therefore byte-identical whether or not the new claim is applied, so:

- the inventory is written from the full rendered set and stays truthful — the operator does still own that object;
- the stale set is empty for the claim, so no prune is triggered;
- only the apply list is filtered.

Inventory and apply list being separable at exactly this point is what makes the approach cheap. Section 1 verifies it rather than trusting this paragraph.

**Verified in section 1** (`test/integration/reconcile/withhold_invariant_test.go`), against the real reconcile rather than `NewEntryFromResource` in isolation: with one resource rendered but kept out of the apply list — and rendered with *changed* content, so the test proves indifference to content and not merely that nothing moved — the committed inventory still carries both entries, identical to the ones the full apply wrote, the stale set is empty, and the withheld object survives the prune still holding what the previous apply gave it. The resource that was not withheld took its new render in the same reconcile.

### The guarantee is bounded to the operator's writes, and says so

Door 3 guards the path the operator controls. A cluster administrator who binds the unbound platform-admin role and edits a claim with `kubectl` is not stopped.

This is a deliberate scope, not an oversight. D16's text is *"when a provider module upgrade re-renders its registration with a `provides` set that drops one or more contracts still demanded by instances"* — the operator path is the decision's own subject. The capability spec states the bound, so nothing downstream reads a guarantee wider than what ships.

If the direct-write path later proves to matter, a webhook stacks on top of this without undoing it: door 3 would keep the operator honest and the webhook would cover everyone else.

### The refusal is reported on the instance, not on the claim

The blocked delete reports on the claim, because the claim is what was being deleted. Here the refused action is the instance's apply, so the instance is what reports.

D16 asks for *"the same shape and diagnostic as the finalizer's blocked delete"*. That is read as the same wording — the dropped contracts and the dependent count, so the operator's next action is obvious — not the same object.

**Alternative — also write a condition on the claim.** Rejected: the claim's conditions are owned by the claim reconciler, and a second writer on them is the pattern this repo has avoided everywhere else. The instance's message names the claim, so an operator starting from either object reaches the other.

### Drift detection skips a withheld resource

`detectDrift` runs on every reconcile, including no-ops, and compares desired against live via SSA dry-run. A withheld claim would differ every time, so the instance would carry `Drifted=True` permanently.

That reading would be wrong, not merely noisy: drift means the cluster diverged from what the operator asserts. Here the operator is deliberately not asserting the rendered claim. Reporting it as drift would flag a difference the operator created on purpose and intends not to close, and it would bury real drift on the same instance behind a condition that never clears.

So a withheld resource is excluded from the drift comparison, and the refusal condition carries the signal instead.

## Risks / Trade-offs

- [A provider upgrade blocks indefinitely while dependents exist] -> the same shape as the blocked delete, and the same escape: move the dependents off the contract, or keep the contract. The instance names both the contracts and the count, so the operator is not guessing. This is the guarantee working, not a failure mode.
- [The instance never reaches a no-op while an upgrade is refused] -> measured in section 1 by withholding one resource at the real position (after the render digest, before apply) and driving the reconcile repeatedly. **The behaviour depends entirely on whether the reconcile commits.** With the withhold reported as a failure — `reconciled` left false, `Ready=False`, `retryAfter = ComputeBackoff(...)` — the attempted render digest stays different from the applied one across every subsequent reconcile (`…6648c` attempted against `…732d4` applied, unchanged over four reconciles), so no reconcile is ever a no-op, and `RequeueAfter` walks the bounded exponential backoff 5s, 10s, 20s, 40s toward the 5-minute cap. It backs off; it does not hot-loop. With the withhold left on the success path instead, the applied digest is written from the *full* render — the one that was never applied — the instance reports `Ready=True`, and the very next reconcile is a no-op. **So section 2's refusal must return before the `reconciled = true` commit**, or the refusal erases itself one reconcile later.
- [Where the refusal returns decides what `Drifted` says] -> also measured in section 1. `status.ClearDrifted` runs immediately after a successful apply, so a refusal that returns after it clears the `Drifted` condition the same reconcile's `detectDrift` had just set, and drift never surfaces. A refusal returning before it leaves `Drifted=True` standing, which is the permanent-wrong-signal the drift exclusion exists to prevent: the same probe with no refusal at all reported `Ready=True` alongside `Drifted=True, 1 resource(s) drifted`, which is exactly the misreading section 3 removes. Section 3's exclusion is therefore load-bearing wherever section 2 puts the return, not a tidy-up.
- [A direct `kubectl` shrink is not stopped] -> stated in the capability spec and in the proposal's "Not in this change". Requires platform-admin RBAC, which ships unbound.
- [The instance reconcile learns a `TransformerRegistration` fact] -> the check lives behind its own seam that the reconcile calls, so the reconcile orchestrates a decision it does not implement (Principle II).
- [Two guards now share one dependent count] -> the delete guard and this one both read `status.requiredContracts`. A change to what the count means moves both, which is a reason to keep the count's definition in one place rather than a reason to duplicate it.

## Resolved Questions

1. **Does a withheld apply interact with `spec.rollout.forceConflicts`?** No, and structurally so. Confirmed in section 2: `withholdRefused` filters the list before `apply.Apply(ctx, applyRM, applyList, force)`, and `force` is only ever read inside that call. A withheld object is not in `applyList`, so it never reaches the force path at all — there is no ordering or interaction to reason about, and no spec or task changed as a result.

## What the tests do not assert

Two clauses of the capability specs are true of the implementation but are not directly asserted, recorded here so the gap is deliberate rather than discovered later.

- **"the instances demanding its contracts continue to render"** (registration-removal-guard, *The previously accepted claim keeps serving*). What the tests assert is the mechanism: the stored claim is unchanged down to its `resourceVersion`, and its `accepted` and `active` verdicts are untouched, so it stays in the platform's active set exactly as before the refused upgrade. Asserting the rendering itself needs a consumer driven through platform generation, which is a different subsystem from the one this change touches; the requirement stands, the coverage is bounded.
- **The refusal path's half of "the inventory still lists it, and the prune does not delete it"** is asserted twice over, but by two different mechanisms. `withhold_invariant_test.go` pins the general invariant — inventory built from the full rendered set is indifferent to what was applied — and `shrink_refusal_test.go` pins the refusal's own path, where the reconcile returns before both the inventory commit and the prune, so the previously committed inventory is retained rather than rewritten.
