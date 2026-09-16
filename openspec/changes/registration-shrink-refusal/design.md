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
- [The instance never reaches a no-op while an upgrade is refused] -> the render digest keeps differing from the applied digest, so the instance reconciles on its backoff instead of settling. Acceptable and arguably correct: it is genuinely not converged. Worth confirming in section 2 that it backs off rather than hot-loops.
- [A direct `kubectl` shrink is not stopped] -> stated in the capability spec and in the proposal's "Not in this change". Requires platform-admin RBAC, which ships unbound.
- [The instance reconcile learns a `TransformerRegistration` fact] -> the check lives behind its own seam that the reconcile calls, so the reconcile orchestrates a decision it does not implement (Principle II).
- [Two guards now share one dependent count] -> the delete guard and this one both read `status.requiredContracts`. A change to what the count means moves both, which is a reason to keep the count's definition in one place rather than a reason to duplicate it.

## Open Questions

1. **Does a withheld apply interact with `spec.rollout.forceConflicts`?** The force path recreates objects on immutable-field conflict; a withheld object never reaches it. Expected to be a non-question, and it changes no spec or task if it is not — section 2 confirms while writing the tests.
