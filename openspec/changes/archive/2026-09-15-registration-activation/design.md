# Design: registration-activation

## Context

See `proposal.md` for motivation. Current state, read 2026-09-15 at alpha.19:

- `internal/controller/transformerregistration_controller.go` holds the acceptance reconciler: `accept`, `refuse`, `deferVerdict`, `transitioned`, a `patch.SerialPatcher`, and `status.MarkReadyWithReason` / `MarkStalled` / `AcceptedReason`.
- It watches `TransformerRegistration` under `predicate.GenerationChangedPredicate`, plus `Platform` through `mapPlatformToRegistrations`, which lists every claim and enqueues it. A second cross-object watch follows that pattern exactly.
- `checkProviderIdentity` already resolves `spec.providerRef` to a **`ModuleInstance`** and reads its `status.inventory`.
- `accept` writes `Accepted = true` and leaves `Active` untouched; its doc comment says activation is a later change.
- `internal/reconcile/moduleinstance.go` sets the instance's own `Ready` via `status.MarkReady`.

## Goals / Non-Goals

**Goals**

- An accepted claim activates promptly when its provider becomes ready, without waiting for an unrelated reconcile.
- Activation latches, so provider health noise cannot move the active set.
- D2's second arm exists, so two providers of one contract is refused where the diagnostic can name both.

**Non-Goals**

- The finalizer, the shrink refusal, and any dependent counting — `registration-removal-guard`.
- `Platform.status.registry` and regeneration — `registration-driven-regeneration`.
- Re-opening any check acceptance already performs.

## Decisions

### The gate reads the provider's `ModuleInstance`, not a `ModulePackage`

D3's prose says *"`spec.providerRef` names the provider's ModulePackage"*. It does not, and cannot:

- D11 stamps `providerRef` from `#TransformerContext` **instance** metadata, so what the renderer writes is an instance coordinate.
- `checkProviderIdentity` already resolves it as a `ModuleInstance` and verifies ownership through that instance's `status.inventory`. Reading the same field as two different kinds in one controller would be incoherent.
- There is no link to traverse: `api/v1alpha1/moduleinstance_types.go` carries no `ModulePackage` reference, and nothing sets a controller reference between the two kinds.

`ModuleInstance` sets its own `Ready` condition, which carries the fact the gate needs — the provider's resources applied and settled, so its CRDs exist. D3's wording is loose about the kind; its *intent* is provider readiness, and the instance is where that is recorded for the object `providerRef` names.

**Alternative considered — resolve the owning `ModulePackage` and gate on its `Ready`.** Matches D3's and D14's wording, and would need an ownership edge this repo does not have. Building one to satisfy a prose detail, when the object named already reports readiness, is cost without a guarantee.

### The latch is a state transition, not a recomputation

Activation is written once and never reconsidered:

```
  accepted && !active && provider Ready=True   ->  set active = true
  active                                       ->  no further evaluation
```

The reconciler reads its own `status.active` first and skips the gate entirely when it is already true. This is what makes the latch structural rather than a rule someone must remember: there is no code path that can clear the field, so no future edit to the readiness check can accidentally introduce flapping.

**Alternative considered — recompute `active` from provider readiness each reconcile.** The obvious implementation, and the one D3 rejects by name. A flapping provider would toggle the active-claim set; D13 keys platform regeneration on that set, so every dependent instance would re-render, and fail, for the duration. The failure is fleet-wide and caused by a transient condition.

### The `ModuleInstance` watch maps by `providerRef`, not list-all

`mapPlatformToRegistrations` lists every claim because the Platform is a cluster singleton whose changes are rare. `ModuleInstance` is neither: a fleet has many, and they reconcile often. The map func therefore filters — enqueue only claims whose `spec.providerRef` matches the instance that changed.

A field index on `spec.providerRef` is the natural way to do that lookup. Whether it is worth one, or whether a filtered list-all is enough at the expected claim count (one per provider instance, so tens at most), is a judgement for the implementation; the requirement is that an unrelated instance's reconcile does not enqueue every claim in the cluster.

### D2's arm keys on contracts, and only active claims hold

The existing D12 refusal compares `spec.catalog` between claims. D2's arm compares **contract FQNs** across two sources: the contracts provided by enabled `Platform.spec.registry` subscriptions, and the `provides` of other **active** claims.

An accepted-but-inactive claim deliberately does not hold a contract. A claim that has never served has no dependents to protect, and letting it block a competitor would let a provider that never came up lock out one that did.

## Research & Decisions

### Nothing links a `ModuleInstance` to a `ModulePackage`

**Context**: whether D3's readiness gate can be read off a `ModulePackage` as its text says.
**Explored**: `api/v1alpha1/moduleinstance_types.go` for a package reference; `internal/controller/modulepackage_controller.go` and `internal/reconcile/modulepackage.go` for `SetControllerReference` or an `Owns` edge.
**Decision**: gate on the `ModuleInstance`'s own `Ready`.
**Rationale**: neither the API type nor the reconcilers establish the relationship, so there is no edge to follow from the coordinate `providerRef` carries. The instance reports `Ready` itself, so the fact is available on the object actually named.

### Acceptance leaves a seam for the latch

**Context**: whether activation can be added without disturbing the shipped verdict path.
**Explored**: `accept` at `internal/controller/transformerregistration_controller.go:349` and its doc comment.
**Decision**: activation is a separate transition after `accept`, reading `status.active` to decide whether the gate runs at all.
**Rationale**: `accept` already writes `Accepted = true` and deliberately leaves `Active` alone, and its "No requeue" reasoning enumerates what can invalidate a verdict. Provider readiness is a new entry in that list, which the watch supplies rather than a requeue.

## Risks / Trade-offs

- [The latch means a claim can be active while its provider is gone entirely, not merely unhealthy] -> deletion is the designed exit, and it is `registration-removal-guard`'s subject. Until that lands, an orphaned active claim is visible in `kubectl get transformerregistrations` and affects nothing, because nothing reads the active set yet.
- [D2's arm needs the contracts an enabled subscription provides, which is platform-derived data] -> the built platform is already in `internal/platform.Store`, and the acceptance path already requeues rather than judging when it is absent. The same handling applies.
- [A field index on `spec.providerRef` is new machinery in this controller] -> optional; the requirement is only that an unrelated instance does not enqueue every claim. A filtered list is acceptable at the expected scale and can be indexed later. What a filtered list does not save is the cost of the reconciles it does enqueue, and a verdict is not cheap: it re-acquires the claimed catalog from the registry, which holds no in-process cache. The watch therefore also carries a predicate passing only the updates that moved a fact a verdict reads — the Ready condition, or the inventory digest the identity check looks the claim up in — so a provider that is applying, drifting or retrying does not re-judge its claim on every status write.

- [A claim accepted before a competitor activated keeps a stale `accepted: true`] -> `accept` does not requeue, and nothing enqueues competitors when a claim activates, so two claims can both report accepted for one contract until the loser is next judged. The window is reporting only: the loser cannot activate wrongly, because it is re-judged when its own provider becomes ready, which is the only moment it would activate, and D2 refuses it there. Enqueuing every competitor on activation would close the reporting window at the cost of a list-all enqueue on a latched, once-per-claim event; that trade is not taken here.

## Open Questions

None.
