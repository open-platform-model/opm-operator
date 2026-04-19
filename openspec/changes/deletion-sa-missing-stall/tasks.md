## 1. Status primitives

- [ ] 1.1 Add `DeletionSAMissingReason = "DeletionSAMissing"` and `OrphanedOnDeletionReason = "OrphanedOnDeletion"` constants to `internal/status/conditions.go`
- [ ] 1.2 Add `AnnotationForceDeleteOrphan = "opm.dev/force-delete-orphan"` constant (either in `api/v1alpha1/` or in a suitable shared package — follow existing annotation conventions)

## 2. Detection helper

- [ ] 2.1 Add helper `isServiceAccountNotFound(err error) bool` in `internal/apply/impersonate.go` (or colocated with `NewImpersonatedClient`) that returns true iff `apierrors.IsNotFound(err)` on the wrapped chain
- [ ] 2.2 Unit test the helper: NotFound wrap → true; other wrapped errors → false; nil → false

## 3. ModuleRelease deletion path

- [ ] 3.1 Rework `handleDeletion` in `internal/reconcile/modulerelease.go` (around line 508-544): remove the silent fallback (`deleteClient = params.Client` on impersonation error)
- [ ] 3.2 On impersonation failure, branch:
  - If `isServiceAccountNotFound(err)` AND orphan annotation is set to `"true"`: clear inventory, emit `Warning` event (`OrphanedOnDeletion`, with orphan count), remove finalizer, return nil
  - If `isServiceAccountNotFound(err)` AND orphan annotation absent/non-`"true"`: `MarkStalled(mr, DeletionSAMissingReason, <template>)`, emit `Warning` event once on transition, retain finalizer, return non-nil error so controller-runtime requeues on the stalled recheck interval
  - If impersonation error is anything else: existing stall path with `ImpersonationFailed`
- [ ] 3.3 Replace the misleading `INFO "ServiceAccount unavailable for deletion cleanup, using controller client"` log line with the new branches' logs (per design.md §D7)

## 4. Release deletion path

- [ ] 4.1 Apply the same edit to the equivalent block in `internal/reconcile/release.go` (around line 613-628)
- [ ] 4.2 Confirm no ordering dependency between `ModuleRelease` and `Release` reconciles that requires a shared helper — if the code bodies are nearly identical, leave duplication; do not refactor in this change (scope discipline)
- [ ] 4.3 Confirm `BundleRelease` has no deletion-time impersonation; if it does, apply the same pattern; if not, add a one-line comment in the `BundleRelease` controller noting the absence so future readers know the guardrail was considered

## 5. Unit tests

- [ ] 5.1 ModuleRelease: SA missing, no annotation → stall with `DeletionSAMissing`, event emitted, finalizer retained, inventory unchanged
- [ ] 5.2 ModuleRelease: SA missing, orphan annotation `"true"` → `OrphanedOnDeletion` event, finalizer removed, `status.inventory` cleared in the final patch
- [ ] 5.3 ModuleRelease: SA missing, annotation set to `"yes"` → behaves as no annotation (stall, no orphan-exit)
- [ ] 5.4 ModuleRelease: SA present but impersonate RBAC denied → existing `ImpersonationFailed` stall; orphan annotation ignored
- [ ] 5.5 ModuleRelease: SA present, prune succeeds → existing clean-exit path unchanged
- [ ] 5.6 Repeat the five cases for `Release` in `internal/reconcile/release.go`
- [ ] 5.7 Event-dedup test: SA-missing stall that requeues N times emits the `DeletionSAMissing` event exactly once per Ready transition (not per reconcile)

## 6. Integration test (envtest)

- [ ] 6.1 Extend `test/integration/reconcile/impersonation_test.go` with a scenario: apply MR with SA, assert apply ok; delete SA; delete MR; assert release stalls with `DeletionSAMissing`, finalizer retained; patch orphan annotation; assert finalizer removed and `OrphanedOnDeletion` event recorded

## 7. Documentation

- [ ] 7.1 Update `docs/design/impersonation-and-privilege-escalation.md` §"What the controller already enforces" table with the new row for SA-missing-during-deletion
- [ ] 7.2 Add a short operator runbook section to `docs/TENANCY.md` (the file being introduced by the `default-sa-and-tenancy-guide` change — coordinate merge order, or add a placeholder section here and the other change fills in): "Recovering a release stuck on DeletionSAMissing"
- [ ] 7.3 If `default-sa-and-tenancy-guide` has not merged by the time this change is ready, add the runbook section to a new `docs/RUNBOOK.md` instead; reconcile on merge
- [ ] 7.4 Update CHANGELOG entry noting the behavior change (deletions that previously looped forever now stall visibly)

## 8. Validation gates

- [ ] 8.1 `task dev:fmt dev:vet`
- [ ] 8.2 `task dev:lint`
- [ ] 8.3 `task dev:test`
- [ ] 8.4 Kind smoke: apply `hello` fixture, delete it via `kubectl delete -f`, confirm release stalls with clear message (not silent retry loop), apply orphan annotation, confirm finalizer removed
- [ ] 8.5 Confirm no generated-file churn (no CRD/API schema changes)
