# Tasks: registration-activation

Three sections. design.md's two research entries are readings of code already in this repo, not
assumptions to prove, so section 1 is not a spike.

## 1. The activation gate

- [ ] 1.1 Add a `ModuleInstance` watch to `TransformerRegistrationReconciler.SetupWithManager`, mapping a changed instance to the claims whose `spec.providerRef` names it — filtered, not the list-all `mapPlatformToRegistrations` uses, since instances are many and reconcile often (design.md § The ModuleInstance watch). Verify: an unrelated instance's reconcile enqueues no claim.
- [ ] 1.2 Set `status.active` when an accepted claim's provider `ModuleInstance` reports `Ready=True`, recording the transition on the claim's conditions. An unaccepted claim never activates. Verify: an integration test drives a provider from not-ready to ready and asserts the claim activates without its own spec changing; a refused claim with a ready provider stays inactive.
- [ ] 1.3 Make the latch structural: read `status.active` first and skip the gate entirely when it is already true, so no code path can clear the field (design.md § The latch is a state transition). Verify: a test drives the provider ready, then not-ready, then ready again, and asserts the claim stays active throughout with no second activation transition recorded.
- [ ] 1.4 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `feat(controller): activate an accepted claim when its provider is ready`.

## 2. D2 — one provider per contract

- [ ] 2.1 Refuse a claim whose `provides` names a contract an enabled `Platform.spec.registry` subscription already provides, naming the contract and the subscribed catalog. Read the contracts from the built platform in `internal/platform.Store`, requeueing rather than judging when it is absent, as the acceptance path already does. Verify: the refusal names both, and a claim judged before the platform is built is requeued.
- [ ] 2.2 Extend the refusal to contracts held by another **active** claim, naming the holder. An accepted but inactive claim does not hold a contract (design.md § D2's arm). Verify: three tests — refused against an active claim, not refused against an inactive one, and the refusal names the holder.
- [ ] 2.3 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `feat(controller): refuse a second provider for one contract`.

## 3. Docs

- [ ] 3.1 Record in `docs/` (or the controller's doc comment, whichever the reviewer prefers) that activation latches and why: the active-claim set is an input to platform regeneration, so health-driven deactivation would cause fleet-wide render churn from a transient condition. This is the rule most likely to be "fixed" into live-tracking by a later reader. Verify: the note names the consequence, not just the rule.
- [ ] 3.2 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `docs(controller): record why activation latches`.
