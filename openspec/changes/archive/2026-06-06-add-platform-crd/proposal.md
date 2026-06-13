## Why

With the library kernel wired in (`wire-library-kernel`), the operator can now `Materialize` a `#Platform` — but it has **no source for one**. Today the platform/provider arrives as a CUE composition loaded from a flag (`--catalog-path`), the deleted `#defines`-era model. Enhancement 0001 §8 decided the replacement: a cluster-scoped singleton `Platform` CRD, platform-admin owned, whose spec is a near-1:1 projection of core `#Platform`. This change introduces just the **CRD types** — the typed source that the PlatformReconciler (next slice) reads and feeds to `SynthesizePlatform → Materialize`.

## What Changes

- New API type `Platform` in `api/v1alpha1/` — **cluster-scoped** (`+kubebuilder:resource:scope=Cluster`), a **singleton** enforced declaratively by a CEL root rule `self.metadata.name == 'cluster'` (`+kubebuilder:validation:XValidation`). The repo has no prior CEL/singleton CRD; this is the first.
- `PlatformSpec` projects core `#Platform`:
  - `type` (string, required) → `#Platform.type` (informational discriminator; matcher does not consult it).
  - `registry` — path-keyed map `map[modulePath]Subscription` → `#Platform.#registry`. `Subscription` = `{ enable *bool (omitted ⇒ schema default true), filter? { range?, allow? []string, deny? []string } }`, mirroring core `#Subscription` / `#SubscriptionFilter`. Shapes map 1:1 onto `synth.PlatformInput.Subscriptions{Enable, Filter{Range,Allow,Deny}}`, so the reconciler slice converts spec→input without translation friction.
- `PlatformStatus` — `conditions []metav1.Condition` (incl. a `Materialized` condition the reconciler will set), `observedGeneration`. Conditions getter/setter mirroring existing CRDs.
- Generated artifacts: `zz_generated.deepcopy.go` (Platform/List/Spec/Status/Subscription/Filter), `config/crd/bases/...platforms.yaml`, RBAC for the new kind, a `config/samples` `Platform` named `cluster`.
- Register `Platform`/`PlatformList` in the scheme (`groupversion_info.go` SchemeBuilder).

**Out of scope (next slices):** the `PlatformReconciler`, any `SynthesizePlatform`/`Materialize` call, the single-slot materialize cache, release gating on platform readiness, and the render-core rewrite. No reconciler is registered for `Platform` in this change.

## Capabilities

### New Capabilities

- `platform-crd`: a cluster-scoped singleton `Platform` custom resource (name pinned to `cluster` by CEL) whose spec projects core `#Platform` (`type` + path-keyed `registry` subscriptions with optional SemVer filters) and whose status carries conditions + observedGeneration — the typed, admin-owned source a later reconciler materializes.

### Modified Capabilities

None — additive new kind. No existing CRD or capability's requirements change. The legacy `catalog-provider-loading` path is untouched this slice.

## Impact

- **APIs/CRDs**: new `api/v1alpha1/platform_types.go`; new cluster-scoped CRD `platforms.releases.opmodel.dev`; new RBAC. Existing `ModuleRelease`/`BundleRelease`/`Release` CRDs unchanged.
- **Code**: `api/v1alpha1/platform_types.go`, `api/v1alpha1/groupversion_info.go` (scheme registration), generated deepcopy, `config/crd`, `config/rbac`, `config/samples`.
- **Controllers**: none registered this slice (types only).
- **Enhancement**: implements the lower layer of 0001 §8.5; provides the platform the render-core rewrite later consumes. `synth.PlatformInput`/`#Platform` are the projection targets.
- **SemVer**: MINOR — additive new API kind; no change to existing types or behavior.
- **Complexity justification (Principle VII)**: the CEL singleton rule is the minimum mechanism that makes "one global Platform per cluster" (§8.1) structurally true without a webhook. Spec mirrors the schema exactly — no operator-invented fields.
