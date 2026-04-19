## Context

The controller already impersonates `system:serviceaccount:<ns>:<sa>` when `spec.serviceAccountName` is set. See `internal/apply/impersonate.go` and the `serviceaccount-impersonation` spec. When the field is empty, `buildApplyClient` in both `internal/reconcile/modulerelease.go` and `internal/reconcile/release.go` returns the controller's own client. Because the controller's RBAC is narrow (no wildcard verbs, no workload write), apply fails with an ambiguous forbidden error attributed to the controller's identity rather than a tenant-scoped SA.

FluxCD addresses the same situation with a manager flag, `--default-service-account`, which resolves an empty `spec.serviceAccountName` to a named SA in the release's namespace. The docs on multi-tenancy (https://fluxcd.io/flux/installation/configuration/multitenancy/) make this the lockdown-recommended posture. Adopting the flag gives us parity with Flux's recommended configuration without widening controller RBAC.

Separately, `docs/` has no tenancy guide. The `serviceaccount-impersonation` spec and `docs/design/impersonation-and-privilege-escalation.md` describe the mechanism and escalation surface, but they do not prescribe the per-namespace SA pattern Flux users are used to writing (`flux2-multi-tenancy`). Platform admins need a short, actionable guide.

## Goals / Non-Goals

**Goals:**
- Resolve empty `spec.serviceAccountName` to `system:serviceaccount:<releaseNamespace>:<flag-value>` when `--default-service-account` is set.
- Preserve existing behavior (controller identity) when the flag is empty. Backwards-compatible.
- Plumb the flag from `cmd/main.go` through `ModuleReleaseParams` and the equivalent struct in the `release_controller` / `bundlerelease_controller` wiring.
- Ship `docs/TENANCY.md` documenting the per-namespace SA pattern with a worked example and a security note pointing at `docs/design/impersonation-and-privilege-escalation.md`.
- Update `docs/design/README.md` and `CLAUDE.md` to reference the new guide.

**Non-Goals:**
- No widening of the controller's own RBAC. The controller continues to hold `impersonate` on `serviceaccounts` only (not `users`/`groups`). This is strictly stronger than Flux's default install.
- No deny-list for privileged target SAs (e.g. refusing SAs bound to `cluster-admin`). Deferred per `docs/design/scope-and-non-goals.md` §9.
- No capability declarations, `opm-rbac-manager` sibling, or admission webhook for SA selection. Deferred per §9.
- No change to missing-SA behavior: whether the SA comes from `spec.serviceAccountName` or the flag default, a missing SA still stalls the reconcile.
- No cross-namespace SA references. The flag value names an SA that must exist in the **release's** namespace, not the controller's.

## Decisions

### D1: Flag name and scope

Adopt Flux's name: `--default-service-account`. Single string value. Empty default preserves today's behavior exactly. Non-empty value names an SA expected in the same namespace as each release.

**Why not** `--tenant-default-sa` or `--default-impersonation-sa`? Flux's name is widely known; matching it reduces operator surprise when migrating from Flux. The flag's semantics are identical.

**Alternatives considered:**
- Per-controller flag (separate flag for `ModuleRelease` vs `BundleRelease` vs `Release`). Rejected: no evidence the three need different defaults, and YAGNI (Principle VII).
- Cluster-scoped ConfigMap. Rejected: flags are simpler for a PoC and mirror Flux.

### D2: Where resolution happens

Resolve the effective SA name inside `buildApplyClient` (and the `release_controller` equivalent) at reconcile time, not in the API type. The CRD `spec.serviceAccountName` stays unchanged; the resolution produces the impersonation target.

```go
saName := mr.Spec.ServiceAccountName
if saName == "" {
    saName = params.DefaultServiceAccount // new field, may be ""
}
if saName == "" {
    return params.ResourceManager, params.Client, nil // preserve today's fallback
}
// existing NewImpersonatedClient path, unchanged
```

**Why not** mutate `spec.serviceAccountName` at admission? Admission mutations hide operator intent in the stored object. Resolving at reconcile time keeps the CRD clean and auditable: `spec.serviceAccountName: ""` always means "use controller default", `spec.serviceAccountName: X` always means "use X explicitly".

**Why not** resolve inside `NewImpersonatedClient`? The resolution is a controller-scope concern (knows about the flag), not an apply-scope concern (cares only about a concrete SA name). Keeping `NewImpersonatedClient` ignorant of defaults preserves its testability.

### D3: No missing-default-SA stall at startup

If `--default-service-account=foo` is set but `foo` does not exist in a given release's namespace, the per-release impersonation client build fails (via existing `NewImpersonatedClient` path) and the release stalls with `ImpersonationFailed`. The controller does **not** fail to start or pre-validate the SA exists in any namespace. This matches Flux's behavior: each tenant namespace is expected to provision its own `foo` SA.

### D4: Status reason reuse

No new condition reason. `ImpersonationFailed` already covers missing-SA and forbidden-on-impersonate. Audit-wise, the `spec.serviceAccountName` being empty vs set is visible in the stored object; the effective SA name appears in log key-values (`serviceAccount`) on the apply client build path.

### D5: Tenancy guide placement

New file: `docs/TENANCY.md`. Linked from `CLAUDE.md` repository guide section and from `docs/design/README.md`. Content:

1. Recommended convention: one SA per tenant namespace.
2. Worked example (SA + RoleBinding + two `ModuleRelease` objects sharing the SA).
3. Brief note on `--default-service-account` for lockdown installations.
4. Security note linking `docs/design/impersonation-and-privilege-escalation.md` §"Threat Model" — specifically that a cluster-admin-bound SA in a tenant namespace breaks tenancy, which is inherent to SA impersonation and not solved by this change.

## Risks / Trade-offs

- **Risk:** Admins set `--default-service-account=default` without provisioning a narrow `default` SA in each tenant namespace. The built-in `default` SA has no RBAC, so apply will fail forbidden. → **Mitigation:** docs emphasize that the flag value names a convention SA the admin must create per namespace; use something explicit like `opm-deployer`, not `default`.
- **Risk:** Operators believe the flag eliminates the escalation gadget. It does not — a privileged SA in a tenant namespace is still impersonable. → **Mitigation:** tenancy guide cross-links `docs/design/impersonation-and-privilege-escalation.md` so readers see the threat model in the same flow.
- **Trade-off:** Flag state is per-controller-instance. Moving between clusters with different flag values could surprise operators. Acceptable: same as every other manager flag. Document in the flag help text.
- **Trade-off:** `ModuleReleaseParams` and the `release_controller` params struct both grow a field. Slightly more plumbing. Acceptable: the alternative (global variable or singleton) is worse.

## Migration Plan

Additive flag, empty default. No migration required. Existing deployments continue to behave exactly as today until an operator opts in by setting the flag.

Rollback: remove the flag from the manager args. Reconciles with empty `spec.serviceAccountName` return to falling back to the controller's own client. No data migration.

## Open Questions

1. Should the default SA flag value apply to all three CRDs (`ModuleRelease`, `BundleRelease`, `Release`) uniformly, or only to leaf-apply controllers (`ModuleRelease`, `Release`)? `BundleRelease` orchestrates children and does not apply workloads directly. Lean: apply to all three for consistency; `BundleRelease` ignores it if it does not build an impersonated client of its own. Confirm during implementation.
2. Should the tenancy guide live at `docs/TENANCY.md` or under `docs/guides/tenancy.md`? Repo currently has no `docs/guides/` subdirectory. Proposal puts it flat to match `docs/STYLE.md`, `docs/TESTING.md`. Revisit if `docs/` grows.
