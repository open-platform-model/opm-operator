# Tasks: envtest-coverage-batch

## 1. 0006 contract locks

- [ ] 1.1 `test/integration/apply/prune_test.go`: new Context mirroring the legacy-label case (`:375`) — live ConfigMap `managed-by: opm-cli` + matching instance UUID → deleted, `PruneResult.Deleted` incremented (0006 D40 window)
- [ ] 1.2 Reconcile the `prune-stale-resources` spec's label naming drift (`module-release.opmodel.dev/uuid` prose vs `module-instance.opmodel.dev/uuid` code) — naming only; correct the spec text via this change's delta if confirmed
- [ ] 1.3 Ownership tests (next to `moduleinstance_reconcile_test.go:370-616`): empty-`spec.owner` ModuleInstance → finalizer registered + stub-rendered resource applied (no skip, no ManagedExternally); API-level assertion that `spec.owner: "future-actor"` is enum-rejected

## 2. TODO stub fills (each its own commit; behavior bugs pause the change)

- [ ] 2.1 `test/integration/apply/apply_test.go:171` — CRD-before-CR apply ordering
- [ ] 2.2 `stale_pruning_test.go` — stale-set prune during reconcile; `prune=false` skip; selective multi-resource prune
- [ ] 2.3 `change_propagation_test.go` — values-change no-op path; source-revision propagation
- [ ] 2.4 `status_tracking_test.go` — ObservedGeneration tracking; history across outcomes; `forceConflicts` passthrough (cross-namespace sourceRef stub `:58` explicitly left for `add-cross-namespace-source-grants`)
- [ ] 2.5 `state_recovery_test.go` — Stalled→Ready, SoftBlocked→Ready, suspend→unsuspend recovery (failure staging via the impersonation-test helpers; flaky cases land stable-subset + explicit record, never a permanent Skip)

## 3. Hygiene + verification

- [ ] 3.1 Regenerate `dist/examples/` (`task examples:bundle`) — retires the stale `*-modulerelease.yaml` filenames
- [ ] 3.2 `task dev:test` + `task dev:lint` green; confirm no Skip/TODO stubs remain in the five files except the cross-namespace one
- [ ] 3.3 Sync/archive per openspec flow
