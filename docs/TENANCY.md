# Multi-Tenancy with opm-operator

## Audience

Platform administrators and operators running opm-operator in a shared cluster where multiple teams (tenants) author `ModuleRelease` and `Release` objects against pre-provisioned RBAC.

## Summary

opm-operator's tenancy boundary is the **Kubernetes namespace**. Per-tenant RBAC, not per-release RBAC, is the convention. One ServiceAccount and one RoleBinding per tenant namespace; every release in that namespace references the same SA via `spec.serviceAccountName`. This matches the Flux [multi-tenancy pattern](https://fluxcd.io/flux/installation/configuration/multitenancy/) and is the smallest primitive that stays safe without admission plugins.

## Recommended pattern: one SA per tenant namespace

For each tenant namespace:

1. Create the namespace.
2. Create one ServiceAccount (example: `opm-deployer`).
3. Create one RoleBinding (or ClusterRoleBinding scoped to the namespace) granting that SA exactly the verbs modules in this namespace are allowed to apply.
4. Reference the SA from every `ModuleRelease` / `Release` via `spec.serviceAccountName`.

The controller's own RBAC stays narrow: `get` + `impersonate` on `serviceaccounts` only. Workload writes happen through impersonation; the tenant SA's RBAC determines what may be applied.

## Worked example: two ModuleReleases sharing one SA

Platform admin provisions the SA and binding once per tenant namespace:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: team-a
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: opm-deployer
  namespace: team-a
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: opm-deployer
  namespace: team-a
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: edit
subjects:
  - kind: ServiceAccount
    name: opm-deployer
    namespace: team-a
```

> **Warning:** `edit` is shown for brevity. In production, bind to a narrower custom ClusterRole that lists exactly the GVKs your modules render. Avoid `admin` and any role that includes `rbac.authorization.k8s.io` verbs unless modules are trusted to author RBAC.

Tenant writes any number of releases against that SA:

```yaml
apiVersion: releases.opmodel.dev/v1alpha1
kind: ModuleRelease
metadata:
  name: frontend
  namespace: team-a
spec:
  module:
    path: opmodel.dev/team-a/frontend
    version: v1.2.0
  serviceAccountName: opm-deployer
  prune: true
  values:
    replicas: 3
---
apiVersion: releases.opmodel.dev/v1alpha1
kind: ModuleRelease
metadata:
  name: worker
  namespace: team-a
spec:
  module:
    path: opmodel.dev/team-a/worker
    version: v0.5.1
  serviceAccountName: opm-deployer
  prune: true
```

Both releases impersonate `system:serviceaccount:team-a:opm-deployer` when applying. RBAC is authored at tenancy granularity, not release granularity.

## Lockdown: `--default-service-account`

For clusters where every release should be impersonated — no controller-identity fallback — start the manager with `--default-service-account`:

```bash
opm-operator --default-service-account=opm-deployer
```

Semantics:

| Situation | Effective identity |
|-----------|-------------------|
| `spec.serviceAccountName` non-empty | The named SA (flag ignored). |
| `spec.serviceAccountName` empty, flag empty | Controller's own identity (default, narrow RBAC). |
| `spec.serviceAccountName` empty, flag non-empty | `system:serviceaccount:<releaseNamespace>:<flag-value>`. |

The SA named by the flag must exist in **each** tenant namespace. It is never resolved cross-namespace — an SA with the same name in the controller's namespace does not satisfy a release in a tenant namespace.

If the flag-defaulted SA is missing, the reconcile stalls:

| Condition | Status | Reason |
|-----------|--------|--------|
| `Ready` | `False` | `ImpersonationFailed` |
| `Stalled` | `True` | `ImpersonationFailed` |

### Pick an explicit flag value

Use something explicit like `opm-deployer`. The built-in `default` SA has no RBAC, so `--default-service-account=default` will cause every empty-SA release to fail `forbidden` — a predictable lockdown, but only intentional if that is what you want.

### Conventional provisioning

A bootstrap tool (Argo, Flux, Terraform, `kustomize`) that creates tenant namespaces should also create the flag-named SA and its RoleBinding. Drift in the convention surfaces as `ImpersonationFailed` on the first release in an under-provisioned namespace.

### Deletion cleanup

Finalizer-driven deletion cleanup follows the same `spec.serviceAccountName > --default-service-account > controller client` resolution as apply and prune — a release with empty spec + a manager flag prunes inventory under the flag-defaulted identity. Deletion is best-effort: if the resolved SA cannot be impersonated (for example, the SA was deleted before the release), the controller logs the failure and falls back to its own client so the finalizer eventually clears. Deletion will never stall indefinitely on an impersonation problem; operators relying on the flag for tenancy isolation should audit finalizer-fallback events when tenant namespaces are torn down out-of-order.

## Security note

ServiceAccount impersonation shifts the privilege boundary onto *which SA a release may reference*. If a privileged SA (one bound to `cluster-admin`, or to any `system:*` role) exists in a tenant namespace, any tenant who can create a release in that namespace can cause the controller to apply arbitrary resources under that identity. The controller enforces same-namespace SA references, but it does not police the target SA's bindings.

Read [`docs/design/impersonation-and-privilege-escalation.md`](design/impersonation-and-privilege-escalation.md) — the threat model section describes the escalation gadget in full. The `--default-service-account` flag narrows the fallback surface but does not close this gadget; it is still possible to misplace a privileged SA in a tenant namespace.

Rules of thumb:

- Never place a `cluster-admin`-bound SA in a tenant namespace.
- Audit every `RoleBinding` whose subject is the tenancy SA.
- Prefer custom namespace-scoped Roles over `edit`/`admin` ClusterRoles when the rendered GVK set is known.
- Treat the tenant-SA permissions as a tenant's maximum blast radius. If that radius is too large, narrow the SA's binding, not the controller.

## Related reading

- [`docs/design/rbac-delegation-crossplane-flux.md`](design/rbac-delegation-crossplane-flux.md) — comparative research with Crossplane's `rbac-manager` and Flux's per-tenant convention.
- [`docs/design/impersonation-and-privilege-escalation.md`](design/impersonation-and-privilege-escalation.md) — threat model and option space for future hardening.
- [Flux multi-tenancy reference](https://github.com/fluxcd/flux2-multi-tenancy) — the pattern opm-operator's convention is modelled on.
