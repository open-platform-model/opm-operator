## Context

Three independent bugs in the ModuleRelease reconcile path were surfaced by code review and confirmed against the live code. They live in different packages (`internal/inventory`, `internal/apply`, `internal/reconcile`) but share a common pathology: the existing tests do not exercise the production code path that triggers the bug.

- `inventory.ComputeStaleSet` uses `IdentityEqual` (which includes `Component`); `K8sIdentityEqual` exists, is correctly tested, but has zero non-test callers. Tests cover version migration but no test moves a resource between components.
- `apply.NewImpersonatedClient` returns a client whose `rest.ImpersonationConfig` has no `Groups` set. Unit tests assert only `impClient != nil` and that the original config is not mutated; the integration test in `test/integration/reconcile/impersonation_test.go` uses RoleBindings whose subjects are direct ServiceAccount references, not Groups, so it does not exercise group-based authorization.
- `internal/reconcile/modulerelease.go` returns `ctrl.Result{}, nil` after adding the finalizer, relying on the watch event to trigger the next reconcile. The controller registers `predicate.GenerationChangedPredicate{}` (`internal/controller/modulerelease_controller.go:81`); finalizer patches do not bump `metadata.generation` and are filtered. Every existing reconcile test calls `Reconcile` directly multiple times in succession, which bypasses the predicate entirely.

A fourth, related observation: the catalog already stamps `module-release.opmodel.dev/uuid` on every rendered resource (`catalog/core/v1alpha1/modulerelease/module_release.cue:31`, flowing through `transformer.cue:116-122` `moduleLabels` merge). This label is a globally-unique SHA1 over `(module-uuid, MR-name, MR-namespace)` and is the strongest available ownership signal. The controller currently ignores it — neither the stale-set computation nor `apply.Prune` reads it. The prune ownership guard introduced here is the right place to start consuming it.

The controller already defines all the building blocks needed for the fixes: `K8sIdentityEqual` at `internal/inventory/entry.go:39`, `core.IsOPMManagedBy` at `pkg/core/labels.go:46`, `core.LabelModuleReleaseUUID` at `pkg/core/labels.go:38`. No new abstractions are needed.

## Goals / Non-Goals

**Goals:**

- Restore documented intent on three reconcile-correctness paths (component rename safety, group-scoped impersonation, post-finalizer-add status latency).
- Add a UUID-based ownership guard in `apply.Prune` so future stale-set computation bugs cannot cause destruction, and so cross-MR ownership collisions are caught.
- Persist the release UUID on `ModuleReleaseStatus` so the deletion path (which may not have a fresh render in scope) can still enforce the guard.
- For each fix, add a regression test that fails under the broken code and passes under the fix. Verify by reverting each fix in isolation locally and observing the test fail.
- Codify the manager-driven test rule in `docs/TESTING.md` so this class of test gap (direct `Reconcile` call masks predicate-driven bugs) does not recur.

**Non-Goals:**

- Reworking `apply.Prune` to use Flux's `DeleteAll`. Per-resource `Delete` is a documented design decision (see `prune.go:26-43`); preserved.
- Adding ResourceVersion preconditions to deletes. Out of scope; the UUID guard is sufficient defense-in-depth for the failure modes we have evidence of.
- Replacing `IdentityEqual` with `K8sIdentityEqual` everywhere. `IdentityEqual` keeps its current contract (Component-aware) and may have future provenance use; only its callsite in `ComputeStaleSet` changes.
- Changing the controller's `GenerationChangedPredicate{}`. The predicate is correct for the spec-driven reconcile loop; the bug is the early return that depended on a watch event the predicate filters.
- Extending the stale-set comparator to also key on UUID. Out of scope (and explicitly deferred per user direction — `K8sIdentityEqual` is the right comparator for the K8s API's view of resource identity).
- Backporting fixes to the in-progress `release-cr` change. Independent CRD work, no overlap.
- The catalog's runtime-identity refactor (`managed-by` value source). Tracked in the sibling change `catalog-runtime-managed-by`; independent of this change.

## Decisions

### Decision 1: Switch `ComputeStaleSet` to `K8sIdentityEqual`, not patch `IdentityEqual`

**Choice**: Change `internal/inventory/stale.go:17` from `IdentityEqual(prev, cur)` to `K8sIdentityEqual(prev, cur)`. Update spec.

**Why**: `K8sIdentityEqual` already exists and is correctly tested (`entry_test.go:57`). It has zero non-test callers, which is itself evidence that the original author intended this comparator for stale-set use but wired the wrong one. The doc comment on `LabelComponentName` at `pkg/core/labels.go:29` explicitly says "Used by inventory to track provenance for component-rename safety checks" — current behavior is the opposite of that stated intent.

**Alternatives considered**:

- *Modify `IdentityEqual` to drop Component*: rejected. `IdentityEqual` is referenced by name in two existing tests and its semantics ("two entries identify the same owned resource, including by component") may have future use in provenance/history reporting where component-aware equality is correct. Better to preserve the helper and fix the callsite.
- *Delete `K8sIdentityEqual` and inline the comparison*: rejected. The named helper is more readable and keeps the inventory comparator API self-documenting.

### Decision 2: UUID-based ownership guard in `apply.Prune`

**Choice**: Before each `c.Delete`, `c.Get` the live object. Skip + log + count as `Skipped` if any of:

- Get returns NotFound → continue silently (already-deleted is success per existing semantics).
- Get returns any other error → bubble up via existing `errs` collection.
- Live object's `app.kubernetes.io/managed-by` label is not recognized by `core.IsOPMManagedBy` → skip (the live object is not OPM-managed at all).
- Live object's `module-release.opmodel.dev/uuid` label is non-empty AND differs from the reconciling MR's release UUID → skip (the live object belongs to a different ModuleRelease). An empty live UUID label is tolerated for backward compatibility with resources applied before the UUID label was introduced.
- Otherwise, proceed with `Delete`.

`apply.Prune` signature changes from `Prune(ctx, c, stale)` to `Prune(ctx, c, ownerUUID, stale)`. Callers (`internal/reconcile/modulerelease.go:374` and `:515`) supply the reconciling MR's UUID. The deletion path (line 515) reads from `mr.Status.ReleaseUUID` (Decision 3); the apply/prune happy path (line 374) reads from the freshly-rendered resources or from the same Status field.

**Why**: Per Constitution Principle III, "Pruning decisions rely on inventory, not labels" — but that principle is about *deciding what to prune*, not about *blindly executing the deletion without a sanity check*. The UUID guard is a final safety net, not the primary decision mechanism. Inventory still drives the stale set; the guard only refuses to delete objects that demonstrably belong to a different MR. Together with Decision 1, this makes the system safe even if a future bug reintroduces a stale-set computation defect, and it correctly handles the case where two MRs in the same namespace render resources with colliding GVK+ns+name.

UUID is strictly stronger than the component-label alternative considered earlier: a component label is local to a single MR's render (two MRs can both have a `web` component), whereas the release UUID is globally unique per MR.

**Alternatives considered**:

- *No guard, comparator-fix only*: rejected. Leaves the system one bug away from data loss again. The Get + label check is cheap (one round-trip per stale entry, in the cold-path where deletion is already happening).
- *Component-label guard*: rejected. Captures the rename bug class but not the cross-MR collision class. UUID subsumes it.
- *ResourceVersion precondition on the delete*: rejected for now. Helps against TOCTOU between Get and Delete but does not help against the rename bug class (RV would still match — the object was just successfully patched). Lower value for the threat model we have evidence of.
- *OwnerReference-based check*: K8s OwnerReferences are not used by the OPM model (resources may live in different namespaces from the MR; OwnerReference is constrained to same-namespace). Label-based identity is the right tool here.

### Decision 3: Persist release UUID on `ModuleReleaseStatus.ReleaseUUID`

**Choice**: Add `ReleaseUUID string` to `ModuleReleaseStatus` (additive optional field). Populated on first successful render by reading the `module-release.opmodel.dev/uuid` label off any rendered resource (all carry the same value). Persisted via the same deferred status patcher that already commits other Status fields. Read by `apply.Prune` callers — both the apply→prune happy path and the deletion path.

**Why**: the UUID is computed by CUE during render. The deletion path (`handleDeletion` in `modulerelease.go`) does not re-render — it operates only on `Status.Inventory.Entries`. Without persistence, the deletion path has no way to know the MR's UUID for the prune guard. Persisting on Status is the standard K8s pattern (Status as the operational ledger; Constitution Principle IV) and survives controller restarts.

Read order in apply→prune happy path: prefer the freshly-rendered value (via the rendered resources); fall back to `Status.ReleaseUUID` if render is unavailable (shouldn't happen in this path but defensive). Persisted on every successful render via the existing patcher.

**Alternatives considered**:

- *Recompute UUID in Go via `uuid.NewSHA1(OPMNamespace, fqn+":"+name+":"+namespace)`*: works but duplicates CUE logic in Go; drift risk if the catalog ever changes the UUID derivation formula. Reject.
- *Read off any rendered resource on each reconcile, never persist*: works for the apply path but breaks the deletion path. Reject.
- *Persist on a controller-managed annotation instead of Status*: Status is the right surface for controller-observed state; annotations are noisier and not subresource-isolated.

### Decision 4: Set the standard SA group set on the impersonation config

**Choice**: In `internal/apply/impersonate.go`, set:

```go
Groups: []string{
    "system:serviceaccounts",
    "system:serviceaccounts:" + namespace,
    "system:authenticated",
}
```

Extract a small unexported helper `buildImpersonationConfig(namespace, saName string) rest.ImpersonationConfig` so the unit test can assert the exact slice without inspecting the client transport. The constructor becomes a thin wrapper: validate SA exists, copy config, call helper, build client.

**Why**: This is exactly what the apiserver's `serviceaccount.TokenAuthenticator` injects when an SA authenticates with a token — `serviceaccount.MakeGroupNames(namespace)` plus `system:authenticated`. Matching that behavior gives impersonated identity parity with token-based identity for the same SA. Flux's `runtime/client/impersonation` package does the same thing for the same reason.

**Alternatives considered**:

- *Use `transport.ImpersonationConfig` and let it populate groups*: that type does not auto-populate groups; the apiserver does not derive them from username either. There is no built-in helper for this in client-go; we have to set it explicitly.
- *Make the group list configurable per ModuleRelease*: rejected. There is no use case for diverging from standard SA token semantics, and over-broad group lists could create privilege-escalation foot-guns.

### Decision 5: `Requeue: true` over fall-through reconcile

**Choice**: `internal/reconcile/modulerelease.go:80` returns `ctrl.Result{Requeue: true}, nil`. Update the surrounding comment to explain the predicate interaction.

**Why**: `Requeue: true` is `queue.AddRateLimited(req)` directly — it bypasses the event filter (predicates only filter informer-driven events, not direct workqueue adds). It works with the current code structure (the deferred patcher is initialized after the finalizer check; restructuring to fall through requires moving patcher init or splitting the function). Simpler diff, lower risk, identical user-visible outcome (~1 second to next reconcile under the rate limiter, vs. 10 hours).

**Alternatives considered**:

- *Fall through to full reconcile in same pass (kustomize-controller pattern)*: cleaner single-pass semantics but requires either reordering Phase 0 (initialize patcher before finalizer check) or in-memory `mr` refresh after `addFinalizer`. More structural change for no user-visible benefit.
- *Drop `GenerationChangedPredicate`*: rejected. The predicate is load-bearing for storm prevention (status-only patches do not retrigger reconcile). Removing it would regress the work done in commits `7c2814b feat(reconcile): add exponential backoff and storm prevention` and `a27e327 feat(reconcile): patch drift + counters on noop`.

### Decision 6: Test ordering — write the regression test first, then the fix

**Choice**: For each of the four behavior fixes, the implementation order is: (1) add the failing test that exercises the bug through the production path, (2) confirm it fails, (3) apply the source fix, (4) confirm the test passes. Run each new test in isolation against a scratch revert to prove it actually catches the bug class — not committed, just a local check before pushing.

**Why**: Existing tests pass under the buggy code. Without first proving a test fails under the bug, we cannot prove the test is load-bearing. This is also the only way to prevent the same class of test gap (assertion at the wrong level / direct `Reconcile` bypassing predicate) from recurring.

**Alternatives considered**:

- *Apply fix and tests together in one commit per bug*: works but loses the demonstration that the test would catch a regression. Test-first discipline pays for itself in confidence.

### Decision 7: Codify the manager-driven test rule in `docs/TESTING.md`

**Choice**: Add a one-paragraph rule: behaviors that depend on controller-runtime wiring (predicates, watch events, owner-ref-driven enqueue) must be exercised through an envtest manager (with `manager.Start`), not by calling `Reconcile` directly. Direct `Reconcile` calls test the function, not the controller; they cannot detect predicate-drop or watch-filtering bugs.

**Why**: The finalizer bug existed precisely because the test pattern ("call `Reconcile` twice in succession") looks reasonable in isolation but silently bypasses the predicate. Capturing this rule in `docs/TESTING.md` prevents the same gap from being re-introduced when the next finalizer-shaped bug lands.

## Risks / Trade-offs

- **Risk**: The prune ownership guard skips deletion of OPM-managed resources whose UUID label is missing (e.g., legacy resources applied before the UUID label was stamped). → **Mitigation**: explicit "tolerate empty UUID for legacy" branch in the guard logic + spec scenario. Operators see exactly why something was or wasn't deleted via structured logging.
- **Risk**: Race between two reconciles of the same MR — the rendered UUID and `Status.ReleaseUUID` could briefly disagree if the MR is recreated with the same name+namespace (UUID would be identical actually — since the formula is deterministic over (module-uuid, name, namespace), recreating the same MR yields the same UUID). → **Mitigation**: this is by design. The UUID is content-addressable; recreating an identical MR yields an identical UUID and the guard correctly identifies the resources as belonging to it.
- **Risk**: An existing user has a `ModuleRelease` mid-component-rename right now, expecting the destructive behavior to recreate state. → **Mitigation**: No evidence such users exist. The current behavior destroys data; nobody designs around it intentionally. The post-fix behavior preserves the in-cluster object, which is strictly safer. Behavior change documented in proposal Impact section.
- **Risk**: Group-list change to impersonation grants new permissions to users whose RBAC happened to deny the impersonated SA but allow `system:authenticated`. → **Mitigation**: This is the intended behavior of group-subject RBAC bindings, identical to what the SA would receive via token auth. Users who want impersonation to grant fewer permissions than the SA's token would have are misusing the impersonation feature; documenting parity in the spec is correct.
- **Risk**: `Requeue: true` after finalizer-add adds one extra reconcile per ModuleRelease creation. → **Mitigation**: This is the entire point — the missing reconcile is the bug. Throughput cost is one workqueue cycle per MR creation, paid once at create time, vs. 10 hours of stuck status. Overwhelmingly favorable trade.
- **Risk**: New `Status.ReleaseUUID` field is consumed by the prune guard before it is populated (first reconcile of a freshly-created MR). → **Mitigation**: prune is a Phase 6 operation that runs after a successful render in the same reconcile; the rendered UUID is available directly and persisted to Status before prune is invoked. The deletion path runs against an already-reconciled MR (Status populated). The only edge case — deletion of an MR that never successfully reconciled — has an empty Status.Inventory anyway, so prune is a no-op.
- **Risk**: New integration tests (group-subject RBAC, finalizer-watch, prune UUID-mismatch) require envtest infrastructure that is already wired up; no new dependencies. Confirmed in `test/integration/{apply,reconcile}/suite_test.go`.

## Migration Plan

No data migration. CRD additive change (one Status field). Behavior changes are bug fixes; the only user-visible deltas are:

1. Component renames in CUE no longer destroy the renamed resource.
2. Cross-MR collision deletes are caught by the UUID guard and logged as Skipped.
3. Group-subject RBAC bindings against the impersonated SA now work.
4. Freshly-created ModuleReleases get conditions within seconds, not hours.
5. `kubectl get mr -o yaml` shows a new `status.releaseUUID` field after first successful render.

All five are restorations of intended/documented behavior plus the deliberate Status surface addition. No rollback procedure needed beyond standard `git revert` of the change commits and re-running `task dev:manifests dev:generate` to regenerate CRD YAML.

## Open Questions

None at design time. Implementation may surface minor questions (exact wording for log messages on prune-skip; whether to extract `buildImpersonationConfig` as exported or unexported); these are local decisions for the apply phase.
