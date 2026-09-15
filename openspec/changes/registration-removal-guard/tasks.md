# Tasks: registration-removal-guard

Four sections. **Section 1 was a spike and it was load-bearing**: design.md committed to no approach
until it landed, because both open questions could change this change's API surface. It has landed
(2026-09-15) and 2.1 has corrected sections 2 and 3 against what it found — the approach, one extra
site the finalizer needs, and the CRD field this change now carries. Open Question 2 is still open
and section 4.1 answers it.

## 1. Spike — where the dependent count comes from

- [x] 1.1 Measure the persist candidate: what the render result already exposes about instance -> component -> transformer -> contract, where a `ModuleInstance` status write would land, and how stale the field would be under normal reconcile cadence. Verify: the finding names a concrete field shape and a concrete write site, or says why neither is available.
- [x] 1.2 Measure the recompute candidate: what re-deriving one instance's demand costs at finalizer time, and what happens when the registry is unreachable. Verify: the failure mode is measured, not reasoned about — the question is whether a delete can be blocked by an unrelated outage.
- [x] 1.3 Write the outcome into design.md, replacing "Candidate: persist demand at render time" with the decision and its evidence, and answer Open Question 1. If neither candidate is affordable, say so plainly and stop: the honest outcome is to re-scope with the enhancement rather than ship a guard that does not guard. Verify: design.md no longer describes two candidates; a reader can tell which was chosen and why.
- [x] 1.4 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `docs(design): settle where the registration dependent count comes from`.

## 2. The dependent count

Section 1 settled the approach: `ModuleInstance.status.requiredContracts` carries the instance's
DECLARED demand — the union of every component's `#resources` and `#traits` keys, which are already
contract FQNs in the keyspace `spec.provides` carries. This section adds the field and fills it.

- [x] 2.1 Re-read this section and section 3 against the spike's outcome and correct them before implementing. If the spike selected persistence, this change now carries a `ModuleInstance` CRD field and `proposal.md`'s Modified Capabilities and Impact need updating to say so. Verify: the artifacts describe what is about to be built, not what was guessed.
- [x] 2.2 Add `RequiredContracts []string` to `ModuleInstanceStatus` (`api/v1alpha1/moduleinstance_types.go`), documented as derived-never-authored, then `task dev:manifests dev:generate`. Verify: the CRD carries the field and `zz_generated.deepcopy.go` is regenerated, both by the generator rather than by hand.
- [x] 2.3 Derive the demand in `internal/render`: read each field of `instance.components` for its `#resources` and `#traits` keys (`cue.MakePath(cue.Def(...))`), sorted and deduped, and carry it on `render.RenderResult` as `RequiredContracts`. Compute it in `KernelModuleRenderer.RenderModule` from the instance it has already synthesized — no second acquisition, no second build, and no read of the platform. Verify: an integration spec renders the `hello` and `hello_web` fixtures and asserts the exact FQN sets; a component that declares nothing yields an empty slice, not nil-vs-empty churn on the status.
- [x] 2.4 Write it in `internal/reconcile/moduleinstance.go` beside the existing `status.InstanceUUID` write, before the no-op early return so the deferred patcher persists it on every successful render. Verify: a no-op reconcile still refreshes the field; a failed render leaves the previous value (over-reporting, the fail-closed direction); suspended and CLI-owned instances return before the render and keep theirs.
- [x] 2.5 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `feat(api): record each instance's declared contract demand on its status`.

## 3. The finalizer

The finalizer alone does not deliver the guarantee: `PlatformReconciler.activeClaims` drops a claim
the moment it carries a deletion timestamp, so a blocked claim's catalog still leaves the next
generated platform and its dependents are abandoned while the block reports that it is holding.
3.2 closes that; the two ship together or the guard is cosmetic.

- [ ] 3.1 Add the finalizer to `TransformerRegistration`, following `internal/reconcile/moduleinstance.go`'s pattern: add it when the claim is first accepted, check dependents on a deletion timestamp, remove it when the count reaches zero. The count is the number of `ModuleInstance`s whose `status.requiredContracts` intersect the claim's `spec.provides`. Note that finalizer patches do not bump generation, which matters because the claim reconciler filters on `GenerationChangedPredicate`. Verify: deleting a depended-on claim blocks and reports the count; the block releases when the last dependent goes; a claim with no dependents deletes immediately.
- [ ] 3.2 Make `PlatformReconciler.activeClaims` (`internal/controller/platform_controller.go`) keep a terminating claim in the active set while its deletion is blocked, and drop it only once the block has released. Verify: a regeneration triggered while a blocked claim is terminating still carries that claim's catalog, so its dependents keep rendering; a terminating claim with no dependents leaves the active set as it does today.
- [ ] 3.3 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `feat(controller): block deleting a claim its dependents still need`.

## 4. The shrink refusal

- [ ] 4.1 Choose the refusal site now that the count exists (design.md Open Question 2: validating webhook, or D16's hold-last-good fallback). Record the choice and its reason in design.md before implementing. If it is a webhook, revisit `proposal.md`'s SemVer and Impact notes, since the install surface changes. Verify: design.md answers Open Question 2 with a reason, not a preference.
- [ ] 4.2 Refuse an update that drops a contract dependents still demand, naming the dropped contracts and the count, with the same shape as the blocked delete. Verify: the refusal takes effect while the previously accepted claim is still effective — the test asserts dependents still render after the refusal, which is the whole point D16 makes; a same-contract-set upgrade and a drop nobody demands both pass untouched.
- [ ] 4.3 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `feat(controller): refuse a provides shrink that would abandon dependents`.
