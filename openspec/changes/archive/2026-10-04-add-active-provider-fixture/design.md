## Context

A claim is accepted when the Platform reconciler's generated platform exists and these checks pass (`internal/controller/transformerregistration_controller.go`):

1. The named catalog resolves (`Kernel.AcquireCatalogFromRegistry(spec.catalog, spec.version)`).
2. `spec.provides` equals the catalog's derived provider contracts (`Catalog.Provides()`), exactly (0015:D11).
3. The claim is in the inventory of the ModuleInstance its `providerRef` names.
4. The catalog's `cue.mod` requirements are no newer than the platform's resolution (0015:D8).
5. No other claim holds the same catalog, no subscription or active claim already provides one of its contracts (0015:D12, 0015:D2).

It activates when that ModuleInstance reports Ready, and it latches (0015:D3). An active claim adds its catalog to the generated platform, so a module demanding the contract renders.

opm's `#TransformerRegistrationResource` and its transformer (opm 4.4.4 and later) render the claim: `catalog`, `version` and `provides` from the component, `providerRef` and the name `<namespace>.<instance>` from the rendering instance.

Reconcile phase impact: none. No operator code changes. The fixtures exercise Render (the claim and the consumer's ConfigMap), Apply (a cluster-scoped claim under an impersonated ServiceAccount) and Status (accepted, active, the Platform's `status.registry`).

## Goals / Non-Goals

**Goals:**

- One published fixture set that, applied to a cluster, yields an accepted and active claim and a Ready consumer of a provider-fulfilled contract.
- A test in this repo that fails when the set stops fitting together.

**Non-Goals:**

- Bumping the library in this change's own commits (D4: opm-operator#213 does it, and lands first).
- An e2e spec for acceptance and activation (D5).
- Changing `hack/fixtures.sh` or what the cascade moves. Only its test's golden list grows by the two backup modules, which the cascade already advances.

## Research & Decisions

### D1. A separate `backup` catalog fixture, and a literal claim

**Context**: The brief suggested extending the existing `provider` catalog fixture with the backup transformer, and building the claim with opm's `#PreBoundRegistration`, which derives `catalog`, `version` and `provides` from the provider catalog's own package.

**Explored**: Built that first. It renders the right claim. Then ran `hack/fixtures.sh check` with an empty CUE cache, as CI does: the provider module's dry run is refused because it imports `testing.opmodel.dev/catalogs/operator/provider` at 0.2.0, which GHCR does not hold yet. `check` dry-runs every fixture against GHCR before the tree is seeded (`test.yml`), so a module fixture cannot depend on a catalog fixture version that is new in the same PR. A warm local cache hides this: the first local run passed.

Read `.tasks/cascade/cascade.sh`: `task deps:cascade` advances the `provider` catalog's version whenever core moves. A module that pins it would then need the unpublished new build, the same gap on every cascade PR. A literal claim naming `provider` would name a build the PR's job-local registry no longer holds, and the registry-backed spec would fail.

**Decision**: The backup provider is a catalog fixture of its own, `testing.opmodel.dev/catalogs/operator/backup@v0`, which the cascade does not touch. `backup_provider` authors its claim with opm's `#TransformerRegistration`, `catalog` and `version` as literals and `provides` as opm's backup trait FQN. It depends on core and the opm catalog only.

This deviates from 0015:D11:R1 (no field of a rendered registration is authored). The fixture README and the spec requirement say so, so nobody copies it as a provider pattern.

**Rationale**: The literal is checked twice. Acceptance re-derives `provides` from the named catalog and refuses a difference (0015:D11). The integration spec (D3) fails when the literal drifts from the catalog fixture's identity package or its transformers, so a hand bump of the catalog must re-pin the claim in the same PR, which Registry Policy rule 3 asks for anyway. The backup catalog's core and opm pins can only trail a platform's, and the build-compatibility check (0015:D8) refuses only a newer requirement, so leaving it off the cascade is safe. The cost: the fixture does not demonstrate `#PreBoundRegistration`. That helper has its own golden fixtures in the opm catalog.

### D2. What the consumer renders

**Context**: opm's backup trait applies only to a component with volumes (`appliesTo: [#VolumesResource]`).

**Decision**: `backup_consumer` has one component with a single `emptyDir` volume and the backup trait. opm's PVC transformer matches the volume and emits nothing for an `emptyDir`. The backup transformer emits one ConfigMap, `<instance>-data-backup`, holding the schedule and the retention as JSON.

**Rationale**: No PVC, no storage class and no image pull, so the instance is Ready on any cluster within seconds, and the one object it owns is the provider's output.

### D3. A registry-backed integration spec, not an envtest acceptance spec

**Context**: The claim checks that can break in this repo are the ones the fixtures control: what the claim names and lists, and whether the consumer renders through the provider.

**Decision**: `test/integration/reconcile/backup_fixture_test.go`, in the registry-backed tier, against the job-local registry in PR CI:

1. Render `backup_provider` through `KernelModuleRenderer`. It renders exactly one `TransformerRegistration`, named `default.backup-provider`. Its `catalog` and `version` equal the backup catalog fixture's identity (`fixtures.MustCatalog`), its `providerRef` names the instance, and its `provides` equals `Catalog.Provides()` of the acquired catalog, which is exactly opm's backup trait.
2. Render `backup_consumer` against the platform subscribed to opm only. The render is refused, naming the backup trait.
3. Render `backup_consumer` against a platform with the backup catalog beside opm. It renders one ConfigMap, produced by the backup catalog's transformer, and its demand lists the backup trait.

The platform store helper gains `generatedPlatformStoreWith(k, registry, extra...)` for case 3.

**Rationale**: Running the `TransformerRegistrationReconciler` against the real Kernel would fail at check 1 on library v1.0.0-beta.1 (D4). The first spec resolves the catalog with the claim's own `catalog` and bare `version`, the inputs acceptance passes, so it fails on that library too. The integration spec proves every fact the fixtures are responsible for, and the cluster run (D4) proves the rest.

### D4. Released operators refuse the rendered claim

**Context**: The capture saw `CatalogUnresolved` for a hand-applied claim with a bare version, and opm-operator#210 calls the CRD doc comment wrong.

**Explored**: On a throwaway podman kind cluster (k8s v1.36.1), with the fixtures seeded into a local registry mapped for `testing.opmodel.dev` only:

- Operator v1.0.0-beta.5 (released, library v1.0.0-beta.1): `backup-provider` Ready, claim `CatalogUnresolved`: `version "0.1.0" ... is not well formed`. `backup-consumer` not Ready, render refused naming the backup trait.
- The same cluster with the operator image rebuilt from `main` (40a2345) on library v1.0.0-beta.4 (it builds and vets unchanged): claim accepted and active within one reconcile, `Platform.status.registry` lists the backup catalog with `source: Registration`, `backup-consumer` Ready with its ConfigMap. Deleting the claim while the consumer existed was blocked (`Stalled=True/DependentsRemain`, `Active` stayed True), and removing the consumer released it.

opm's `#VersionType` is a bare SemVer, so every rendered claim carries one. Library v1.0.0-beta.1 passes the version to `module.NewVersion` unchanged. Library v1.0.0-beta.2 (library#170) canonicalises it first.

The captured YAML for both runs and for the blocked deletion is on opm-operator#212 (issuecomment-5981329966).

**Decision**: Ship the fixtures as they are, after the library bump. The bump is its own `fix(deps)` pull request, opm-operator#213, so it releases the operator under its own CHANGELOG entry; it merges before this change and before release PR #208, so the release that first carries these manifests in its examples bundle accepts the claim. Until #213 merges, this branch carries its commit, which drops out on a rebase onto `main`. Once it lands, opm-operator#210 reduces to the doc comment.

**Rationale**: The fixtures are correct for the contract as opm renders it. Making them pass on beta.5 would need a `v`-prefixed version, which opm's `#VersionType` refuses.

### D5. No e2e spec yet

**Context**: `test/e2e` can apply both manifests to a kind-backed operator built from the branch.

**Decision**: Not in this change. Without library v1.0.0-beta.2 or later the spec would fail (D4), and the spec belongs after that bump is released.

**Rationale**: After the library bump, an e2e spec that applies `backup_provider` and `backup_consumer` and waits for accepted, active and Ready is a small follow-up.

## Risks / Trade-offs

- **The literal claim can drift** from the backup catalog. Mitigated by the integration spec (D3) and by acceptance (0015:D11).
- **The backup catalog's pins trail** core and opm. Harmless under 0015:D8 until a core major moves; a hand bump then fixes it.
- **The ClusterRole in `backup_provider/moduleinstance.yaml`** grants write on every `TransformerRegistration` to one ServiceAccount. That is the point of the fixture (0015:D3:R2), but it is a cluster-wide grant in a sample. The manifest says so.
