## Why

An active `TransformerRegistration` is a claim the operator has judged, accepted and gated on its provider's readiness — and nothing reads it. The generated platform package is still a pure function of the Platform CR's spec alone, so a provider module that installs successfully and registers successfully still cannot render anything: its catalog is not in the platform the render builds against.

Enhancement 0015 D13 closes that: the generated package is a function of **one tuple** — the Platform CR's spec and the set of accepted-and-active claims — regenerated edge-triggered and level-computed. D17 amends how the operator holds the result: one generated package per D13 identity rather than one per Platform CR generation, because a claim change that leaves the generation unchanged still produces a different package.

This is the change that makes the second transformer path actually reach a render.

## What Changes

- **D13 — regeneration keyed on the tuple.** The Platform reconciler watches `TransformerRegistration` and regenerates whenever the active-claim set changes. The package is computed from the current tuple, **never from the event's content**, so rapid changes coalesce for free and a burst of provider installs converges in one or few regenerations.
- **D13 — the active providers' catalogs are imported.** The generated platform module gains an import and a `#registry` entry per active claim, beside the subscriptions the Platform CR authored.
- **D13 — package identity.** `#EffectiveRegistry.key` — the CR generation plus the sorted `catalog@version` list of active claims — is stamped into the generated package and surfaced in `Platform.status`, so every render is attributable to an exact registry state.
- **D13 — `Platform.status.registry` carries the resolved union**, making the effective set enumerable from the Platform rather than only by listing claims.
- **D17 — the store is re-keyed.** `internal/platform.Store` holds `generated` and `leases` keyed on `generation int64`; both become keyed on package identity. A package stays readable while any render leases it, and every render reports the identity it consumed.

## Not in this change

- **D14's readiness exclusion.** Still nothing to exclude from — `ModulePackage.Ready` is set from the reconcile outcome alone, and the only waiting is Flux's `WaitForSet` on the cluster-definition and class stages, never the general resource stage. It belongs to whichever change introduces a general-stage wait.
- **Moving acceptance into the Platform reconciler.** D13's text says the woken reconcile "runs the full acceptance battery in place"; acceptance already ships in its own reconciler (alpha.19) and stays there. See design.md.
- **Anything about removal.** The finalizer and the shrink refusal are `registration-removal-guard`.

## Depends on

`registration-activation`. This change reads `status.active`, which nothing sets until that lands. Sequencing them the other way round would mean regenerating on a set that is always empty.

## Impact

- **API types**: `PlatformStatus` gains the resolved registry union and the package identity. `TransformerRegistration` is unchanged.
- **`internal/platform`**: `Store` changes shape — this is the one piece of existing machinery this change rewrites rather than extends. Its four callers (`platform_controller.go`, both renderers, the claim reconciler) move with it.
- **Controllers**: the Platform reconciler gains a watch and a larger generate step. The claim reconciler is untouched except that its `Store.Lease()` call follows the new key.
- **Downstream consumers**: a render now resolves contracts a provider catalog supplies, which is the point. No existing platform changes behaviour: with no active claims the tuple reduces to the CR spec and the generated package is what it is today.
- **SemVer**: MINOR. Additive status fields, no existing behaviour removed.
- **Complexity (Principle VII)**: the identity re-key is real cost, and D17 exists because the cheaper alternative is wrong — keying on generation alone means a claim activating produces no new package, so the render keeps consuming a platform that does not contain the provider it just accepted.

## Capabilities

### New Capabilities

- `registration-driven-regeneration`: what wakes regeneration, what the generated package is a function of, how it is identified, and what a render reports about the state it consumed.

### Modified Capabilities

- `platform-module-generation`: its generated package is currently a function of the Platform CR alone. That becomes the tuple.
- `platform-reconciler`: gains the claim watch and the status union, if its requirements name the reconciler's trigger set.

Both are confirmed against their current spec text during the change rather than guessed here; if one turns out not to state the assumption, its delta is dropped.

## Impact on existing behaviour

A cluster with no active claims generates the same package it does today. A cluster with one generates a package that additionally imports the provider's catalog, and renders against it — which is new capability, not changed behaviour, since nothing could previously produce an active claim.
