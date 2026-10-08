## 1. Release the finalizer of a deleting CLI-owned instance

- [x] 1.1 Add a spec to `internal/controller/moduleinstance_reconcile_test.go` (context "CLI-owned instance"): a CLI-owned instance with the finalizer, `spec.prune: true` and an inventory naming a managed ConfigMap is deleted and reconciled; the instance goes away, the ConfigMap stays, the result is empty. Add a spec that a live CLI-owned instance keeps a leftover finalizer. Verify: on the unchanged tree both specs fail where they wait for the deleted instance to go away
- [x] 1.2 Release the finalizer in `handleCLIOwned` (`internal/reconcile/moduleinstance.go`) for a deleting instance, without pruning or a status write, and correct the comments of `handleCLIOwned` and `handleNotReconciled`. Verify: both specs pass
- [x] 1.3 `task dev:fmt dev:vet dev:lint dev:test docs:bundle:check` green, then commit `fix(controller): release the cleanup finalizer of a deleting cli-owned instance`

## 2. Reconcile a deleting instance when the orphan annotation is set

- [x] 2.1 Add `test/integration/reconcile/deletion_wake_test.go` with a manager-driven spec: an instance stalled with `DeletionSAMissing` gets the orphan annotation and is gone within 10 seconds. Add a table test of the predicate in `internal/controller`. Verify: on the tree of section 1 the manager spec fails (the instance stays)
- [x] 2.2 Add `orphanAnnotationSet` and OR it with the generation predicate on `For()` in `internal/controller/moduleinstance_controller.go`; update the watch comment. Verify: the manager spec and the table test pass
- [x] 2.3 `task dev:fmt dev:vet dev:lint dev:test docs:bundle:check` green, then commit `fix(controller): reconcile a deleting instance when the orphan annotation is set`

## 3. Reconcile an instance when its ServiceAccount returns

- [ ] 3.1 Add a manager-driven spec to `test/integration/reconcile/deletion_wake_test.go`: an instance stalled with `DeletionSAMissing` is gone, and its inventory pruned, within 10 seconds of the ServiceAccount's creation. Add specs for the map function in `internal/controller` (named, flag default, other name, other namespace, CLI-owned, suspended live, suspended deleting). Verify: on the tree of section 2 the manager spec fails
- [ ] 3.2 Export the effective-ServiceAccount precedence from `internal/reconcile`; add `mapServiceAccountToModuleInstances`, `serviceAccountCreated` and the metadata-only watch; change the RBAC marker to `get;impersonate;list;watch`; run `task dev:manifests` and `task operator:installer`. Verify: `git diff` of `config/rbac/role.yaml`, `dist/install.yaml` and `modules/opm_operator/zz_generated_rbac.cue` shows only `list` and `watch` added to the `serviceaccounts` rule, and the specs of 3.1 pass
- [ ] 3.3 Add a spec to `test/integration/reconcile/impersonation_rbac_test.go` that the shipped role's verbs on `serviceaccounts` are exactly `get`, `impersonate`, `list`, `watch`. Verify: it passes, and `task operator-module:drift` is green
- [ ] 3.4 `task dev:fmt dev:vet dev:lint dev:test docs:bundle:check` green, then commit `fix(controller): reconcile an instance when its ServiceAccount returns`

## 4. Documentation

- [ ] 4.1 Update the outlines in `docs/site/operating/deletion-and-pruning.md` and `docs/site/operating/delete-an-instance-safely.md` (the CLI-owned delete after an operator-owned past; the recovery actions take effect at once on a ModuleInstance, at the stalled recheck on a ModulePackage) and the row in `docs/site/diagnostics/operator-conditions.md` if it names the wait. Verify: `task docs:bundle:check` green
- [ ] 4.2 `task dev:fmt dev:vet dev:lint dev:test docs:bundle:check` green, then commit `docs: record the released cli-owned finalizer and the prompt deletion recovery`
