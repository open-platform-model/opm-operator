## Context

`internal/source.Resolve` (`internal/source/resolve.go:42`) honors a caller-supplied `sourceRef.namespace` and looks the source up with the controller's own client, which holds cluster-wide `get;list;watch` on Flux `ocirepositories`/`gitrepositories`/`buckets` (`config/rbac/role.yaml:77`). `SourceReference` is a type alias for Flux's `NamespacedObjectKindReference` (`api/v1alpha1/common_types.go:33`), so `spec.sourceRef.namespace` is a settable, unvalidated field on every `Release`. A tenant who can create a `Release` in namespace A can point it at a source in namespace B and have the controller read and render B's private artifact into A — the CRITICAL finding from the 2026-06 security audit.

The operator's tenancy boundary is the **namespace** (`docs/TENANCY.md`), and that boundary is enforced everywhere else: `checkDependsOn` (`internal/reconcile/release.go:546`) rejects cross-namespace `dependsOn`, and SA impersonation never resolves cross-namespace (`internal/reconcile/modulerelease.go:785`). Source resolution is the one unguarded crossing. The fix restores the boundary by default, then offers a consent-based, auditable opt-in so platform teams that legitimately want shared-source namespaces are not forced to choose between "blunt global switch" and "no sharing at all."

## Goals / Non-Goals

**Goals:**
- Close the cross-namespace source read by default (default-deny), consistent with the existing cross-namespace `dependsOn` rejection.
- Provide a consent-based opt-in where the *namespace being read* grants access — never the reader.
- Make the access map first-class and auditable (`kubectl get sourcerefgrants -A`), GitOps-manageable, and policy-engine-constrainable.
- Require two independent gates (admin flag AND data-owner grant), both default-deny.
- Add no new external module dependency.

**Non-Goals:**
- Depending on `sigs.k8s.io/gateway-api` or requiring its `ReferenceGrant` CRD to be installed.
- Object-granular `from` (per-Release): the tenant boundary is the namespace, so namespace-granular `from` is the correct grain.
- Cross-namespace support for `ModuleRelease` module acquisition (registry-path based, no Flux `sourceRef`) — out of scope.
- Grant expiry, label-selector `from`, or deny-grants — deferred until a concrete need surfaces (Principle VII).

## Decisions

### D1: Default-deny implemented as a policy decision point, not a hardcoded reject
`Resolve` consults a `CrossNamespacePolicy` interface (`Allows(ctx, fromNamespace, fromKind, sourceRef) bool`) at the cross-namespace branch. The default wiring is `DenyAllCrossNamespacePolicy`. The bare reject and the opt-in feature are then the *same* code path with different policies injected — the security fix ships as the default policy and the call site never changes again.
- **Alternative considered:** a hardcoded `if ns != releaseNS { return err }` mirroring `checkDependsOn`, with the grant feature bolted on later. Rejected: it would force a second edit of the resolver and its tests when the opt-in lands, and the user has already named the opt-in as a foreseen requirement.

### D2: OPM-native `SourceRefGrant`, not Gateway API `ReferenceGrant`
We adopt the ReferenceGrant *model* — grant lives in the target namespace, `from`/`to` shape, additive, fail-closed — but as an OPM-owned CRD named `SourceRefGrant` in `releases.opmodel.dev`.
- **Alternative considered:** reuse `gateway.networking.k8s.io/v1beta1.ReferenceGrant`. Rejected: the operator is not a Gateway API consumer (`go.mod` confirms no dependency) and many clusters never install Gateway API. Coupling a Flux-source feature to an unrelated, often-absent CRD is a fragile, surprising runtime dependency. Owning the type also lets us scope it to source kinds and extend it later without negotiating an upstream schema.
- **Consequence:** we accept the cost of authoring/documenting a CRD that resembles an existing standard, in exchange for zero external coupling and full control of semantics.

### D3: Consent originates from the namespace being read
The grant lives in the target (source-owning) namespace; its `from` names the referrer namespaces. A reader naming a target buys nothing — the target must independently grant. This is the load-bearing security property; a grant signal living on the `Release` would merely rename the hole.

### D4: Two independent gates, both default-deny
A cross-namespace reference is permitted iff `--allow-cross-namespace-source-refs=true` (admin, default off) AND a matching `SourceRefGrant` exists (data owner, default none). Enabling the flag exposes nobody until owners opt in; a grant does nothing until the admin enables the mechanism. Strictly safer than either gate alone and than the Flux-style single `--no-cross-namespace-refs` switch.

### D5: Cache-backed grant evaluation, stalled on denial
Grants are read from a controller-runtime cached informer (cluster-wide `get;list;watch` on `sourcerefgrants`), not a live API read per resolution. A denial maps to `ErrCrossNamespaceForbidden` → `Stalled` (it needs an operator action to clear), matching how `checkDependsOn` failures and unsupported-kind errors already stall rather than transient-retry.

### D6: Matching algorithm
For a `Release` of kind `K_from` in `N_from` referencing a source of kind `K_to` named `S` in `N_to` (`N_to != N_from`, flag on): permit iff some `SourceRefGrant` in `N_to` has a `from` entry `{group: releases.opmodel.dev, kind: K_from, namespace: N_from}` and a `to` entry `{group: source.toolkit.fluxcd.io, kind: K_to, name: "" | S}`. Re-evaluated every reconcile, so grant deletion revokes on the next pass.

## Risks / Trade-offs

- **Behavioral change for existing deployments relying on implicit cross-namespace reads** → Document prominently in `docs/TENANCY.md` and the changelog; the migration is "set the flag and author a grant." The behavior being removed is the vulnerability, so a hard default-deny is intended.
- **`SourceRefGrant` is itself a namespaced object — "who may create grants in namespace B" becomes security-relevant** → This collapses to "is namespace RBAC correct," the operator's existing trust assumption (same as who may place a privileged SA per `docs/TENANCY.md`). No new trust root is introduced; call it out in the tenancy doc.
- **Reinventing a known standard (ReferenceGrant)** → Mitigated by mirroring its shape and semantics exactly, so operators familiar with Gateway API transfer their mental model; documented as a deliberate trade in D2.
- **In-flight fetch after grant deletion may complete once** → Negligible; next reconcile denies. No mid-fetch interruption is attempted.
- **Scope vs. Principle VIII (small batches)** → Implementation is sequenced into independently-verifiable steps (see `tasks.md`): the default-deny security fix lands and is testable before any grant/flag/CRD machinery, so the CRITICAL is closed early even if the opt-in work is staged across commits.

## Migration Plan

1. Ship the default-deny policy + `ErrCrossNamespaceForbidden` + `Stalled` wiring first. After this, the CRITICAL is closed; cross-namespace references stall with a clear reason.
2. Add the `SourceRefGrant` CRD (types, generated manifests/DeepCopy) and RBAC — inert until wired.
3. Add the `--allow-cross-namespace-source-refs` flag and the grant-backed policy; wire the informer and inject the policy in `cmd/main.go`.
4. Document in `docs/TENANCY.md` (consent model, two-gate requirement, RBAC note) and changelog.

**Rollback:** the flag defaults off and the CRD is additive, so reverting to default-deny is "set the flag false / delete grants." Reverting the default-deny itself would reopen the vulnerability and is not a supported rollback.

## Open Questions

- Should `from.kind`/`to.group` carry defaults (e.g. default `from.kind=Release`, `to.group=source.toolkit.fluxcd.io`) to reduce grant verbosity, or stay fully explicit for clarity and forward-compatibility with `BundleRelease`?
- Whether `BundleRelease` (currently rejected `UnsupportedKind`) should be enumerable in `from.kind` now or when it becomes a live render path.
