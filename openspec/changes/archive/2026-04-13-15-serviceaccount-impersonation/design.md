## Context

Kubernetes supports user impersonation via the `Impersonate-User` HTTP header. A controller can create a client that impersonates a ServiceAccount, causing all API calls through that client to be authorized against the SA's RBAC bindings. Flux's kustomize-controller implements this same pattern for multi-tenant clusters.

The controller needs `impersonate` verb RBAC on the target ServiceAccount resource for this to work.

## Goals / Non-Goals

**Goals:**
- Build an impersonated `client.Client` when `spec.serviceAccountName` is set.
- Pass the impersonated client to apply and prune functions.
- Fail with stalled condition if SA doesn't exist or impersonation is unauthorized.
- Add RBAC markers for impersonation permissions.

**Non-Goals:**
- ServiceAccount token mounting or volume injection.
- Cross-namespace impersonation (SA MUST be in the same namespace as the ModuleRelease).
- Impersonation for source resolution (source-controller handles its own auth).

## Decisions

### 1. Impersonation via rest.Config ImpersonateConfig

Build a new `rest.Config` from the controller's config with `ImpersonateConfig` set to `rest.ImpersonationConfig{UserName: "system:serviceaccount:<namespace>:<name>"}`. Create a new `client.Client` from this config. This is the standard Kubernetes impersonation mechanism.

### 2. Impersonated client used only for apply and prune

Source resolution (Phase 1), artifact fetch (Phase 2), rendering (Phase 3), and status patching (Phase 7) all use the controller's own client. Only Phase 5 (Apply) and Phase 6 (Prune) use the impersonated client. This keeps the RBAC surface minimal — the SA only needs permissions for the resources the module manages.

### 3. SA validation happens early, stalls on failure

Before entering the apply phase, the reconciler verifies the ServiceAccount exists. If it doesn't exist or impersonation is rejected (403), the reconcile is classified as `FailedStalled` — this is a configuration error that won't self-heal.

### 4. Empty serviceAccountName means use controller client

When `spec.serviceAccountName` is empty (default), the controller uses its own client for all operations. No impersonation overhead or additional RBAC needed.

### 5. Same-namespace only

The impersonated SA MUST be in the same namespace as the ModuleRelease. Cross-namespace impersonation is a security risk and is not supported in v1alpha1.

## Risks / Trade-offs

- **[Risk] RBAC misconfiguration** — If the controller's ClusterRole lacks `impersonate` verb, all impersonated reconciles fail. Mitigation: clear stalled condition message and documentation.
- **[Risk] SA permissions too narrow** — The SA may lack permissions for some resources the module renders. Mitigation: the SSA apply returns clear 403 errors; the reconcile fails with actionable error messages.
- **[Trade-off] Client construction per reconcile** — Building a new client each reconcile has overhead. Acceptable for v1alpha1; caching can be added later if profiling shows it's needed.
