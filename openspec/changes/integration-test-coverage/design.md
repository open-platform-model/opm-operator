## Context

The reconcile loop (`internal/reconcile/modulerelease.go`) has 7 phases plus a deletion path. Existing test coverage is strong for:

- **Unit tests**: Individual packages (source, render, apply, inventory, status) have thorough coverage of their internal logic, edge cases, and error paths.
- **Integration tests** (`test/integration/reconcile/`): Error paths — render failure, fetch failure (transient + stalled), source not found, status-on-failure, partial failure preserving inventory.
- **Controller tests** (`internal/controller/`): Full pipeline happy path, suspend, source not ready, no-op detection, finalizer lifecycle, deletion (prune/orphan/safety/suspend/partial-failure), source watch.

The gaps are all **reconcile-level behaviors that span multiple phases or multiple reconcile cycles**. These are the flows users actually exercise day-to-day — changing values, watching resources get pruned, recovering from transient failures — but they lack integration or e2e coverage.

## Goals / Non-Goals

**Goals:**

- Catalog every missing integration and e2e test with clear rationale.
- Create stub files grouped by theme so they're ready for implementation.
- Each stub compiles and runs (as skipped tests), keeping `task dev:test` green.

**Non-Goals:**

- Implementing the test bodies (that's the apply phase).
- Changing production code.
- Adding unit tests for individual packages (already well-covered).
- Modifying existing test files.

## Decisions

### File organization: one file per test theme

Each new file targets a cohesive set of related behaviors. This keeps files focused and `-ginkgo.focus`-friendly.

**Integration files** (in `test/integration/reconcile/`):

| File | Theme | Tests |
|------|-------|-------|
| `change_propagation_test.go` | Re-reconcile on input changes | 2 |
| `stale_pruning_test.go` | Phase 6 prune during normal reconcile | 3 |
| `state_recovery_test.go` | Condition transitions across reconcile cycles | 3 |
| `status_tracking_test.go` | Multi-cycle status fidelity | 4–5 |

**E2E files** (in `test/e2e/`):

| File | Theme | Tests |
|------|-------|-------|
| `lifecycle_test.go` | Full happy-path lifecycle with real controller | 2 |
| `concurrent_test.go` | Multi-resource parallelism and resilience | 2 |

### Why these specific tests are missing and needed

Each test below targets a code path that has **no reconcile-level coverage** despite being exercised in normal operation.

---

## Integration Test Catalog

### Group 1 — Change Propagation (`change_propagation_test.go`)

#### 1.1 Values change triggers re-apply

**Code path**: Phase 4 no-op check (`modulerelease.go:248`) — `IsNoOp` returns false when config digest differs → full re-render → re-apply.

**Why missing**: The existing no-op test (`modulerelease_reconcile_test.go`) only proves that *identical* inputs skip apply. No test proves the inverse: a values-only change (no source change) propagates through render and produces updated resources in the cluster.

**Why needed**: This is the most common user action — editing `spec.values` to change configuration. If the config digest comparison or re-render path has a regression, the controller silently does nothing. This is the single highest-value missing test.

**Validates**: `spec.values` change → new config digest → Phase 3 re-render → Phase 5 re-apply → updated resource in cluster → new inventory revision → history records second success.

#### 1.2 Source revision change triggers re-apply

**Code path**: Phase 1 source resolve (`modulerelease.go:191`) — new artifact digest → new source digest → `IsNoOp` returns false → full pipeline re-executes.

**Why missing**: The controller watch test (`modulerelease_source_watch_test.go`) proves that an OCIRepository change *triggers* reconciliation, but uses a no-op reconciler. No test proves the full pipeline re-executes with new content when the source changes.

**Why needed**: Source updates (new OCI artifact pushed) are the primary GitOps trigger. If source digest propagation breaks, the controller stops reacting to new pushes. This is the second most common real-world flow.

**Validates**: OCIRepository artifact revision changes → source digest differs → re-fetch → re-render → re-apply → new content in cluster → new lastAppliedSourceDigest.

---

### Group 2 — Stale Pruning in Normal Reconcile (`stale_pruning_test.go`)

#### 2.1 Render removes a resource → stale resource pruned from cluster

**Code path**: Phase 4 stale set computation (`modulerelease.go:255-259`) → Phase 6 prune (`modulerelease.go:285-297`).

**Why missing**: Integration tests cover `apply.Prune()` directly (in `test/integration/apply/prune_test.go`). Controller tests cover deletion-path pruning. But **no test exercises the normal reconcile prune path** — where a resource disappears from the render output between cycles and the controller auto-deletes it from the cluster.

**Why needed**: This is the core garbage-collection behavior. `inventory.ComputeStaleSet` + `apply.Prune` working individually doesn't guarantee they work together through the reconcile orchestrator. A regression in Phase 4→6 wiring would leave orphaned resources.

**Validates**: First reconcile creates resources A+B → second reconcile renders only A → B is in stale set → B deleted from cluster → inventory updated to contain only A → outcome = AppliedAndPruned.

#### 2.2 Prune=false skips stale deletion in normal reconcile

**Code path**: Phase 6 condition (`modulerelease.go:285`) — `mr.Spec.Prune && len(staleSet) > 0` is false when prune disabled.

**Why missing**: `prune=false` is only tested in the deletion path (`modulerelease_reconcile_test.go`). The Phase 6 branch that skips prune during normal reconciliation has no dedicated test.

**Why needed**: Users who set `prune=false` expect stale resources to remain. If the condition is accidentally inverted or removed, resources get deleted unexpectedly.

**Validates**: First reconcile creates A+B → second reconcile renders only A → B is stale → but prune=false → B remains in cluster → outcome = Applied (not AppliedAndPruned).

#### 2.3 Multiple resources: only removed resource is pruned

**Code path**: Same as 2.1, but with a larger resource set to verify selectivity.

**Why missing**: All existing tests render a single ConfigMap. No test exercises the stale set computation with multiple resources where only a subset becomes stale.

**Why needed**: `ComputeStaleSet` uses `IdentityEqual` matching. If the identity comparison has an edge case with multiple resources of the same kind, selective pruning could fail (prune too many or too few).

**Validates**: First reconcile creates A+B+C → module changes so only B is removed → A and C remain, B deleted → inventory reflects A+C.

---

### Group 3 — State Recovery (`state_recovery_test.go`)

#### 3.1 Stalled → Ready: source appears after being missing

**Code path**: Phase 1 → `FailedStalled` (source not found) → user creates source → next reconcile → Phase 1 succeeds → full pipeline → `MarkReady`.

**Why missing**: The existing "source not found" test (`reconcile_test.go`) proves the stalled condition is set. No test proves the controller **recovers** when the source appears.

**Why needed**: Stalled conditions should be temporary — users fix the issue, and the controller should self-heal on next reconcile. If `MarkReconciling` doesn't properly clear `Stalled`, or if the serial patcher mishandles the transition, the CR stays stuck.

**Validates**: First reconcile → Stalled=True (source missing) → create OCIRepository → second reconcile → Ready=True, Stalled removed, resources applied.

#### 3.2 SoftBlocked → Ready: source becomes ready

**Code path**: Phase 1 → `SoftBlocked` (source not ready, 30s requeue) → source artifact appears → next reconcile → full pipeline succeeds.

**Why missing**: The "source not ready" test proves the 30s requeue and condition. No test proves recovery when the source transitions to ready.

**Why needed**: SoftBlocked is the most common transient state (source is syncing). The recovery path exercises condition clearing (`MarkReconciling` removes stalled, then `MarkReady` sets final state). If the condition lifecycle has a bug, users see perpetual "not ready" despite source being fine.

**Validates**: First reconcile → SourceReady=False, requeue 30s → update OCIRepository status to ready → second reconcile → Ready=True, SourceReady=True, resources applied.

#### 3.3 Suspend → unsuspend resumes full reconcile

**Code path**: Suspend check (`modulerelease.go:148`) returns early → user sets `suspend=false` → next reconcile → full pipeline runs.

**Why missing**: The suspend test proves the early return. No test proves that **removing** suspend causes a full reconcile to execute and produce resources.

**Why needed**: Suspend is a safety gate. If unsuspend doesn't cleanly resume (e.g., stale conditions from the suspended state interfere), users must delete and recreate the CR.

**Validates**: First reconcile with suspend=true → Reconciling=True, no resources → update spec.suspend=false → second reconcile → Ready=True, resources applied in cluster.

---

### Group 4 — Status Tracking (`status_tracking_test.go`)

#### 4.1 ObservedGeneration tracks across spec changes

**Code path**: Deferred status commit (`modulerelease.go:96`) — `mr.Status.ObservedGeneration = mr.Generation`.

**Why missing**: ObservedGeneration is asserted incidentally in some tests but no test explicitly verifies it increments correctly across multiple spec changes.

**Why needed**: ObservedGeneration is the standard K8s mechanism for clients to know whether the controller has processed the latest spec. If it's stale, tools like `kubectl wait` and GitOps dashboards report incorrect status.

**Validates**: Create MR (gen=1) → reconcile → ObservedGeneration=1 → update spec.values (gen=2) → reconcile → ObservedGeneration=2.

#### 4.2 History records across success → failure → success

**Code path**: Deferred status commit (`modulerelease.go:124-129`) — success entry, failure entry, success entry.

**Why missing**: Existing tests check history for a single reconcile outcome. No test exercises the sequence across multiple outcomes and verifies ordering, sequences, and trimming.

**Why needed**: History is the operational ledger. If entries overwrite each other or sequences don't increment monotonically across mixed outcomes, debugging becomes unreliable.

**Validates**: First reconcile succeeds (history[0] = success) → inject fetch error → second reconcile fails (history[0] = failure, history[1] = success) → fix error → third reconcile succeeds (history[0] = success, [1] = failure, [2] = success). Verify sequences are 1, 2, 3.

#### 4.3 ForceConflicts propagates through spec.rollout

**Code path**: Phase 5 (`modulerelease.go:270`) — `force := mr.Spec.Rollout != nil && mr.Spec.Rollout.ForceConflicts`.

**Why missing**: SSA force is tested at the apply integration level, but no reconcile-level test sets `spec.rollout.forceConflicts=true` and verifies it reaches the apply layer (i.e., overwrites a field owned by another manager).

**Why needed**: ForceConflicts is a user-facing spec field. If the wiring from spec → apply is broken, users can't resolve ownership conflicts without manual intervention.

**Validates**: External manager owns a field on a resource → reconcile with forceConflicts=false fails or leaves field → reconcile with forceConflicts=true takes ownership → resource reflects controller's desired state.

#### 4.4 Cross-namespace sourceRef resolution

**Code path**: Phase 1 source resolve — `source.Resolve()` uses `sourceRef.Namespace` when specified, falls back to release namespace.

**Why missing**: Unit test in `internal/source/resolve_test.go` covers this. No reconcile-level test creates an OCIRepository in a different namespace and references it via `sourceRef.namespace`.

**Why needed**: Cross-namespace source references are a real deployment pattern (shared source namespace). If the namespace passthrough from spec → resolve is broken, users get "source not found" errors for valid configurations.

**Validates**: Create OCIRepository in namespace "sources" → create ModuleRelease in "default" with sourceRef.namespace="sources" → reconcile succeeds → resources applied.

---

## E2E Test Catalog

### Group 5 — Full Lifecycle (`lifecycle_test.go`)

#### 5.1 Create → Ready → update → re-reconcile → delete → cleanup

**Why e2e**: Requires a deployed controller processing watch events, real reconcile timing, and real deletion flow. Envtest tests call `ReconcileModuleRelease` directly — they don't exercise the controller manager, leader election, or watch-triggered reconciliation.

**Why needed**: This is the single most important end-to-end confidence test. Every envtest test is a simulation. This test proves the actual deployed controller performs the complete lifecycle.

**Validates**: Deploy controller → create OCIRepository + ModuleRelease → Eventually Ready=True → managed resources exist → update spec.values → Eventually resources updated → delete ModuleRelease → Eventually managed resources cleaned up and CR removed.

#### 5.2 Real OCI artifact fetch

**Why e2e**: Every envtest test uses `copyDirFetcher` or `stubFetcher`. No test exercises the real HTTP fetch → digest verification → zip extraction → CUE validation pipeline against a real artifact server.

**Why needed**: The fetch pipeline has multiple failure modes (digest mismatch, size limit, corrupt zip, missing CUE module) that are individually unit-tested but never exercised as a complete chain with real HTTP.

**Validates**: Push OCI artifact to test registry → create OCIRepository pointing at it → create ModuleRelease → controller fetches real artifact → renders → applies → Ready=True.

### Group 6 — Concurrent Reconciliation (`concurrent_test.go`)

#### 6.1 Multiple ModuleReleases from same source

**Why e2e**: Requires real controller concurrency (work queue, rate limiting). Envtest tests are sequential single-reconcile calls.

**Why needed**: Real clusters will have multiple ModuleReleases. If the controller has shared state issues or resource manager conflicts, they only manifest under concurrency.

**Validates**: Create shared OCIRepository → create 3 ModuleReleases referencing it → all reach Ready=True → managed resources for each are distinct and correct.

#### 6.2 Controller restart mid-reconcile (idempotency)

**Why e2e**: Requires killing and restarting the controller pod. Envtest has no process lifecycle.

**Why needed**: The reconcile loop must be idempotent — if the controller crashes after apply but before status patch, the next reconcile must converge correctly (not duplicate resources or skip prune).

**Validates**: Create ModuleRelease → reconcile starts → kill controller pod → pod restarts → Eventually Ready=True → resources correct (no duplicates, no orphans).

---

## Risks / Trade-offs

**Stub tests pass silently** → Stubs use `Skip()`, so `task dev:test` stays green but coverage doesn't actually improve until bodies are implemented. Mitigation: tasks track implementation as follow-up work.

**Test isolation** → Integration tests sharing the same envtest instance need unique resource names to avoid collisions. The existing pattern (unique names per test like `render-fail-mr`) works. New tests must follow this.

**E2e infrastructure requirements** → lifecycle and concurrent tests need an OCI registry in the Kind cluster. This may require extending the e2e suite setup. Mitigation: stub now, solve infrastructure in a separate change.
