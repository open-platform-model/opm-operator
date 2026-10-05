# Tasks: add-healthy-condition

Sections per design.md, Sections. Run every test with a private absolute `TMPDIR` (`mktemp -d` under the session scratchpad) and `OPM_TEST_REGISTRY_FORCE=1`, so a registry-backed spec that would skip fails instead. Never run `task test` or `task check`: they chain the Kind suites. A Kind suite runs only under `flock` on the session's `kind-opm-dev.lock`, with an explicit `--context kind-opm-dev` (and an explicit `--kubeconfig`), and never changes the host kubeconfig's current context. Line numbers were taken at `c303a46`. Re-check them.

## 1. Vocabulary and API

- [x] 1.1 `internal/status/conditions.go`: add `HealthyCondition = "Healthy"` beside the other condition types, with a doc comment that says it is independent of `Ready` and why (design.md D1). Add the reasons `RolledOutReason`, `NotRolledOutReason`, `ProgressDeadlineExceededReason` and `HealthUnknownReason`, each with a one-line comment naming its status. Add `MarkHealthy(obj conditions.Setter, s metav1.ConditionStatus, reason, messageFormat string, messageArgs ...any)`, which sets `Healthy` only.
- [x] 1.2 `MarkManagedExternally` and `MarkSelfManagementRefused` also delete `HealthyCondition` (design.md D7). `MarkSuspended`, `MarkReady`, `MarkReconciling`, `MarkStalled` and `MarkNotReady` do not touch it. Unit tests in `internal/status`: the constants, `MarkHealthy` leaving every other condition alone, the four `Mark*` helpers above leaving `Healthy` alone, and the two removals.
- [x] 1.3 Add `status.HealthyCondition` to every `patch.WithOwnedConditions` list (design.md D8): `internal/reconcile/moduleinstance.go:128`, `:186`, `:366`, `:841`, `:901`, `:931`, `:1118`, and `modulepackage.go:828` and `:979`. Verify with `grep -n -A8 'WithOwnedConditions' internal/reconcile/*.go`.
- [x] 1.4 `api/v1alpha1/moduleinstance_types.go:199` and `modulepackage_types.go:163`: add the `Healthy` printer column right after `Ready` (design.md D9). In the `Conditions` doc comment of both status types, add a sentence that `Healthy` reports whether the applied objects have rolled out and `Ready` whether the render was applied. It becomes the CRD description.
- [x] 1.5 Envtest specs in `internal/controller`: a CLI-owned instance carrying `Healthy=True` loses it on acknowledgement, and the re-acknowledgement patch stays empty. A refused own instance carrying `Healthy=True` loses it, and re-refusal stays a no-op.
- [x] 1.6 `task dev:manifests dev:generate` (regenerates `config/crd/bases`, and `modules/opm_operator/zz_generated_crds.cue` through `operator-module:generate`), then `task operator:installer` for `dist/install.yaml`, then `task operator-module:drift` and `task docs:bundle:check`.
- [x] 1.7 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `feat(api): add the Healthy condition and printer column`.

## 2. Judge health and wire it into the ModuleInstance reconciler

- [x] 2.1 `internal/reconcile/health.go` (new): `healthVerdict` (status, reason, message, requeue class), `judgeHealth(ctx, reader, entries)` and `readEntries` per design.md D3/D4. Use one 30-second deadline and at most 8 reads in flight, keep results in inventory order, count `NotFound` and a no-match error (`meta.IsNoMatchError`) as missing, and count every other error, the deadline included, as unreadable. Judge with `health.Evaluate`, `health.IsHealthy`, `health.Aggregate` and `health.ProgressDeadlineExceeded` only (no readiness rule of the operator's own). The message names up to five objects, then "and N more"; for `ProgressDeadlineExceeded` the stalled Deployments come first. A read failure names the first unreadable object's error in inventory order. Add `healthRequeue(v, lastAppliedAt, now)` per D5 and `applyHealth(obj, v)`, which calls `status.MarkHealthy`.
- [x] 2.2 `internal/reconcile/health_test.go`, with a fake client and hand-built unstructured objects:
  - each row of the D4 table, plus the precedence when rows 1 and 2, and rows 2 and 3, both match;
  - message stability: two judgements of the same objects give equal messages, and a sixth not-ready object reads "and 1 more";
  - a stalled Deployment sixth in inventory order behind five not-ready objects is named first under `ProgressDeadlineExceeded`;
  - two unreadable objects whose reads finish in reverse order: the message names the error of the first in inventory order;
  - a no-match read error counts as `Missing`;
  - `health.go` calls no `unstructured.Nested*` accessor and holds no `"status"` literal (go/ast parse; `kubernetes-tier-adoption`);
  - `healthRequeue` at 0s, 4s, 10s, 1m, 10m and a future or nil `lastAppliedAt`, and for each reason;
  - parallelism: a reader that blocks counts never more than 8 reads in flight;
  - the deadline: a reader that never returns gives `HealthUnknown` within the deadline (use a short test override of the deadline).
- [x] 2.3 `internal/reconcile/moduleinstance.go`, the success path (`:636`): after `MarkReady`, judge with the reader of design.md D2 over `newEntries` (the impersonated `applyClient` when `effectiveSA != ""`, else `params.APIReader`), apply the verdict, and return `ctrl.Result{RequeueAfter: healthRequeue(...)}`. `lastAppliedAt` is the time the deferred commit is about to write, so use `time.Now()` for the elapsed time. Leave `retryAfter` and `nextRetryAt` alone.
- [x] 2.4 The NoOp path (`:530`): build the reader as `buildApplyClient` does (on failure: `HealthUnknown` with the error, health backoff), judge over `mi.Status.Inventory`, apply the verdict, and return its requeue. The verdict is set on `mi` before the deferred `commitNoOpStatus` patches it.
- [x] 2.5 The skip path (`:260`): when `instanceRenderSkippable` holds, build the reader, judge over `mi.Status.Inventory`, apply the verdict, and patch through `patcher` with `WithOwnedConditions{HealthyCondition}` only. Then return the health requeue (design.md D6). Do not arm the deferred commit, write `observedGeneration`, emit an event or record metrics. Log a patch failure and return the requeue anyway.
- [x] 2.6 Envtest specs, extending `internal/controller/moduleinstance_reconcile_test.go` and `render_skip_test.go`. Envtest runs no workload controller, so the specs set a Deployment's status by hand with a status update.
  - A ConfigMap-only instance ends `Ready=True`, `Healthy=True/RolledOut`, with no requeue.
  - A Deployment instance ends `Ready=True` and `Healthy=False/NotRolledOut`, `RequeueAfter` is 5s, and `nextRetryAt` is unset.
  - After the Deployment status is set to rolled out, the next reconcile is a skip: it patches only `Healthy` (now `RolledOut`, with `observedGeneration`, `lastAttemptedAt`, history and `renderedAt` unchanged) and returns no requeue.
  - A Deployment with `Progressing/ProgressDeadlineExceeded` at its observed generation gives `Healthy=False/ProgressDeadlineExceeded` and a requeue of `StalledRecheckInterval`.
  - A NoOp after a Deployment's available replicas drop gives `NotRolledOut`.
  - A failed render leaves `Healthy` as it was.
  - A deleted inventory object yields `Healthy=False/NotRolledOut` naming it `Missing`, and it stays so across a following `NoOp` (design.md Risks).
  - In `test/integration/reconcile/impersonation_test.go`, an impersonated instance reads through its ServiceAccount: a ServiceAccount whose `get` on Deployments is revoked after the apply gives `HealthUnknown`, while one with `get` gives the real verdict.
  - Existing specs that expect a skip to patch nothing seed `Healthy=True/RolledOut`. Existing specs that assert `ctrl.Result{}` after a success keep passing, because their stubs render passive kinds; fix any that do not, and say which in the commit body.
- [x] 2.7 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `feat(controller): report whether a ModuleInstance has rolled out in a Healthy condition`.

## 3. Wire it into the ModulePackage reconciler

- [ ] 3.1 `internal/reconcile/modulepackage.go`: let `applyAndPruneModulePackage` also return the apply client and whether it is impersonated, so the success path (`:357`) judges over `newEntries` with the reader of design.md D2. Return `RequeueAfter: shorterRequeue(healthRequeue(...), interval)`, where a zero health requeue means `interval`.
- [ ] 3.2 The NoOp path (`:340`): build the reader with `buildModulePackageApplyClient` (or `params.APIReader`), judge over `pkg.Status.Inventory`, apply the verdict before the deferred commit's NoOp branch patches, and return the shorter requeue.
- [ ] 3.3 The skip path (`:307`): restore `pkg.Status.Conditions` from `conditionsAtStart`, judge, apply the verdict, and patch with `WithOwnedConditions{HealthyCondition}` only (design.md D6). Keep `renderSkipped` so the deferred commit still sends nothing else, and return the shorter requeue.
- [ ] 3.4 Envtest specs in `internal/controller/modulepackage_controller_test.go` and `render_skip_test.go`. They mirror 2.6 for a package: `RolledOut` keeps `spec.interval`, `NotRolledOut` requeues after the shorter interval, and a skip that changes `Healthy` writes only `Healthy` and never `Reconciling`. A `dependsOn` spec: a package that depends on a `Ready=True, Healthy=False` package proceeds.
- [ ] 3.5 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `feat(controller): report whether a ModulePackage has rolled out in a Healthy condition`.

## 4. Docs and end-to-end check

- [ ] 4.1 `docs/site/diagnostics/operator-conditions.md`: add four rows in the page's comment-outline style, after the `Drifted` row, for `Healthy`: `RolledOut`, `NotRolledOut`, `ProgressDeadlineExceeded` and `HealthUnknown`. Each row says when the condition is judged, which identity reads, the requeue, that `Ready` and `dependsOn` do not read it, and its "Check against" files (`internal/reconcile/health.go`, `internal/status/conditions.go`). In the opening comment, add that `Healthy` is independent of `Ready`. Update the `Ready: ManagedExternally` and `Ready: SelfManagementRefused` rows to list `Healthy` among the removed conditions, and add to the `Ready: Suspended` row that `Healthy` is kept.
- [ ] 4.2 `docs/RENDERING.md`, under the render-skip rules: a skip reads the inventory's objects to judge `Healthy` and may patch that one condition. With `--drift-render-interval=0`, or while `status.lastAppliedInputs` is unset because the key is incomplete, every health requeue renders (design.md Risks).
- [ ] 4.3 `test/e2e/podinfo_test.go`: after the existing readiness checks, assert that the podinfo ModuleInstance eventually reports `Healthy=True` with reason `RolledOut`, and that `kubectl get mi` prints a `HEALTHY` column.
- [ ] 4.4 Run `task dev:e2e` under `flock` on the session's `kind-opm-dev.lock`, with `LOCAL_REGISTRY` set as `.github/workflows/test-e2e.yml` sets it and an explicit kubeconfig. Do not publish to the shared local registry. Record the result and any skipped spec, with the reason, in this task.
- [ ] 4.5 `task docs:bundle:check` and `openspec validate add-healthy-condition --strict` pass. Run `openspec validate --specs --strict` and name any failure that already fails on `origin/main`.
- [ ] 4.6 Before archiving (not in this stage), re-copy every MODIFIED requirement in this change (`render-input-key`, `reconcile-loop-assembly`, `modulepackage-reconcile-loop`, `module-instance-ownership`) from the then-current `origin/main` spec text and re-apply this change's edits, so that no concurrent edit to those requirements, a citation sweep included, is reverted by the archive.
- [ ] 4.7 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `docs: document the Healthy condition and its requeue`.
