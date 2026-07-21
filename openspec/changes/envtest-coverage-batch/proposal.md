# Proposal: envtest-coverage-batch

Test-only change from the 2026-07-21 workspace fixture audit. No controller behavior changes.

## Why

The audit found the operator's envtest tier excellent where it exists but carrying a backlog of never-written specs: five integration files hold TODO/Skip stubs for behaviors that shipped long ago (status tracking, change propagation, stale-prune during reconcile, state recovery, CRD-before-CR apply ordering), and two contracts that enhancement 0006 leans on are asserted only by analysis, not tests — the prune ownership guard's tolerance of resources labeled with the **CLI's** manager identity (`opm-cli`, the D40 post-handoff window), and the *empty-owner ⇒ operator-managed reconcile* default that the version-skew story (D24) assumes. All are fillable with the existing envtest harness (`reconcileParamsWithConfig`, `stubRenderer`, `testEnv.AddUser`) — this is unwritten work, not blocked work.

## What Changes

- **Prune guard tolerance test**: a live resource labeled `app.kubernetes.io/managed-by: opm-cli` with a matching instance UUID is pruned normally (not skipped) — locking `core.IsOPMManagedBy`'s cross-actor acceptance in the removed-but-not-yet-relabeled handoff window (0006 D40 drafting note).
- **Empty-owner reconcile test**: a `ModuleInstance` with no `spec.owner` receives a normal reconcile (finalizer registered, resources applied) — the operator-managed default the A4 design states in prose. (The originally-proposed unknown-owner variant is moot: CRD enum validation rejects any value other than `cli`/`operator` at the API server, which one added scenario also pins.)
- **Fill the open integration TODO stubs** (behaviors already specified in existing capabilities; tests only):
  - `status_tracking_test.go` — ObservedGeneration tracking, history across outcomes, `forceConflicts` passthrough. *Excluded*: the cross-namespace sourceRef stub (`:58`) — owned by the active `add-cross-namespace-source-grants` change.
  - `change_propagation_test.go` — values-change no-op path, source-revision propagation.
  - `stale_pruning_test.go` — stale-set prune during reconcile, `prune=false` skip, selective multi-resource prune.
  - `state_recovery_test.go` — Stalled→Ready, SoftBlocked→Ready, suspend→unsuspend recovery.
  - `apply_test.go:171` TODO — CRD-before-CR apply ordering.
- **Hygiene**: regenerate `dist/examples/` (still carries pre-rename `*-modulerelease.yaml` filenames).

## Capabilities

### Modified Capabilities

- `prune-stale-resources`: the Live-state UUID-based ownership guard requirement gains the CLI-manager-identity tolerance scenario (no behavior change).
- `module-instance-ownership`: the ownership-marker requirement gains the empty-owner-reconciles-normally scenario (no behavior change).

No other requirement changes — the stub fills implement tests for behavior already specified under `history-tracking`, `status-conditions`, `prune-stale-resources`, `suspend-resume`, `reconcile-backoff`, and `ssa-apply`.

## Impact

- **Packages**: `test/integration/**` (new specs in existing files), `test/integration/apply/prune_test.go`, `internal/controller`/`test/integration/reconcile` ownership tests, `dist/examples/` regen.
- **SemVer**: none. Commit type `test:` (+ `chore:` for the regen).
- **Dependencies**: none; all envtest tier (`task dev:test`). No kind, no Flux, no registry.
