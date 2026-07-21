# Design: envtest-coverage-batch

## Context

Five integration files carry TODO/Skip stubs citing design docs whose behaviors shipped (see each file's header refs); the reconcile harness they need already exists and is exercised by the neighboring passing specs. Two 0006 contracts were closed by analysis only: D40's prune-guard verdict ("no gap") traced `core.IsOPMManagedBy` accepting `opm-cli` but no test seeds a live `opm-cli`-labeled resource through `Prune`; and D24's skew model assumes absent `spec.owner` reconciles as operator-managed, stated in A4's spec prose but untested on the reconcile side.

## Goals / Non-Goals

**Goals:** convert both analysis-closed contracts into envtest assertions; retire every open TODO stub this repo owns; regen the stale `dist/examples`.

**Non-Goals:** the cross-namespace sourceRef stub (`status_tracking_test.go:58` — active `add-cross-namespace-source-grants` change owns it); any e2e/kind work (separate `live-flux-e2e` change); any behavior change — a stub fill that *discovers* a behavior bug pauses and reports rather than patching code in this change.

## Decisions

### LD1: New specs live in the stub files they replace

Each TODO stub is replaced in place by a real spec in the same file/Context, preserving the design-doc references in the spec text. This keeps the historical mapping (stub ⇒ design section ⇒ test) legible and avoids a parallel test-file layout.

### LD2: Prune tolerance test mirrors the existing guard Contexts

`test/integration/apply/prune_test.go` already has Contexts for missing-label skip (`:265`), UUID-mismatch skip (`:300`), and legacy-label delete (`:375`). The new Context mirrors `:375` with `app.kubernetes.io/managed-by: opm-cli` and a **matching** UUID → asserts deletion proceeds (`PruneResult.Deleted` incremented). While editing, reconcile the spec's stale label naming (`module-release.opmodel.dev/uuid` in `prune-stale-resources` prose vs the post-0002 `module-instance.opmodel.dev/uuid` in code) — naming drift only, flagged as its own task; if the spec text is corrected it rides this change's delta.

### LD3: Empty-owner reconcile test sits with the A4 ownership tests

Next to the existing owner-skip and fall-through specs (`moduleinstance_reconcile_test.go:370-616`): create a `ModuleInstance` with `spec.owner` unset → assert finalizer registered and the stub-rendered resource applied. A companion API-level scenario asserts enum rejection of an arbitrary owner value (locks the CRD validation that makes unknown-owner reconciles unreachable).

### LD4: Stub-fill order is dependency-free, cheapest first

`apply_test.go` ordering TODO (pure apply-layer) → `stale_pruning` → `change_propagation` → `status_tracking` → `state_recovery` (needs Stalled/SoftBlocked staging via the failure-injection helpers the impersonation tests already use). Each lands as its own commit so a discovered behavior bug isolates cleanly.

## Risks / Trade-offs

- [A stub fill exposes a real behavior divergence from the design doc] → stop, record in the change, decide fix-vs-spec-amend outside this test-only scope.
- [State-recovery staging (Stalled→Ready) proves flaky under envtest timing] → use the existing `Eventually` patterns and the serial-patcher hooks; if still flaky, land the stable subset and record the flaky case explicitly rather than a permanent Skip.
