## 1. The apply keeps claims on a forced recreate

- [ ] 1.1 In `internal/apply`: add `ApplyOptions{Force, DeleteData bool}` as the last parameter of `Apply` in place of `force bool`; add `ClaimConflictError` (namespace, name, refused fields, cause; `Unwrap` returns the cause); add the claim check of `design.md` decision 2 before the staged apply. Update every caller (`internal/reconcile`, `test/integration`): the reconcilers pass `DeleteData` from `spec.dataPolicy.DeletesClaims()`. Update the doc comments of `Apply` and `Prune` (the exception sentence in `prune.go` goes). Verify: `go build ./... && go vet ./...` pass.
- [ ] 1.2 In `internal/apply/manager.go`: wrap the client that `NewResourceManager` hands to Flux so that `Delete` and `DeleteAllOf` of a core PersistentVolumeClaim are refused unless the context carries the permission `Apply` sets under `DeleteData` (decision 4). Verify: a unit test in `internal/apply` with a fake client for the claim, another kind, and the permission.
- [ ] 1.3 Add specs against a real API server in `test/integration/apply`, one per scenario of the `ssa-apply` delta: claim kept with its UID and the ConfigMap of the same set unchanged; claim recreated under `DeleteData`; an immutable ConfigMap recreated as before; a label change on a claim applied; the resource manager's client refuses a claim delete. Verify: with the check and the wrapper switched off the kept-claim spec fails; record the observed failure in the report.
- [ ] 1.4 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `fix(apply)!: keep PersistentVolumeClaims on a forced recreate unless spec.dataPolicy is Delete`

## 2. The reconcilers report the conflict

- [ ] 2.1 Add `ClaimConflictReason = "ClaimConflict"` to `internal/status/conditions.go` and one helper that formats the message of `design.md` decision 5 from the typed error. Verify: a unit test of the text.
- [ ] 2.2 ModuleInstance and ModulePackage: on a `ClaimConflictError` from `Apply`, set `Ready=False` with `ClaimConflict`, emit one `Warning` event with reason `ClaimConflict` and action `Apply`, no `ApplyFailed` event, outcome transient with the backoff. Verify with specs in `test/integration/reconcile`, one per scenario of the `status-conditions`, `events-emission`, `modulepackage-reconcile-loop` deltas and the new `prune-stale-resources` scenario; search `test/integration` and `test/e2e` for assertions this change makes stale.
- [ ] 2.3 Add an e2e spec beside the two claim specs of `test/e2e/podinfo_test.go` if the redis fixture can express it (a `storageClass` change under `forceConflicts`). It is not run in this change. Verify: `go vet -tags=e2e ./test/e2e/` passes.
- [ ] 2.4 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `fix(reconcile): report a refused claim recreate as ClaimConflict`

## 3. API texts

- [ ] 3.1 Rewrite the last paragraph of the `DataPolicy` doc comment on both kinds and the `spec.prune` sentence it depends on, per the `prune-stale-resources` delta; say in the `ForceConflicts` doc comment what the field does and that claims follow `spec.dataPolicy`. Run `task dev:manifests dev:generate` and `task operator:installer`. Verify: `task operator-module:drift` passes; `dist/install.yaml` differs only in the CRD descriptions (restore any other line, the image included).
- [ ] 3.2 `task dev:manifests dev:generate dev:fmt dev:vet dev:lint dev:test docs:bundle:check` green, then commit `fix(api): describe the forced recreate under spec.dataPolicy`

## 4. Docs and the decision record

- [ ] 4.1 `docs/site/operating/deletion-and-pruning.md`: replace the "forced recreate" exception by the rule, the `ClaimConflict` report and the ways out. `docs/site/diagnostics/operator-conditions.md`: add the `ClaimConflict` row. Amend `adr/020-data-claims-kept-by-default.md` (the exception paragraph and its negative consequence). Search `docs/`, `adr/`, `modules/opm_operator/` and `README.md` for other texts that name the exception. Verify: `task docs:bundle:check` passes.
- [ ] 4.2 `task dev:fmt dev:vet dev:lint dev:test docs:bundle:check` and `task operator-module:drift` green, then commit `docs: describe the forced recreate of claims under spec.dataPolicy`
