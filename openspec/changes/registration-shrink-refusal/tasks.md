# Tasks: registration-shrink-refusal

Three sections. **Section 1 is a spike**, and a narrow one: the approach is settled, and what it
verifies is the single assumption everything else rests on — that withholding one resource from the
apply list leaves the inventory and the stale set untouched, so no prune fires and the accepted
claim survives. If that does not hold, the approach collapses and section 2 must not be built on it.

No API types change, so no section runs `task dev:manifests dev:generate`.

## 1. Spike — withholding one resource perturbs nothing else

- [x] 1.1 Prove the inventory invariant against the real reconcile: render an instance, drop one resource from the apply list, and assert the inventory entry for it is unchanged and `ComputeStaleSet` reports it as not stale. Verify: the assertion is on the entry and the stale set, not on `NewEntryFromResource` in isolation — the claim under test is that the reconcile's own inventory path is indifferent to what was applied.
- [x] 1.2 Establish what a withheld resource does to the reconcile's outcome and digests today, before any refusal exists: whether the render digest still differs from the applied digest, and whether the next reconcile backs off rather than hot-looping. Verify: the behaviour is measured and written into design.md's Risks section, replacing the "worth confirming" note with what was observed.
- [x] 1.3 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `test(reconcile): pin the inventory invariant a withheld apply depends on`.

## 2. The refusal

- [ ] 2.1 Add the shrink decision behind its own seam in `internal/` — given a rendered claim, the stored claim and the instances' demand, it answers whether the rendered one drops a contract dependents still need, and names them. Read the stored claim through the uncached `APIReader`, since a cached read can be stale and the comparison is the whole verdict. Verify: unit tests cover a shrink with dependents, a shrink with none, a same-set upgrade at a new version, a claim that is new, and an instance set that demands nothing.
- [ ] 2.2 Call it from the `ModuleInstance` reconcile between render and apply, withholding the claim from the apply list when it refuses, leaving the inventory built from the full rendered set. Verify: an integration spec renders a provider whose claim drops a demanded contract, and asserts the stored claim is byte-identical afterwards while every other rendered resource applied.
- [ ] 2.3 Report the refusal on the instance: not ready, naming the claim, the dropped contracts and the dependent count, in the blocked delete's wording. Leave the claim's own conditions untouched. Verify: the instance's condition carries all three facts; the claim's conditions are unchanged from before the refused reconcile.
- [ ] 2.4 Confirm the refusal releases by itself: with the dependents no longer demanding the contract, the next reconcile applies the upgrade and the instance converges, with no operator action on the claim. Verify: an integration spec drives that transition end to end.
- [ ] 2.5 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `feat(controller): refuse a provides shrink that would abandon dependents`.

## 3. Drift

- [ ] 3.1 Exclude a withheld resource from drift detection, so the difference the refusal created on purpose does not set `Drifted`. Verify: an instance with a withheld claim and no other divergence reports no drift; an instance with a withheld claim and a genuinely drifted resource reports drift for that resource alone; a resource that stops being withheld is included again.
- [ ] 3.2 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `fix(controller): keep a withheld resource out of drift detection`.
