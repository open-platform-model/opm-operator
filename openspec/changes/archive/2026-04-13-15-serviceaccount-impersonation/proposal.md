## Why

The ModuleRelease CRD has `spec.serviceAccountName` but the controller currently applies all resources using its own service account credentials. Without impersonation, there is no RBAC boundary between tenants — the controller's broad permissions are used for every release. ServiceAccount impersonation lets cluster administrators scope each ModuleRelease's apply/prune operations to a specific SA's RBAC, enforcing least-privilege per release.

## What Changes

- When `spec.serviceAccountName` is set, build an impersonated Kubernetes client scoped to that SA.
- Use the impersonated client for apply (Phase 5) and prune (Phase 6) operations only.
- Source resolution, artifact fetch, and status patching continue using the controller's own client.
- If the specified ServiceAccount does not exist or the controller lacks impersonation RBAC, the reconcile fails with a stalled condition.
- When `spec.serviceAccountName` is empty, use the controller's own client (current behavior).

## Capabilities

### New Capabilities
- `serviceaccount-impersonation`: Build impersonated client from `spec.serviceAccountName` for apply and prune operations.

### Modified Capabilities

## Impact

- `internal/apply/` — `NewResourceManager` and `Prune` accept a client parameter (impersonated or default).
- `internal/reconcile/modulerelease.go` — Phase 0 or early phase builds impersonated client if SA specified.
- `internal/controller/modulerelease_controller.go` — RBAC markers need `impersonate` verb on ServiceAccounts.
- No API changes. Uses existing `spec.serviceAccountName`.
- SemVer: MINOR — new capability.
