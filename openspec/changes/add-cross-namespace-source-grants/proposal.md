## Why

A `Release` (or `BundleRelease`) may set `spec.sourceRef.namespace` to any value, and `internal/source.Resolve` honors it verbatim using the controller's cluster-wide read access to Flux sources. A tenant who can create a release in their own namespace can therefore make the controller read and render a private source owned by another tenant — a cross-namespace read straight through the namespace tenancy boundary the operator otherwise enforces (`checkDependsOn` rejects cross-namespace `dependsOn`; impersonation never resolves cross-namespace). This was flagged CRITICAL in the 2026-06 security audit. We close the hole with a default-deny, then add a consent-based opt-in so platform teams that genuinely need shared-source namespaces can enable it safely rather than reopening the hole with a blunt global switch.

## What Changes

- **Default-deny cross-namespace source resolution.** `source.Resolve` rejects any `sourceRef.namespace` that differs from the release namespace, surfacing the same typed-error / `Stalled` treatment as the existing cross-namespace `dependsOn` rejection. This is a **behavioral change** (previously silently permitted) but not an API change.
- **Policy seam.** Resolution consults a `CrossNamespacePolicy` decision point instead of hardcoding the rejection; the default wired implementation denies all cross-namespace references.
- **Admin master switch.** A controller flag (`--allow-cross-namespace-source-refs`, default `false`) gates the whole mechanism. Off ⇒ grants are inert and every cross-namespace reference is denied.
- **New OPM-native grant CRD: `SourceRefGrant`** (`releases.opmodel.dev`). Lives in the namespace being referenced (the data owner's namespace), declaring which referrer namespaces/kinds may reference which source kinds/names within it. Additive, fail-closed, modeled on Gateway API `ReferenceGrant` semantics but owned by OPM (no dependency on Gateway API being installed).
- **Grant-aware policy.** When the flag is on, a cross-namespace reference is permitted iff a `SourceRefGrant` in the target namespace matches both the referrer (`from`) and the referent (`to`). Two independent gates — admin flag AND data-owner grant — both default-deny.
- Controller gains cluster-wide read-only RBAC on `sourcerefgrants` and an informer to keep the lookup off the hot API path.

Implementation is sequenced so the security fix (default-deny) lands and is verifiable on its own, ahead of the opt-in grant machinery.

## Capabilities

### New Capabilities
- `cross-namespace-source-grants`: The `SourceRefGrant` CRD, the `--allow-cross-namespace-source-refs` master switch, and the grant-matching policy that permits an otherwise-denied cross-namespace source reference.

### Modified Capabilities
- `source-resolution`: `Resolve` gains a cross-namespace gate — references to a foreign namespace are denied unless a supplied `CrossNamespacePolicy` permits them; the default policy denies all.

## Impact

- **API**: new `api/v1alpha1/sourcerefgrant_types.go` (`SourceRefGrant`, namespaced); regenerated CRD + DeepCopy. MINOR — additive type plus a default-deny behavioral tightening on an existing field.
- **Code**: `internal/source/resolve.go` (gate + policy param), a new policy package (default-deny + grant-backed impls), `internal/reconcile/release.go` (`resolveReleaseSource` wiring + `Stalled` reason), `cmd/main.go` (flag, informer wiring, policy construction).
- **RBAC**: `+kubebuilder:rbac` for `sourcerefgrants` `get;list;watch`; regenerated `config/rbac/role.yaml`.
- **Docs**: `docs/TENANCY.md` gains a cross-namespace-source section.
- **Dependencies**: none added (deliberately not depending on `sigs.k8s.io/gateway-api`).
- **Compatibility**: existing same-namespace releases are unaffected; any deployment relying on the old implicit cross-namespace behavior must set the flag and author grants.
