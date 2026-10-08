## 1. An expired Job with a TTL is finished

- [x] 1.1 `internal/apply/drift.go`: add `Expired` (the missing resources that `Restorable` leaves out); verify with a unit test in `internal/apply/restore_test.go` that `Restorable` and `Expired` split a missing set with no overlap
- [x] 1.2 `internal/reconcile/moduleinstance.go`: with unchanged digests, remove the expired Jobs from the inventory entries on a `NoOp` and on a restore, keep `inventory.digest` as the rendered digest, and judge health over the kept entries; verify with a unit test of the entry filter (`TestWithoutEntries`, `TestForgetExpiredJobs`) and, for the digest that the deferred commit writes, the envtest assertions on `status.inventory.digest` after a `NoOp` and after a restore
- [x] 1.3 `internal/reconcile/health.go` and the skip path: the verdict reports an absent `batch` Job entry, and a skippable reconcile renders when it does and `failureCounters.drift` is zero; verify with a unit test of the verdict flag
- [x] 1.4 `internal/controller/restore_missing_test.go`: change the spec that pins `Healthy=False` for the expired Job to the new verdict, and add specs through a reconcile for: a Job with a TTL seen complete and then removed (on the skip path, with one render); a Job without a TTL that is missing (restored); a Job with a TTL removed before it ran (finished, not restored); a failed dry-run (reported `Missing`, no render loop); verify each fails when the rule is removed
- [x] 1.5 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `fix(controller): treat an expired Job with a TTL as finished`

## 2. Docs and decision record

- [x] 2.1 `docs/RENDERING.md`, `docs/site/diagnostics/operator-conditions.md` and `adr/019-fixed-requeue-interval-for-module-instances.md`: state the Job rule and correct every sentence that says an expired Job reads `Missing`; ADR-019 stays "Accepted"; verify with `grep -n -i ttl` over the three files and `task docs:bundle:check`
- [x] 2.2 `task dev:fmt dev:vet dev:lint dev:test docs:bundle:check` green, then commit `docs: state that an expired Job with a TTL is finished`

## 3. Review fixes

- [x] 3.1 Add envtest specs for a Job with a TTL that failed and expired (finished, not run again) and for an instance whose only object was an expired Job (`HealthUnknown`, empty inventory); verify both pass
- [x] 3.2 State both cases in `docs/RENDERING.md`, the `NotRolledOut` row of `docs/site/diagnostics/operator-conditions.md`, ADR-019 and the delta specs; add the `inventory-bridge` delta for the stored digest; verify with `openspec validate treat-expired-ttl-job-as-finished --strict`
- [x] 3.3 `task dev:fmt dev:vet dev:lint dev:test docs:bundle:check` green, then commit `test(controller): pin a failed and a lone expired Job, and state both`
