# RBAC Delegation: Crossplane and Flux Comparison

## Summary

Concrete research into how Crossplane and FluxCD solve the same problem opm-operator faces today: an operator that applies arbitrary rendered manifests needs *some* path to privilege, but should not itself be `cluster-admin` and should not require hand-authored RBAC per release.

Companion to [`impersonation-and-privilege-escalation.md`](impersonation-and-privilege-escalation.md), which enumerates the option space. This document pins the option space to two real-world implementations and answers one specific question: **can we eliminate per-`ModuleRelease`/`BundleRelease` RBAC authoring without losing meaningful security?**

Short answer: **yes, by collapsing to per-tenant (namespace) RBAC, and the mechanism Flux uses is directly portable**. Per-tenant RBAC is what Flux users actually write in practice. Per-release RBAC is a straw man; neither Crossplane nor Flux requires it.

## Crossplane: `rbac-manager` as a separate, trusted sibling

### Mechanism

- **Separate pod, separate SA.** `rbac-manager` is not the core Crossplane controller. It has its own Deployment and ServiceAccount. This is deliberate isolation — only `rbac-manager` holds `escalate` and `bind` on `rbac.authorization.k8s.io`, so a compromised core pod cannot fabricate privileges.
- **Aggregated ClusterRoles as the extension point.** Crossplane defines three empty ClusterRoles (`crossplane-admin`, `crossplane-edit`, `crossplane-view`) whose rules are built from an `aggregationRule.clusterRoleSelectors` label match. Any ClusterRole that later appears with the right label (e.g. `rbac.crossplane.io/aggregate-to-crossplane-admin: "true"`) is merged in by the apiserver — no rebinding required. When a Provider installs, rbac-manager creates a per-Provider ClusterRole with the aggregation label; existing admin/edit/view bindings immediately pick up the new verbs.
- **Provider system ClusterRole derived from CRDs.** rbac-manager reads the `ProviderRevision`, lists the CRDs it owns, and synthesises a ClusterRole granting `[get, list, watch, update, patch]` on exactly those `apiGroups`/`resources`. Plus baseline perms on `events`, `secrets`, `configmaps`, `leases`. The Provider pod itself is not `cluster-admin` and not aggregated into the user-facing roles — it is scoped to its own types.
- **Allowlist gate.** `--provider-clusterrole=crossplane:allowed-provider-permissions` is an allowlist ClusterRole that defines the *upper bound* of what any Provider package may declare. rbac-manager validates every requested permission against the allowlist before minting a ClusterRole for the Provider.
- **Opt-out flag.** `--manage=all|basic|serviceaccounts` lets operators disable most of rbac-manager's behavior so RBAC can be pre-authored out-of-band.

### What Crossplane does NOT do

- It does not bind the user-facing `crossplane-admin/edit/view` roles to any user or group. Binding humans is left to the platform admin.
- It does not create RBAC per Claim/Composite — only per Provider and per XRD. This is the key point: **Crossplane's "per-release" equivalent (a Claim instance) gets zero bespoke RBAC**. Aggregation gives the Provider pod what it needs once, for all instances of the types it reconciles.
- It does not impersonate. The Provider pod authenticates as its own SA and reconciles its own CRDs with the rights rbac-manager granted it.

### Relevance to opm-operator

Crossplane's model is a poor direct fit because Providers reconcile a *fixed, declared* set of CRDs. opm-operator reconciles `ModuleRelease`/`BundleRelease` that apply *arbitrary* rendered manifests — the set of target GVKs is not known until render time. The Provider pattern assumes the resource set is static. You could mint a ClusterRole per module (not per release) from the union of GVKs the module's rendered manifests could produce, but that requires knowing the full GVK surface at publish time, which is harder for CUE templates than for a Go Provider.

Portable lessons:

1. **Split the trusted component.** If opm-operator ever needs `escalate`/`bind`, put it in a sibling (`opm-rbac-manager`) whose only job is provisioning ServiceAccounts. The core ModuleRelease reconciler never holds those verbs.
2. **Allowlist ceiling.** An `opm:allowed-module-permissions` ClusterRole caps what any module-derived SA can hold. Admins widen it once, not per release.
3. **Aggregation for human-facing roles.** If we ever expose "ModuleRelease admin/edit/view" to humans, aggregate via labels — don't rebind on every install.

## Flux: ServiceAccount impersonation, per-tenant RBAC

### Mechanism

- **Controller RBAC: `cluster-admin` by default.** Surprising but true: the stock install binds `kustomize-controller` and `helm-controller` to `cluster-admin` via a `cluster-reconciler` ClusterRoleBinding. This is a *floor*, and it only applies when a `Kustomization` / `HelmRelease` does **not** specify `.spec.serviceAccountName`.
- **SA impersonation overrides the floor.** When `.spec.serviceAccountName` is set, the controller builds a per-reconcile REST config with `rest.ImpersonationConfig{UserName: "system:serviceaccount:<ns>:<sa>"}` (see `fluxcd/pkg/runtime/client/impersonator.go`). The apiserver evaluates every create/patch against *that* SA's RBAC. Floor becomes irrelevant.
- **`--default-service-account` lockdown.** Setting this flag (typically to `default`) forces any `Kustomization`/`HelmRelease` that omits `.spec.serviceAccountName` to use `system:serviceaccount:<ns>:default`, which by convention has no rights. Combined with `--no-cross-namespace-refs=true` and `--no-remote-bases=true`, this gives a tenant-safe posture without tightening controller RBAC further.
- **Per-tenant RBAC, not per-release.** The official `fluxcd/flux2-multi-tenancy` pattern creates *one* ServiceAccount + *one* RoleBinding per tenant namespace. All `Kustomization`s in that namespace reference the same SA. Tenants write as many releases as they want against the same pre-authored RBAC.
- **No SAR pre-check.** Flux does not run a `SubjectAccessReview` before apply. It relies on the apiserver to return `forbidden` if the impersonated SA lacks a verb. Apply surfaces the forbidden error to the user via status.
- **Apiserver escalation prevention is inherited.** The apiserver's built-in `escalate`/`bind` checks on `rbac.authorization.k8s.io` stop a tenant SA from writing itself broader RBAC than its binder had.

### What Flux does NOT do

- It does **not** create per-`Kustomization` RBAC. There is no CRD-level `impersonate` grant — the controller either uses its broad floor or impersonates a pre-existing SA written by a human admin.
- It does not verify which human principal created the `Kustomization`. Tenancy boundary = namespace, not creator identity. A CI bot in the right namespace is indistinguishable from a human in the right namespace.
- It does not limit which kinds can appear in the rendered manifest set. Defense is entirely via the impersonated SA's RBAC.

### Relevance to opm-operator

opm-operator already implements the Flux pattern for the apply path. `spec.serviceAccountName` is wired end-to-end, `rest.ImpersonationConfig` is assembled with `UserName` + the standard SA group set, and same-namespace-only is enforced. See the `serviceaccount-impersonation` spec.

Gaps vs. Flux today:

| Feature | Flux | opm-operator |
|---------|------|--------------|
| `--default-service-account` flag | Yes | No (empty SA falls back to controller identity) |
| `cluster-admin` on the controller by default | Yes, as floor | **No** — controller has narrow RBAC (good) |
| `--no-cross-namespace-refs` | Yes | Enforced implicitly (SA must be same-namespace) |
| Per-tenant RBAC convention documented | Yes (`flux2-multi-tenancy`) | Not yet |
| `impersonate` grant on the controller | Limited to `serviceaccounts` (in lockdown overlay) | Limited to `serviceaccounts` (matches) |

The existing impersonation design doc notes that an empty `spec.serviceAccountName` falls back to the controller's own identity, and apply will fail because the controller's RBAC is narrow. That's a reasonable default. Flux's `--default-service-account=default` is more explicit: it forces the failure into an apiserver-level `forbidden` that is attributable to a real SA, which gives better audit and avoids the mental model "controller identity is a fallback."

## The specific question: can we avoid per-release RBAC?

Yes. Neither Crossplane nor Flux requires per-release RBAC. The mechanism already shipped in opm-operator (SA impersonation via `spec.serviceAccountName`) lets one pre-authored SA serve many releases. The convention that makes this safe is **per-tenant RBAC, scoped by namespace**:

```yaml
# Platform admin provisions ONCE per tenant namespace:
apiVersion: v1
kind: ServiceAccount
metadata: { name: deployer, namespace: team-a }
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata: { name: deployer, namespace: team-a }
roleRef: { kind: ClusterRole, name: edit, apiGroup: rbac.authorization.k8s.io }
subjects:
  - kind: ServiceAccount
    name: deployer
    namespace: team-a

# Tenant writes any number of releases against that SA:
apiVersion: releases.opmodel.dev/v1alpha1
kind: ModuleRelease
metadata: { name: app-a, namespace: team-a }
spec:
  serviceAccountName: deployer
  # ... rest of spec
```

One SA, one binding, N releases. RBAC is authored at **tenancy** granularity, not release granularity.

### Why this is secure enough without more machinery

- **The apiserver enforces every apply.** Impersonated requests go through normal authz. The controller has no way to bypass.
- **Namespace is the tenancy boundary.** Same as Flux, same as most K8s-native tooling. If a tenant can create releases in namespace X, they can cause apply as the SA in X. That's the contract.
- **Controller's own RBAC stays narrow.** We do *not* need Flux's `cluster-admin` floor. Keep the existing `impersonate` on `serviceaccounts` only. This is strictly better than Flux's default posture.

### Where this is NOT secure enough

The escalation gadget in [`impersonation-and-privilege-escalation.md`](impersonation-and-privilege-escalation.md) §"Threat Model" still applies: if a privileged SA (e.g. one bound to `cluster-admin`) exists in a tenant's own namespace, a tenant who can create releases can impersonate it. Namespace-scoping does not fix misplaced SAs. **This is the same risk Flux has**, and Flux addresses it by convention and documentation, not by mechanism.

The cheapest defense-in-depth that goes beyond Flux:

1. **Static deny-list** (Option H-lite in the existing doc): refuse to impersonate any SA whose effective permissions include `*/*` or any `system:*` ClusterRole, discovered via `SubjectAccessReview` on a canary verb at reconcile start. Small, non-invasive, catches the cluster-admin-SA-in-wrong-namespace footgun.
2. **Require `spec.serviceAccountName`** (already recommended in the existing doc): remove the controller-identity fallback entirely. Forces intentional SA choice, closes the "empty means controller" path.

Both are deliverable in a single small change and meaningfully exceed Flux's stock posture.

## Proposed direction

Near-term (aligns with scope-and-non-goals §9 deferral):

- **Adopt Flux's per-tenant convention as documented practice.** Add a tenancy guide to `docs/` showing: one namespace per tenant, one `deployer` SA per namespace, RoleBinding to built-in `edit` or a curated ClusterRole, all releases reference `deployer`. No per-release RBAC.
- **Add `--default-service-account` flag.** Match Flux's lockdown mechanism. When set, empty `spec.serviceAccountName` resolves to `system:serviceaccount:<releaseNamespace>:<flag-value>` instead of falling back to controller identity. Cleaner failure mode, better audit.
- **Add a deny-list of known-privileged roles.** Controller refuses impersonation if the target SA resolves to `cluster-admin` or any `system:*` role via SAR probe. One extra reconcile step, large blast-radius reduction.
- **Keep the controller's `impersonate` scope to `serviceaccounts` only.** Do not widen to `users`/`groups`. This is already the case and it is better than Flux's default.

Long-term (post-PoC):

- **Reconsider capabilities (Option C in the existing doc) only if the per-tenant SA model proves insufficient.** The CUE module declares the GVK set it renders; a sibling `opm-rbac-manager` mints a per-module ServiceAccount from that declaration, bound to a module-scoped ClusterRole. This is Crossplane's pattern adapted for our render-time GVK surface. Defer until there is a concrete tenancy requirement the per-tenant model cannot meet.
- **Do not build per-release RBAC automation.** It is the wrong granularity. Neither reference implementation does it, and the cost (admission complexity, lifecycle coupling, orphaned RBAC on release deletion) dwarfs the benefit.

## Sources

- Crossplane RBAC Manager design doc: https://github.com/crossplane/crossplane/blob/main/design/design-doc-rbac-manager.md
- Flux multi-tenancy guide: https://fluxcd.io/flux/installation/configuration/multitenancy/
- Flux multi-tenancy reference repo: https://github.com/fluxcd/flux2-multi-tenancy
- `fluxcd/pkg/runtime/client/impersonator.go` — `setImpersonationConfig`
- Kubernetes RBAC privilege-escalation prevention: https://kubernetes.io/docs/reference/access-authn-authz/rbac/#privilege-escalation-prevention-and-bootstrapping
- Existing analysis: [`impersonation-and-privilege-escalation.md`](impersonation-and-privilege-escalation.md)
- Current contract: `openspec/specs/serviceaccount-impersonation/spec.md`
