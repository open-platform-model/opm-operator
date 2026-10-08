## 1. The apply guard (internal/apply, internal/status)

- [ ] 1.1 Add `Guard`, `GuardInput`, `GuardResult`, `Judged` and `GuardReadError` to `internal/apply` as `design.md` shapes them: one GET per object through the given reader, `ownership.CanApply` with `InInventory` matched by group, kind, namespace and name, a second question with the earlier identity on an ownership refusal, never on `terminating`, and `Admit` never set. A kind that is not served and that a CustomResourceDefinition of the same list defines counts as "does not exist" (reuse `pendingCRDKind`).
- [ ] 1.2 Unit tests for `Guard`: one object for each row of the table "Verdict and action per case" in `design.md`, the two identities, the read error, the unserved kind with and without its CRD in the list. The expected reason and message come from the library, never from a literal copy of its text.
- [ ] 1.3 Let `DetectDrift` take the guard's result in place of its own `refusedRead` GET, so drift sends no read of its own before `Diff`; keep the Forbidden outcome (`DriftCheckForbidden`) and its tests green.
- [ ] 1.4 Add to `internal/status` the reason constants `ApplyRefused` and `AdoptedElsewhere` and the note builders for the condition message and the two events, with the limits of `LeftBehindNote` (ten objects, 1024 characters, then a count), and their unit tests.
- [ ] 1.5 Extend the call-site test of `internal/apply` with the closed list of write call sites (`Create`, `Update`, `Patch`, `Apply`, `ApplyAll`, `ApplyAllStaged` on a client or a resource manager, matched by receiver type), the matcher test with a `testdata` file, and the check that no non-test file names the adopt annotation key.
- [ ] 1.6 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `feat(apply): add the ownership guard for an apply list`

## 2. ModuleInstance (internal/reconcile)

- [ ] 2.1 In `ReconcileModuleInstance`, run the guard after the client that applies is built and before drift detection, with the identities of the plan (the render's, then the earlier one while a change is not settled). Hand `Allowed` to drift detection and to the apply; compute the stale set from the full render as today.
- [ ] 2.2 Plan the write: with unchanged digests the restore list is the restorable missing objects plus `TakenIn`; a reconcile with `TakenIn` is not a no-op. Refuse, before the identity store and before any write, when `Refused` holds an object of the write list or an object that exists outside the inventory: `Ready=False` `ApplyRefused`, not Stalled, a failed apply for the counters, the backoff, the `ApplyRefused` event.
- [ ] 2.3 Let go: leave `LetGo` out of the apply list, of drift and of the restore; record the inventory without those entries on a success, a restore and a no-op (beside the expired Jobs, with the digest of the rendered set); emit the `AdoptedElsewhere` event on every reconcile that renders and finds one.
- [ ] 2.4 A failed guard read: a reconcile that would write fails through `markApplyFailure` with nothing written; a reconcile with unchanged digests reports it as the drift check does today, restores nothing, takes nothing in and lets nothing go.
- [ ] 2.5 Integration tests in `test/integration/reconcile` (envtest), one per scenario of the `ssa-apply`, `reconcile-loop-assembly`, `drift-detection`, `status-conditions` and `events-emission` deltas for a ModuleInstance. Each refusal test asserts that the refused object's resourceVersion did not change and that no other rendered object was created. Show for the foreign-object and the let-go test that they fail on the tree before 2.1.
- [ ] 2.6 Check that the specs of #260 to #271 still pass unchanged: impersonation, restore, expired Jobs, kept claims taken back, forced recreate, the two identities, `IdentityChangeUnsettled`.
- [ ] 2.7 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `feat(controller)!: guard every ModuleInstance apply with the library ownership verdict`

## 3. ModulePackage (internal/reconcile)

- [ ] 3.1 In the ModulePackage reconcile, build the client that applies before the no-op decision and run the guard there with the package's identities. A reconcile with matching digests applies `Allowed` when `TakenIn` is not empty or an inventoried object is in `LetGo`; otherwise it stays a no-op. A reconcile that skips its render runs no guard.
- [ ] 3.2 Refusal, let-go, the failed read, the two events and the inventory without let-go entries, as sections 2.2 to 2.4, through the package's own failure and commit paths.
- [ ] 3.3 Integration tests for the `modulepackage-reconcile-loop` delta and the package forms of the refusal, the adopt and the let-go; check that the package tests of the delete half still pass unchanged.
- [ ] 3.4 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `feat(controller)!: guard every ModulePackage apply with the library ownership verdict`

## 4. Documentation

- [ ] 4.1 Add `docs/site/operating/ownership-on-apply.md`: what the operator checks before it applies, the table of refusals with the way out for each (the table "Breaking changes and the way out" in `design.md`), the adopt annotation with the `kubectl annotate` line, the shared Namespace or CRD case first, the hand-over between two instances, and the module path change. Link it from `deletion-and-pruning.md` and correct there the passage on the adopt annotation that names an earlier identity.
- [ ] 4.2 Add `ApplyRefused` and `AdoptedElsewhere` where the operating docs list reasons and events. Check `docs/TENANCY.md` and say there that an impersonated ServiceAccount needs `get` on every kind it applies.
- [ ] 4.3 Record in `design.md` anything the build decided differently, and the migration note as it will go into the PR body.
- [ ] 4.4 `task dev:fmt dev:vet dev:lint dev:test docs:bundle:check` green, then commit `docs: explain the ownership check on apply and the adopt annotation`
