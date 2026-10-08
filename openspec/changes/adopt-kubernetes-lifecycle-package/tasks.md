## 1. Spike: Foreground deletes under envtest

- [ ] 1.1 Write an envtest test in `internal/apply` that deletes a Deployment with Foreground propagation and asserts what the API server does without a garbage collector: the object stays with a `deletionTimestamp` and the `foregroundDeletion` finalizer. Verify: the test passes and its outcome is written into design.md ("Risks") as a finding, replacing "unverified assumption"
- [ ] 1.2 Add a test helper in `internal/controller` (and one for `internal/apply` if the suites do not share one) that removes the `foregroundDeletion` finalizer from terminating objects, on demand and as a background loop a test can start and stop. Verify: a test deletes with Foreground, runs the helper and reads NotFound
- [ ] 1.3 Check with a unit test against the fake client used by `internal/apply/prune_test.go` whether it honours a propagation policy and a UID precondition; record in design.md what the unit tests can assert and what only envtest can. Verify: the note is in design.md
- [ ] 1.4 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `test(apply): prove foreground deletes under envtest and add a collector helper`

## 2. Internal packages: the plan runner

- [ ] 2.1 Add `internal/apply/deletion.go`: `StepResult`, `DeletionRun` and the runner that drives one `lifecycle.DeletionPlan` from the zero State to done with a `client.Client`, passing the action's propagation and preconditions unchanged and wrapping a Conflict on a preconditioned delete in `ErrReplaced`. Verify: table test with one entry per `Advance` outcome (deleted, already absent, each skip reason, failed read, forbidden, conflict, read that returns another object)
- [ ] 2.2 Add the per-identity driver: plan 1 with the first identity over all entries; entries skipped as `owner-mismatch` or `adopted-elsewhere` form the plan of the next identity; an empty identity list is one plan with no identity; an entry no plan deletes keeps the reason and message of plan 1. Verify: the cases of `TestPruneJudgesWithEachIdentity` and `TestPruneAdoptAnnotationAndIdentities` (`prune_test.go`) pass against the driver
- [ ] 2.3 Add the claim split and the read-only classification of kept claims (kept after a proceed verdict, left behind after a skip, gone, unreadable kept without an error). Verify: the kept-claim cases of `prune_claims_test.go` pass against it
- [ ] 2.4 Test the order: a plan over entries in inventory order sends its DELETEs in descending kind weight, and the request carries `Foreground` and the UID. Verify: the test reads the recorded requests
- [ ] 2.5 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `refactor(apply): add the deletion plan runner`

## 3. Internal packages and controller: stale prune on the plan

- [ ] 3.1 Rewrite `apply.Prune` as a caller of the runner with `lifecycle.Policy{Prune: true}`, keeping its signature, `PruneResult`, `LeftBehind`, the joined error and the log lines. Remove `judgeDelete` from the delete path (keep it only if the claim classification uses it). Verify: `prune_test.go`, `prune_claims_test.go`, `internal/reconcile/prune_outcome_test.go` and `left_behind_test.go` pass with no change other than expected request order and propagation
- [ ] 3.2 Update `TestDeleteCallSites` (`internal/apply/callsites_test.go`): allowed sites are `internal/apply/deletion.go` (1) and `internal/apply/claims.go` (2). Verify: the test fails when a `Delete` is added to `prune.go`
- [ ] 3.3 Envtest: a stale Deployment is deleted with Foreground, the reconcile ends `Ready=True` while it still terminates, and the inventory no longer lists it (ModuleInstance and ModulePackage). Verify: the two specs pass
- [ ] 3.4 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `feat(apply)!: prune stale resources through the library deletion plan`

## 4. Controller: deletion cleanup on the plan and the hold verdict

- [ ] 4.1 In `handleDeletion` (`internal/reconcile/moduleinstance.go`): build `lifecycle.Policy{Prune, ForceOrphan}` and `lifecycle.HoldInput{Identity}`, run the plans, and branch on `lifecycle.MayReleaseHold` for every plan. Keep each status, event, log line and requeue interval of today's branches (table in proposal.md, item 2). Verify: `internal/reconcile/deletion_test.go` and `internal/controller/moduleinstance_deletion_wake_test.go` pass unchanged
- [ ] 4.2 The same in `handleModulePackageDeletion` (`internal/reconcile/modulepackage.go`). Verify: the ModulePackage deletion tests pass unchanged
- [ ] 4.3 Add a table test that pairs every `HoldReason` with the operator's outcome (finalizer, condition reason, event, requeue), for both kinds. Verify: one row per reason, seven rows
- [ ] 4.4 Test that with a missing or failed identity no object is read or deleted, and that force-orphan does not lift a failed step. Verify: both tests pass
- [ ] 4.5 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `feat(controller)!: run the deletion cleanup on the library plan and its hold verdict`

## 5. Controller, status and docs: wait until deleted objects are gone

- [ ] 5.1 Add the reasons `DeletionInProgress` and `DeletionBlocked` and their notes to `internal/status` (at most ten objects named, the rest counted; the blocked note names the ways out). Verify: unit tests for both notes, the cut included
- [ ] 5.2 After a release verdict, read every `ResultDeleted` step with the delete client; gone means NotFound or another UID. With objects left: mark `DeletionInProgress` and requeue after 10 s, or after 10 minutes from the oldest `deletionTimestamp` mark `DeletionBlocked` and requeue after 1 minute; emit each event once. Both kinds. Verify: envtest specs for each scenario of the two new `finalizer-and-deletion` requirements, using the collector helper of section 1
- [ ] 5.3 Test that `spec.prune=false` on a waiting deletion releases the finalizer at the next reconcile, and that a kept claim, an object left behind and an absent object are not waited for. Verify: the tests pass
- [ ] 5.4 Update `docs/site/operating/deletion-and-pruning.md`, `docs/site/operating/delete-an-instance-safely.md` and `docs/site/diagnostics/operator-conditions.md`: Foreground and the order, the wait, the two reasons, the two ways out. Verify: `task docs:bundle:check` passes
- [ ] 5.5 Run the kind e2e suite once against a real garbage collector (`task dev:e2e`, its own cluster `opm-operator-test-e2e`) when the session may use a cluster; otherwise leave this box open and say so. Verify: the lifecycle specs pass, and the result is written into design.md ("Risks")
- [ ] 5.6 `task dev:fmt dev:vet dev:lint dev:test docs:bundle:check` green, then commit `feat(controller)!: keep the cleanup finalizer until deleted objects are gone`
