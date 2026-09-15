# Tasks: registration-removal-guard

Four sections. **Section 1 is a spike and it is load-bearing**: design.md deliberately commits to
no approach, because both open questions can change this change's API surface. Sections 2 to 4 are
written against what the spike finds, and 2.1 is the checkpoint where the plan is corrected before
anything is built on it.

## 1. Spike — where the dependent count comes from

- [ ] 1.1 Measure the persist candidate: what the render result already exposes about instance -> component -> transformer -> contract, where a `ModuleInstance` status write would land, and how stale the field would be under normal reconcile cadence. Verify: the finding names a concrete field shape and a concrete write site, or says why neither is available.
- [ ] 1.2 Measure the recompute candidate: what re-deriving one instance's demand costs at finalizer time, and what happens when the registry is unreachable. Verify: the failure mode is measured, not reasoned about — the question is whether a delete can be blocked by an unrelated outage.
- [ ] 1.3 Write the outcome into design.md, replacing "Candidate: persist demand at render time" with the decision and its evidence, and answer Open Question 1. If neither candidate is affordable, say so plainly and stop: the honest outcome is to re-scope with the enhancement rather than ship a guard that does not guard. Verify: design.md no longer describes two candidates; a reader can tell which was chosen and why.
- [ ] 1.4 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `docs(design): settle where the registration dependent count comes from`.

## 2. The dependent count

- [ ] 2.1 Re-read this section and section 3 against the spike's outcome and correct them before implementing. If the spike selected persistence, this change now carries a `ModuleInstance` CRD field and `proposal.md`'s Modified Capabilities and Impact need updating to say so. Verify: the artifacts describe what is about to be built, not what was guessed.
- [ ] 2.2 Implement the count as section 1 settled it, derived from actual demand and never from an author-writable field. Verify: the same cluster state yields the same count twice; an instance that stops demanding a contract lowers it with no edit to the claim.
- [ ] 2.3 `task dev:manifests dev:generate` if an API type changed, then `task dev:fmt dev:vet dev:lint dev:test` green, then commit the Conventional Commit section 2.1 settled on — `feat(api)` if a field was added, `feat(controller)` otherwise.

## 3. The finalizer

- [ ] 3.1 Add the finalizer to `TransformerRegistration`, following `internal/reconcile/moduleinstance.go`'s pattern: add it when the claim is first accepted, check dependents on a deletion timestamp, remove it when the count reaches zero. Note that finalizer patches do not bump generation, which matters because the claim reconciler filters on `GenerationChangedPredicate`. Verify: deleting a depended-on claim blocks and reports the count; the block releases when the last dependent goes; a claim with no dependents deletes immediately.
- [ ] 3.2 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `feat(controller): block deleting a claim its dependents still need`.

## 4. The shrink refusal

- [ ] 4.1 Choose the refusal site now that the count exists (design.md Open Question 2: validating webhook, or D16's hold-last-good fallback). Record the choice and its reason in design.md before implementing. If it is a webhook, revisit `proposal.md`'s SemVer and Impact notes, since the install surface changes. Verify: design.md answers Open Question 2 with a reason, not a preference.
- [ ] 4.2 Refuse an update that drops a contract dependents still demand, naming the dropped contracts and the count, with the same shape as the blocked delete. Verify: the refusal takes effect while the previously accepted claim is still effective — the test asserts dependents still render after the refusal, which is the whole point D16 makes; a same-contract-set upgrade and a drop nobody demands both pass untouched.
- [ ] 4.3 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `feat(controller): refuse a provides shrink that would abandon dependents`.
