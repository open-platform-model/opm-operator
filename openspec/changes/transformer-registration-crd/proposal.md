## Why

Enhancement 0015 D3 gives transformers a second path onto a platform: a cluster-scoped `TransformerRegistration` CR that a provider module ships among its rendered resources, gated by the RBAC the operator already enforces through tenant impersonation. `catalog_opm` published the authoring half in `opm 4.3.0` (D9): a `transformer-registration@v1alpha1` contract and a transformer that renders the CR with D12's instance-derived name and D11's stamped `providerRef`. Nothing in this repo knows that kind exists, so a provider module applying one today fails on an unknown kind.

`open-platform-model/opm-operator` issue 132 records what this side must match, and one thing it cannot delegate: `spec.catalog`, `spec.version` and `spec.provides` are required fields in the CUE contract, but CUE reports a missing required field as an *incomplete value*, not an error. Measured 2026-09-14 at cue v0.17.1: a component omitting `catalog` passes `cue vet ./...` and fails only under `cue export`. A claim can therefore reach the cluster with a field missing, so the CRD must carry its own `required` and validation rather than trusting the catalog.

This change lands the kind and its admission-level guarantees only. Acceptance, activation and regeneration are separate changes (see Scope below).

## What Changes

- `api/v1alpha1/transformerregistration_types.go` (new): `TransformerRegistration` and `TransformerRegistrationList`, **cluster-scoped**, with a status subresource. `Spec` carries `Catalog`, `Version`, `Provides` and `ProviderRef`, every one required at the CRD level. `Status` carries `Conditions`, plus `Accepted` and `Active` as the claim/accept split D3 defines. Hand-written `GetConditions`/`SetConditions` and a per-type `init()` scheme registration, matching the three existing types.
- CRD name validation: the rendered name is the dot-joined `<namespace>.<name>` of the provider instance (D12), so the CRD constrains `metadata.name` to that shape. Two instances of one provider module then produce two distinct claims and the second is refused later at acceptance naming the claimant, rather than two objects fighting over one name.
- `config/crd/bases/opmodel.dev_transformerregistrations.yaml` (generated) and its entry in `config/crd/kustomization.yaml`.
- `config/rbac/`: RBAC markers on the Platform controller for the new kind, and a **platform-admin ClusterRole that carries create on it**. Today this repo ships no tenant-versus-platform-admin split at all, so D3's gate rests on whatever RBAC a cluster admin happens to have written. Shipping the role makes the gate an artifact rather than an assumption; the tenant side stays absent, which is what denies create.
- `test/integration/crdvalidation/`: admission tests for cluster scope, the required fields, and the name shape, following the existing `skewpolicy_test.go` precedent.

**Not in this change**: acceptance (D3's three checks, D8, D10, D11), the activation health gate and finalizer (D3, D16), readiness exclusion (D14), regeneration keyed on the active-claim set (D13, D17), and `Platform.status.registry`. Nothing reconciles a `TransformerRegistration` when this change lands.

## Scope warning and the proposed split

🛑 **Scope Warning**: This request does not cut into a handful of mergeable sections. The operator's share of enhancement 0015 is ten decisions (D3, D8, D10 through D17) plus D18's operator half, which is four to five distinct subsystems. I suggest we split it into the following changes:

1. **`transformer-registration-crd`** (this change): the kind, its admission-level validation, and the RBAC that makes D3's gate real. Releasable alone; an unreconciled CRD is inert.
2. **`registration-acceptance`**: the reconciler's claim-to-accept path — catalog resolvable and of kind `Catalog` (D10), `provides` re-derived and compared for exact equality (D11), contract presence against the subscribed catalogs' maps and the one-provider rule (D3, D2), duplicate claims refused naming the claimant (D12), build-incompatible providers refused at admission rather than at render (D8), plus the finalizer and the shrink refusal (D16).
3. **`registration-driven-regeneration`**: `Platform.status.registry`, the watches that make regeneration edge-triggered (D13), the store re-keyed on package identity rather than generation alone (D17), and the readiness exclusion by kind (D14, D15).
4. **`surface-contract-inventory`** (independent of the other three): D18's operator half — read `Platform.Contracts()` after the platform builds, publish a non-gating `ContractsFulfilled` condition, and refuse generation when `Routable` is false.

Should we start with the first? This proposal describes change 1; the other three are not yet written.

## Impact

- **API types and controllers**: `api/v1alpha1/` gains one type. The Platform controller gains RBAC markers only; its `Reconcile` is untouched in this change.
- **Downstream consumers**: none in-cluster yet. No module renders a registration today, because no provider catalog exists; a claim can only arrive by hand until change 2 and a provider module land.
- **`catalog_opm`**: the group, version and kind literals in `opm/transformers/transformer_registration_transformer.cue` must equal the CRD's exactly. Both sides are `v1alpha1`, which promises nothing (0010 D34), so they can still move together.
- **SemVer**: MINOR. One new API type, no change to an existing one.
- **Complexity (Principle VII)**: the added surface is one CRD and one ClusterRole. The alternative D3 rejected, registering by annotation on a namespaced object, would put a cluster-scoped privilege on a tenant-writable field.

## Capabilities

### New Capabilities

- `transformer-registration-crd`: the cluster-scoped kind a provider module's rendered claim lands as, its required fields, its name shape, and the RBAC that governs who may create one.

### Modified Capabilities

None. The sibling conventions this type follows (per-type `init()` scheme registration, the generated manifest and its kustomization entry, cluster scope) are stated in the new capability rather than bolted onto `platform-crd`, whose "registered in the scheme without a reconciler" requirement predates the Platform controller and should not be rewritten as a side effect of this change.

## Impact on existing behaviour

None. Until change 2 lands, a `TransformerRegistration` applied to a cluster is stored and ignored: no controller watches it, no Platform status reflects it, and no render consults it.
