## Why

When a `ModuleRelease` or `Release` is deleted at the same time as its impersonation ServiceAccount (classic `kubectl delete -f` racing all objects in one file), the finalizer path currently logs an `INFO` line "ServiceAccount unavailable for deletion cleanup, using controller client" and silently falls back to the controller's own client. The controller's RBAC is narrow (no workload verbs), so prune then fails with a forbidden error that is attributed to the controller's identity rather than the missing SA. The finalizer is retained, the reconciler re-queues forever, and logs are filled with misleading forbidden errors against the controller SA. Observed in kind smoke test on 2026-04-19 with the `hello` fixture.

Root cause is operator-facing: the release spec references an SA that no longer exists. The controller cannot prune without it. Retrying silently until the heat death of the cluster is the wrong default — the operator needs a clear, actionable signal and a documented escape hatch.

## What Changes

- Introduce condition reason `DeletionSAMissing` (distinct from the generic `ImpersonationFailed` used during apply). Emitted only on the deletion cleanup path when the impersonation client cannot be built because the named SA does not exist.
- On deletion-cleanup SA-missing:
  - Stop silently falling back to the controller's own client.
  - Stall the release with `Ready=False, reason=DeletionSAMissing`, message naming the missing SA and listing recovery verbs.
  - Emit a `Warning` event with the same reason.
  - Retain the finalizer and requeue on a longer backoff (operator-attention interval, not tight-loop).
- Introduce an escape-hatch annotation `opm.dev/force-delete-orphan: "true"` on the release. When the annotation is present AND the deletion-cleanup impersonation fails with SA-missing, the controller:
  - Skips prune.
  - Removes the finalizer.
  - Emits a `Warning` event naming the orphaned inventory size so operators have an audit trail.
  - Does NOT attempt to apply anything; this is a one-way exit from the stuck state.
- Apply this behavior consistently in both `internal/reconcile/modulerelease.go` (`handleDeletion`) and `internal/reconcile/release.go` (the `Release` equivalent). Both currently share the silent-fallback bug.
- Other impersonation errors on the deletion path (transient API errors, controller lacks `impersonate` verb, etc.) keep the existing stall-and-requeue behavior with the generic `ImpersonationFailed` reason. Only the specific "SA does not exist" case gets the new reason + escape hatch, because only it has a deterministic recovery flow.

Not in scope:
- No change to the apply path impersonation behavior.
- No owner-references or ordered-prune changes (options #3 and #4 from discussion; deferred pending investigation).
- No fixture changes. The `hello` fixture's bundled-SA pattern stays as a regression probe for this new behavior.

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities
- `serviceaccount-impersonation`: add requirements for deletion-path SA-missing behavior — distinct condition reason, no silent fallback, escape-hatch annotation for orphan-delete.
- `finalizer-and-deletion`: document the new stall state and the orphan-annotation exit.
- `prune-stale-resources`: note that a stalled-on-DeletionSAMissing release does not execute prune.

## Impact

- **Code**: `internal/reconcile/modulerelease.go` (`handleDeletion`), `internal/reconcile/release.go` (equivalent deletion block around line 613), `internal/status/conditions.go` (new reason constant `DeletionSAMissingReason`).
- **Tests**: unit tests in `internal/reconcile/` covering (a) SA missing without annotation → stall, (b) SA missing with annotation → finalizer removed, orphan event, (c) SA present → existing behavior unchanged, (d) transient impersonation error (non-NotFound) → generic `ImpersonationFailed` stall unchanged.
- **Integration**: extend `test/integration/reconcile/impersonation_test.go` with a kind-driven deletion-race scenario — create release, delete SA, delete release; assert stall then annotation-driven orphan-exit.
- **Docs**: update `docs/design/impersonation-and-privilege-escalation.md` §"What the controller already enforces" with the new row; add a short operator-runbook section to `docs/TENANCY.md` (landing in the other open change).
- **Spec**: deltas to `serviceaccount-impersonation`, `finalizer-and-deletion`, `prune-stale-resources`.
- **APIs**: no CRD schema change. The annotation is a metadata concern, not a spec field. No breaking change.
- **SemVer**: MINOR (additive annotation, additive condition reason, changed but stricter deletion behavior is bug-fix-shaped — the old behavior is an infinite retry which no one depends on).
- **Dependencies**: none.
