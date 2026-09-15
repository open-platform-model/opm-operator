# Tasks: registration-acceptance

Four sections. Section 1 is a spike: design.md's inventory-based identity check replaces the
label-based one D11 suggested, and the claim that a rendered claim carries no namespace-bearing
label was measured in `catalog_opm`'s fixture rather than in a live cluster.

## 1. Spike — the identity signal, and the reconciler skeleton

- [x] 1.1 Confirm against a live claim what labels a rendered `TransformerRegistration` actually carries and what `ModuleInstance.status.inventory` records for it. Use the e2e or integration path that applies a module rendering the contract; if none exists, apply the CRD and a hand-built claim plus a fixture instance. Verify: the finding is written into design.md § The rendered claim carries no namespace-bearing identity label, confirming or correcting it — a spike that measures nothing has not run.
- [x] 1.2 Add `internal/controller/transformerregistration_controller.go`: watch the kind, patch status through a `patch.SerialPatcher`, set `conditions` and `observedGeneration`, and register in `SetupWithManager`, following `platform_controller.go`. No checks yet — every claim reconciles to a single "not yet judged" condition. Verify: applying a claim produces a status patch and `observedGeneration` matches its generation.
- [x] 1.3 Requeue rather than judge when `platform.Store.Generated()` reports no platform (design.md § A claim arriving before the platform is generated is a requeue). Verify: an integration test asserts no verdict is written and the claim is requeued, not refused.
- [x] 1.4 `task dev:manifests dev:generate`, then `task dev:fmt dev:vet dev:lint dev:test` green, then commit `feat(controller): reconcile TransformerRegistration claims without judging them`.

## 2. The catalog checks — D10 and D11

- [ ] 2.1 Acquire `spec.catalog` at `spec.version` through `Kernel.AcquireCatalogFromRegistry` and refuse a non-`#Catalog` on the library's `ErrWrongKind`, naming the kind found. Refuse an unresolvable coordinate distinguishably, naming the coordinate. Verify: the two refusals carry different reasons; the wrong-kind case asserts the sentinel with `errors.Is`, not a message match.
- [ ] 2.2 Compare `Catalog.Provides()` to `spec.provides` for exact equality, refusing drift in either direction and naming both lists. Verify: tests cover a claim naming an unimplemented contract, a claim omitting an implemented one, and an exact match; the match case passes regardless of the order the two lists were produced in.
- [ ] 2.3 Implement the identity check the spike settled (inventory-based per design.md, or the recorded fallback if 1.1 overturned it): refuse unless the `ModuleInstance` named by `spec.providerRef` exists and its `status.inventory` holds this claim. Requeue, do not refuse, while that instance's inventory has not settled. Verify: a claim whose `providerRef` names another instance is refused naming both identities; a claim applied before its instance's inventory is written is requeued.
- [ ] 2.4 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `feat(controller): accept a claim only against the catalog it names`.

## 3. The duplicate refusal — D12

- [ ] 3.1 Refuse every claim for a provider catalog another claim already holds, naming the holder by name. The holder is the earliest `metadata.creationTimestamp`, ties broken by name (design.md § D12's holder). Verify: the refusal names the accepted claim, and repeated reconciles with no spec change never move acceptance.
- [ ] 3.2 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `feat(controller): refuse a second claim for one provider naming the holder`.

## 4. The build-compatibility refusal — D8

- [ ] 4.1 Compare `Catalog.Requires()` against the built platform's resolved versions from `platform.Store` — not against `Platform.spec.registry` (design.md § D8 compares against the built platform's resolution). Per shared OPM-namespace path: refuse when the catalog requires a greater version within the same major; refuse unconditionally across majors. Verify: tests cover greater-same-major, different-major, and at-or-below; the last does not refuse.
- [ ] 4.2 The refusal message names the path, both versions, that the comparison is conservative (a `cue.mod` requirement records what the provider was tidied against, not what it uses), and that lowering the requirement is the fix. Verify: the test asserts the message carries all four, because D8 requires the wording, not just the refusal.
- [ ] 4.3 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `feat(controller): refuse a build-incompatible provider at acceptance`.
