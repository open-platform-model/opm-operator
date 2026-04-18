## 1. Stale-set component-rename fix

- [x] 1.1 Add regression test `TestComputeStaleSet_ComponentRenameSafe` in `internal/inventory/stale_test.go`: previous `[{apps Deployment ns app v1 Component:web}]`, current `[{apps Deployment ns app v1 Component:frontend}]`, assert stale set empty. Run `go test ./internal/inventory -run TestComputeStaleSet_ComponentRenameSafe -v` and confirm it FAILS under current code.
- [x] 1.2 Change `internal/inventory/stale.go:17` from `IdentityEqual(prev, cur)` to `K8sIdentityEqual(prev, cur)`. Update the doc comment on `ComputeStaleSet` (lines 5-7) to state Component is excluded for rename safety; reference `K8sIdentityEqual` instead of `IdentityEqual`.
- [x] 1.3 Update doc comment on `IdentityEqual` at `internal/inventory/entry.go:26-28` to state the helper is component-aware and is **not** the comparator used by `ComputeStaleSet`; direct callers needing K8s resource identity to `K8sIdentityEqual`.
- [x] 1.4 Re-run `TestComputeStaleSet_ComponentRenameSafe` and confirm it passes. Run full `go test ./internal/inventory/...` and confirm no regressions.

## 2. Status.ReleaseUUID API field

- [x] 2.1 Add `ReleaseUUID string` to `ModuleReleaseStatus` in `api/v1alpha1/modulerelease_types.go` (additive optional field with `+optional` marker and `json:"releaseUUID,omitempty"` tag). Place near other identity-tracking fields for readability.
- [x] 2.2 Run `task dev:manifests dev:generate` to regenerate CRD YAML and DeepCopy methods. Verify diff is restricted to the new field in `config/crd/bases/releases.opmodel.dev_modulereleases.yaml` and in `api/v1alpha1/zz_generated.deepcopy.go`.
- [x] 2.3 Add a printcolumn marker (optional) — only if the team wants `kubectl get mr` to surface the UUID by default. Default: skip; the field is visible via `-o yaml`.

## 3. Render→Status persistence of release UUID

- [x] 3.1 In `internal/reconcile/modulerelease.go` (or a new helper in `internal/reconcile/`), extract the release UUID from the rendered resources' `module-release.opmodel.dev/uuid` label after successful render. Use `core.LabelModuleReleaseUUID` from `pkg/core/labels.go:38`. All rendered resources carry the same UUID; reading the first one is sufficient.
- [x] 3.2 Assign the extracted UUID to `mr.Status.ReleaseUUID` in the same place where other Status fields are populated post-render (around the apply phase, before the deferred patcher commits). Persisted automatically via the existing patcher.
- [x] 3.3 Add a unit test in `internal/reconcile/` (or extend an existing reconcile test) that asserts `Status.ReleaseUUID` is populated after a successful render and persisted by the patcher.

## 4. Prune UUID-based ownership guard

- [x] 4.1 Change `apply.Prune` signature in `internal/apply/prune.go` from `Prune(ctx, c, stale) (*PruneResult, error)` to `Prune(ctx, c, ownerUUID string, stale) (*PruneResult, error)`. Update both call sites in `internal/reconcile/modulerelease.go`: the apply→prune happy path (line 374, via `pruneStaleResources`) supplies the freshly-rendered UUID (or `mr.Status.ReleaseUUID`); the deletion path (line 515) supplies `mr.Status.ReleaseUUID`.
- [x] 4.2 Add integration regression test `Prune skips resource missing OPM managed-by label` in `test/integration/apply/prune_test.go`: pre-create a ConfigMap without managed-by label, include it in the stale set, assert it is skipped (not deleted) and `PruneResult.Skipped` is incremented.
- [x] 4.3 Add integration regression test `Prune skips resource whose release UUID disagrees with ownerUUID` in `test/integration/apply/prune_test.go`: pre-create ConfigMap labeled `managed-by=opm-controller, module-release.opmodel.dev/uuid=<UUID-A>`, call Prune with `ownerUUID=<UUID-B>`, assert skipped.
- [x] 4.4 Add integration regression test `Prune deletes resource whose release UUID matches ownerUUID`: pre-create ConfigMap with both labels matching, call Prune with matching `ownerUUID`, assert deleted.
- [x] 4.5 Add integration regression test `Prune tolerates legacy resource with empty UUID label`: pre-create ConfigMap with `managed-by=open-platform-model` (legacy value) and no UUID label, assert deleted (legacy fallback).
- [x] 4.6 Update existing prune tests in `test/integration/apply/prune_test.go` to set `app.kubernetes.io/managed-by=opm-controller` and `module-release.opmodel.dev/uuid=<UUID>` on pre-created resources, and pass the matching UUID to Prune calls, so the happy path still passes after the guard is added.
- [x] 4.7 Run the new tests against current `prune.go` (with the signature change but without the guard logic) with `KUBEBUILDER_ASSETS=$(./bin/setup-envtest use 1.35.0 --bin-dir ./bin -p path) go test ./test/integration/apply -run TestApply -ginkgo.focus="Prune skips" -v -ginkgo.v` and confirm they FAIL (current code deletes everything).
- [x] 4.8 Implement the guard in `internal/apply/prune.go`: between the safety-exclusion check (line 54) and the `c.Delete` (line 70), add a `c.Get` call. Skip + log + count `Skipped` if (a) Get returns non-NotFound error → still bubble via errs, (b) `core.IsOPMManagedBy(live.GetLabels()[core.LabelManagedBy])` returns false, (c) `live.GetLabels()[core.LabelModuleReleaseUUID]` is non-empty AND differs from `ownerUUID`. Preserve existing NotFound and fail-slow semantics.
- [x] 4.9 Re-run the new tests and confirm they pass. Run full `KUBEBUILDER_ASSETS=... go test ./test/integration/apply/...` and confirm no regressions.

## 5. Impersonation Groups fix

- [x] 5.1 Refactor `internal/apply/impersonate.go`: extract an unexported helper `buildImpersonationConfig(namespace, saName string) rest.ImpersonationConfig` returning `{UserName: "system:serviceaccount:<ns>:<sa>", Groups: ["system:serviceaccounts", "system:serviceaccounts:<ns>", "system:authenticated"]}`. Have `NewImpersonatedClient` call this helper.
- [x] 5.2 Add unit test `TestBuildImpersonationConfig_SetsExpectedGroups` in `internal/apply/impersonate_test.go`: assert `UserName == "system:serviceaccount:team-a:deploy-sa"` AND `slices.Equal(cfg.Groups, []string{"system:serviceaccounts", "system:serviceaccounts:team-a", "system:authenticated"})`.
- [x] 5.3 Add integration regression test `Reconcile with group-subject RoleBinding` in `test/integration/reconcile/impersonation_test.go`: create SA with no direct-subject bindings; create RoleBinding with `Subjects: [{Kind: "Group", Name: "system:serviceaccounts:<ns>"}]` granting permissions on the resources to apply; assert reconcile produces `Ready=True`. Run against pre-fix code and confirm it FAILS (Forbidden).
- [x] 5.4 Run all impersonation tests after the fix and confirm they pass: `go test ./internal/apply -run TestBuildImpersonationConfig -v` and `KUBEBUILDER_ASSETS=... go test ./test/integration/reconcile -run TestReconcile -ginkgo.focus="group-subject" -v -ginkgo.v`.

## 6. Finalizer requeue fix

- [x] 6.1 Add integration regression test in a new `test/integration/reconcile/finalizer_test.go` (or extend `reconcile_test.go`): create a ModuleRelease via the envtest API server. Do NOT call `Reconcile` directly. Use `Eventually(... 10*time.Second)` to assert (a) the finalizer `releases.opmodel.dev/cleanup` appears on `metadata.finalizers` AND (b) `mr.Status.Conditions` contains at least one of `Ready`, `Reconciling`, or `Stalled`. Run against current code and confirm it FAILS (no conditions appear within 10s due to predicate filtering).
- [x] 6.2 Change `internal/reconcile/modulerelease.go:80` from `return ctrl.Result{}, nil` to `return ctrl.Result{Requeue: true}, nil`. (Landed in prior commit `54f158c fix(reconcile): requeue after finalizer add`; verified retained under this change.)
- [x] 6.3 Rewrite the comment on `internal/reconcile/modulerelease.go:73-74` to explain: the watch event produced by the finalizer patch is filtered by `predicate.GenerationChangedPredicate` because finalizer-only changes do not bump `metadata.generation`; we requeue directly to bypass the predicate via the workqueue. (Comment already in place; verified retained.)
- [x] 6.4 Re-run the new integration test and confirm it passes within the 10-second window. Run full `KUBEBUILDER_ASSETS=... go test ./test/integration/reconcile/...` and confirm no regressions.

## 7. Test-quality documentation

- [x] 7.1 Add a one-paragraph rule to `docs/TESTING.md` (under the unit/integration/e2e tier guidance): "Behaviors that depend on controller-runtime wiring — predicates, watch events, owner-ref-driven enqueue, finalizer-add semantics — MUST be exercised through an envtest manager (created via `manager.New` and started in the suite's `BeforeSuite`), not by calling `Reconcile` directly. Direct `Reconcile` calls test the function, not the controller; they cannot detect predicate-drop, watch-filtering, or workqueue-routing bugs. The finalizer-requeue regression in `fix-reconcile-correctness-trio` is the canonical example: every existing reconcile test passed because they called `Reconcile` twice in succession, bypassing the predicate that filtered the bug into existence."

## 8. Local bug-introduction proof

- [x] 8.1 Reverted `stale.go` comparator to `IdentityEqual`; `TestComputeStaleSet_ComponentRenameSafe` FAILED with stale entry `[{apps Deployment ns app v1 web}]`. Restored; test passes.
- [x] 8.2 Removed managed-by + UUID checks from `prune.go` (kept Get + signature); the two "skip" regression tests FAILED (`Prune ... missing OPM managed-by label`, `Prune ... UUID disagrees`) with `Skipped=0, Deleted=1`. The matching-UUID and legacy tests still PASS (they assert Delete fires — expected without guard). Restored; all four tests pass.
- [x] 8.3 Set `buildImpersonationConfig.Groups = nil`; `TestBuildImpersonationConfig_SetsExpectedGroups` FAILED (`Groups = []`). The `group-subject RoleBinding` integration test did NOT fail — Kubernetes apiserver auto-derives SA groups when `Impersonate-User` is in `system:serviceaccount:<ns>:<name>` format (`k8s.io/apiserver/.../impersonation/impersonation.go:85-91`), so the functional bug the proposal describes does not reproduce on modern apiservers. Fix is still correct (matches Flux, makes intent explicit, works with non-SA UserNames); integration test captures spec-compliance rather than a regression. Restored; unit test passes.
- [x] 8.4 Changed `modulerelease.go:84` back to `ctrl.Result{}, nil`; `Finalizer-add requeue` integration test FAILED with 10s timeout — conditions stayed empty because `GenerationChangedPredicate` filters the finalizer-only UPDATE. Restored; test passes.
- [x] 8.5 None of the scratch reverts committed (working tree verified clean after each restore). §8.3 finding documented above for PR body.

## 9. Validation gates

- [x] 9.1 Run `task dev:manifests dev:generate` — verify diff scoped to the new Status field added in 2.1.
- [x] 9.2 Run `task dev:fmt dev:vet` — must pass.
- [x] 9.3 Run `task dev:lint` — must pass; new helper names should not trip `revive` / `unused`.
- [x] 9.4 Run `task dev:test` — full unit + integration suite must pass.
- [x] 9.5 Run `openspec validate fix-reconcile-correctness-trio --strict` — must pass.
- [ ] 9.6 Optional sanity (time-permitting): `task kind:setup && task dev:e2e` to confirm no regression in the deployed-controller path.
