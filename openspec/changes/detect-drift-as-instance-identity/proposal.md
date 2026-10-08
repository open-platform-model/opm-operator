## Why

A ModuleInstance applies its objects as its effective ServiceAccount (`spec.serviceAccountName`, else `--default-service-account`), but its drift detection, a server-side dry-run apply, runs as the operator (opm-operator issue 209). So the operator needs, and uses, write access to every tenant object only to compare it, and the answer is not the one an apply would get: under a least-privilege operator role the dry-run is refused, `status.failureCounters.drift` grows and drift is never reported.

## What Changes

- Drift detection of a ModuleInstance runs through the same client as its apply: the impersonated ServiceAccount when one is effective, the operator's own client when none is. The reconcile builds that client once, before drift detection, and the apply, the prune and the health reads of the same reconcile reuse it.
- When the effective identity may not dry-run an object (the API server answers `Forbidden`), the reconcile sets `Drifted=Unknown` with the new reason `DriftCheckForbidden` and the API server's message, which names the identity and the object. Today the condition is left as it was.
- When the effective ServiceAccount cannot be impersonated at all (it does not exist, or the client cannot be built), drift detection does not run and the reconcile sets `Drifted=Unknown` with reason `ImpersonationFailed`. It never falls back to the operator's identity.
- Both cases count as a failed drift detection (`status.failureCounters.drift`), restore nothing and do not move `Ready`: a reconcile with unchanged digests stays a `NoOp`, and a reconcile that applies stalls or fails in the apply phase as it does today.
- Docs state which identity runs drift detection and list the new reason.

No change to `api/v1alpha1`, no new flag, no RBAC change: the operator's ClusterRole already holds `impersonate` on ServiceAccounts, and the dry-run needs no rule the apply does not need. ModulePackage runs no drift detection and is not changed.

SemVer class: PATCH after GA (a wrong identity is corrected; a condition reason is added); in beta it ships in the next `-beta.N`. Not breaking: an instance whose ServiceAccount can apply its objects can dry-run them.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `drift-detection`: a new requirement for the identity of the dry-run and one for a drift check that is refused or has no identity; the requirement "Drift detection failure increments counter" states the two new outcomes.
- `serviceaccount-impersonation`: the requirement "Impersonated client for apply and prune" covers drift detection.
- `status-conditions`: the reason `DriftCheckForbidden` and the helper that sets `Drifted=Unknown`.

## Impact

- Code: `internal/reconcile/moduleinstance.go` (the apply client is built before drift detection), `internal/status/conditions.go` (the reason and its helper). `internal/apply/drift.go` and `internal/apply/impersonate.go` keep their signatures.
- Tests: `internal/apply/drift_identity_test.go` (the dry-run carries the impersonation header), `test/integration/reconcile/drift_identity_test.go` (envtest: identity, refusal, missing ServiceAccount, flag default), `internal/status` unit test.
- Docs: `docs/RENDERING.md`, `docs/site/diagnostics/operator-conditions.md`, the tenancy page, `adr/012-drift-detection-only.md`.
- Load: none added. A rendering reconcile of an impersonating instance already built one impersonated client (for health on a `NoOp`, for apply otherwise); it now builds it earlier and once.
- Operators who relied on the operator's own rights for drift detection (a ServiceAccount that may not `patch` what it once applied) now read `Drifted=Unknown` with `DriftCheckForbidden` where they read a drift verdict.
