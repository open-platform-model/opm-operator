## Why

Three correctness bugs were identified in the ModuleRelease reconcile path during a code review. All three pass existing tests because the tests assert at the wrong level or bypass the production controller wiring. Two are silent failure modes for end users — one risks data loss on routine refactors, one breaks group-scoped RBAC bindings — and the third leaves freshly-created ModuleReleases without status conditions for up to 10 hours. We are fixing them as one change because they share a single theme (reconcile correctness) and a single test-quality lesson (tests must exercise the path the production controller actually takes).

## What Changes

- **Stale-set component-rename data loss (`internal/inventory/stale.go`)**. `ComputeStaleSet` currently calls `IdentityEqual` (which includes `Component` in the identity tuple). When a CUE refactor moves a resource (same GVK+namespace+name) under a different component label, the previous inventory entry is added to the stale set, SSA apply patches the live object in place with the new label, then `apply.Prune` deletes the just-applied object — destroying state for PVC/Secret/StatefulSet and silently disrupting Deployment/Service. Switch to `K8sIdentityEqual` so component renames do not produce stale entries.
- **Prune ownership guard via `module-release.opmodel.dev/uuid` (`internal/apply/prune.go`)**. The catalog already stamps `module-release.opmodel.dev/uuid` on every rendered resource (a SHA1 over module-uuid + MR-name + MR-namespace, globally unique per ModuleRelease). The controller currently ignores this label entirely. Add a defense-in-depth guard that fetches the live object first and skips deletion if the live object's `module-release.opmodel.dev/uuid` label does not match the reconciling MR's release UUID, OR if the live object lacks an OPM `app.kubernetes.io/managed-by` value (per `core.IsOPMManagedBy`). This is strictly stronger than a component-label guard: it discriminates ownership at the MR identity level, prevents future stale-set computation bugs from causing destruction, and protects against cross-MR collisions.
- **Persist release UUID on Status (`api/v1alpha1/modulerelease_types.go`)**. Add `ModuleReleaseStatus.ReleaseUUID string`. Populated on the first successful render from any rendered resource's `module-release.opmodel.dev/uuid` label. Read from Status during the deletion path (where render may have already been pruned from memory). On the apply/prune happy path, the value is read directly off the rendered resources for that reconcile (no API-server roundtrip beyond what render already does).
- **Impersonation missing Groups (`internal/apply/impersonate.go`)**. `NewImpersonatedClient` currently sets only `UserName` on the `rest.ImpersonationConfig`. The apiserver does not derive groups from `Impersonate-User`; it reads `Impersonate-Group` headers independently. Without groups, RBAC bindings whose subjects target `system:serviceaccounts`, `system:serviceaccounts:<namespace>`, or `system:authenticated` silently fail. Set the standard SA group set on the impersonation config, matching what the SA token authenticator and Flux's kustomize-controller produce.
- **Finalizer-add requeue (`internal/reconcile/modulerelease.go`)**. The finalizer-add branch returns `ctrl.Result{}, nil` and relies on the resulting watch event to drive the next reconcile. The controller registers `predicate.GenerationChangedPredicate{}`; finalizer patches do not bump `metadata.generation`, so the predicate filters that event. Return `ctrl.Result{Requeue: true}` to bypass the predicate via direct workqueue add, ensuring the next reconcile starts immediately rather than waiting for the 10h periodic resync.
- **Test hardening — apply each fix's regression test FIRST, then the source fix**. For each of the four behavior fixes, add a test that fails under the current code and passes under the fix, then implement the fix. Specifically: a `ComputeStaleSet` component-rename unit test; a `BuildImpersonationConfig` unit test asserting the exact Groups slice; envtest integration tests for prune UUID-mismatch ownership-guard, group-subject RBAC, and watch-driven finalizer-add reconcile. Add a one-paragraph rule to `docs/TESTING.md`: behavior depending on controller-runtime wiring (predicates, watch events, owner refs) must be exercised through an envtest manager, not through direct `Reconcile` calls.

This is a PATCH change with one Status field addition: no breaking API change for users following the documented happy path; only restoring intended semantics for documented edge cases plus exposing the release UUID Status that the controller now owns.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `inventory-bridge`: `Stale set computation` requirement changes from `IdentityEqual` to `K8sIdentityEqual`; new scenario for component rename safety.
- `prune-stale-resources`: New requirement for live-state UUID-based ownership guard before deletion. Includes scenarios for matched UUID (delete proceeds), mismatched UUID (skip), missing OPM managed-by label (skip), and tolerated empty UUID for legacy resources.
- `serviceaccount-impersonation`: New requirement that the impersonation config includes the standard SA group set.
- `finalizer-and-deletion`: `Finalizer registration` requirement gains a scenario stating the finalizer-add reconcile must enqueue an immediate follow-up reconcile (not depend on the watch-event-driven path).

## Impact

- **API**: One additive Status field on `ModuleRelease` (`Status.ReleaseUUID string`). Triggers `task dev:manifests dev:generate`. No CRD breaking change (additive optional field).
- **RBAC**: None at the controller level. End-users may now configure group-subject RoleBindings against the impersonated SA and have them work as expected (previously failed silently).
- **Specs**: Four `MODIFIED` deltas, no new capabilities.
- **Code**: Small, localized edits in `internal/inventory/stale.go`, `internal/apply/prune.go`, `internal/apply/impersonate.go`, `internal/reconcile/modulerelease.go`, `internal/reconcile/<status persistence helper>`, `api/v1alpha1/modulerelease_types.go`. New tests in `internal/inventory/`, `internal/apply/`, `test/integration/apply/`, `test/integration/reconcile/`.
- **Behavior change for existing users**: A `ModuleRelease` whose render moves a resource between components will now keep the resource in place (correct) rather than delete-and-recreate it (broken). A user who relied on the broken behavior to clear state during a component rename would see a behavior change — none expected, since the broken behavior is data-destructive. New `Status.ReleaseUUID` field becomes visible on `kubectl get mr -o yaml` after first successful render.
- **SemVer**: PATCH (bug fixes within v1alpha1; the Status field addition is additive optional and does not break clients).
- **Docs**: One-paragraph addition to `docs/TESTING.md` codifying the manager-driven-test rule.
- **Coordination with sibling change `catalog-runtime-managed-by`**: independent. The UUID label is already stamped today (via the catalog's `moduleLabels` merge); this change does not depend on the catalog refactor. Both can land in either order.
