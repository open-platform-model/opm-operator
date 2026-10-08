## 1. Periodic requeue of a healthy ModuleInstance

- [ ] 1.1 Add `instanceRequeue` and `requeueJitter` in `internal/reconcile/moduleinstance.go`, `ReconcileInterval` on `ModuleInstanceParams`, and use it on the three healthy returns (NoOp, apply, skipped render); verify with a unit test of `instanceRequeue` (health requeue wins, `0` disables, the result is in `[interval, 1.1 x interval]`)
- [ ] 1.2 Add `ReconcileInterval` to `ModuleInstanceReconciler`, pass it in `Reconcile`, add the flag `--instance-reconcile-interval` (default 10m, negative refused, logged at start) in `cmd/main.go`; verify with a flag test in `cmd/flags_test.go` beside the drift render interval one
- [ ] 1.3 Add envtest specs in `internal/controller`: the apply, the NoOp and the skipped render requeue within `[interval, 1.1 x interval]`; a suspended and a CLI-owned instance return no requeue with the interval set; a skipped periodic reconcile sends no write (`patchCountingClient`) and calls no render; verify they fail without 1.1
- [ ] 1.4 Update the comments that say ModuleInstance is watch-only on the happy path (`internal/reconcile/outcome.go`, `healthRequeue` doc, the `--drift-render-interval` help text)
- [ ] 1.5 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `fix(controller): requeue a healthy module instance on a fixed interval`

## 2. Restore of missing objects

- [ ] 2.1 `internal/apply/drift.go`: report the objects whose dry-run answers `CreatedAction` in `DriftResult.Missing`; verify with a test in `test/integration/apply` or the package's own tests that a missing object is listed and is not counted as drifted
- [ ] 2.2 `internal/reconcile/moduleinstance.go`: `detectDrift` returns the missing set; add `restorable` (drops a `batch/v1` Job with `ttlSecondsAfterFinished`); on matching digests with a restorable set, apply that set only, keep the computed `Drifted`, end as `Applied`; verify with a unit test of `restorable`
- [ ] 2.3 Add envtest specs: a deleted ConfigMap is created again by a rendering reconcile with outcome applied and a moved `lastAppliedAt`; a modified object beside it keeps its content and `Drifted=True`; a skipped render restores nothing; an unchanged instance still ends `NoOp` with no apply
- [ ] 2.4 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `fix(controller): restore a missing object of a module instance`

## 3. Docs and decision record

- [ ] 3.1 Add `adr/019-fixed-requeue-interval-for-module-instances.md` (context, options, decision, consequences); add a status note to ADR-016 that ADR-019 replaces its planned-mechanism sentence, and a pointer in ADR-012 that a missing object is restored; verify the ADR follows `adr/TEMPLATE.md`
- [ ] 3.2 Update `docs/RENDERING.md` (the drift render interval section: what schedules a reconcile, the restore, the cost with one slot) and `docs/site/diagnostics/operator-conditions.md` (the `Healthy` and `Drifted` rows that say an instance is not requeued); verify with `task docs:bundle:check`
- [ ] 3.3 `task dev:fmt dev:vet dev:lint dev:test docs:bundle:check` green, then commit `docs: record the periodic reconcile and the restore of module instances`
