## Why

No published artifact in the workspace drives a `TransformerRegistration` to accepted and active. The provider catalog fixture (`testing.opmodel.dev/catalogs/operator/provider`) implements no provider-fulfilled contract, so a claim naming it is refused `ProvidesMismatch`, and no module renders a claim at all. The live capture for the OPM portal (2026-10-04) had to hand-apply claims, and every one was refused. The portal's V1 needs a real accepted, active claim and a real consumer of a provider contract to design its views against. The operator itself has no fixture that exercises 0015:D3 and 0015:D9 end to end on a cluster.

`opmodel.dev/catalogs/opm/traits/backup@v1alpha1` is the right contract to provide. The opm catalog declares it with `fulfilment: "provider"` and ships no transformer for it, and nothing else in the workspace implements it, so a fixture claim on it never meets a second provider (0015:D2).

## What Changes

- **A provider catalog fixture, `backup`.** `test/fixtures/catalogs/backup` declares `testing.opmodel.dev/catalogs/operator/backup@v0` at 0.1.0. Its one transformer implements opm's backup trait and renders the policy as one ConfigMap, so it needs no CRD on any cluster.
- **A provider module fixture, `backup_provider`.** Its one component is opm's `#TransformerRegistration`, naming the backup catalog at 0.1.0 and providing the backup trait. Its `moduleinstance.yaml` binds a ClusterRole that may write `transformerregistrations`, the platform-team identity 0015:D3 requires.
- **A consumer module fixture, `backup_consumer`.** Its one component carries a volume and opm's backup trait. With no active provider its render is refused naming the contract (0010:D28). With the backup catalog in the registry it renders one ConfigMap through the backup catalog's transformer.
- **A registry-backed integration spec** holds the set together: the rendered claim names the backup catalog at its declared build and lists exactly what that catalog implements (the facts acceptance re-derives, 0015:D11); the consumer is refused without the provider and renders with it.
- **Docs:** the fixture README table and the `AGENTS.md` fixture paragraph name the new fixtures.

The provider catalog fixture is unchanged. Design D1 says why the backup provider is a catalog of its own.

## Classification

No product code changes: no API type, controller, flag or `dist/install.yaml` content. The fixtures and the spec are test-only, so the change ships under a `test(fixtures)` title and releases nothing (`AGENTS.md`, "Commit type decides the release"). After GA this would also be no release.

## Depends on / gates

- **Gates:** `task dev:fmt dev:vet dev:lint dev:test`, `task examples:check` (`hack/fixtures.sh check`). No API type changes, so `task dev:manifests dev:generate` and `task docs:bundle:check` do not apply beyond the standard run.
- **Released operators refuse the rendered claim.** Measured on v1.0.0-beta.5: `CatalogUnresolved`, because library v1.0.0-beta.1 does not parse the bare SemVer that opm's transformer renders in `spec.version` (opm-operator#210). Library v1.0.0-beta.2 and later accept it (library#170). The fixture set is correct today and reaches accepted and active on an operator built with library v1.0.0-beta.4 (design D4). The library bump is the cascade's to make; this change does not take it.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `example-test-modules`:
  - Fleet composition adds `backup_provider` and `backup_consumer`.
  - Modulepackage parity covers the workload fleet only; the backup pair has none.
  - A new requirement: the backup fixture set exercises a registration end to end.
  - A new requirement: the backup catalog fixture.

## Impact

- New: `test/fixtures/catalogs/backup/`, `test/fixtures/modules/backup_provider/`, `test/fixtures/modules/backup_consumer/`, `test/integration/reconcile/backup_fixture_test.go`.
- `test/integration/reconcile/registry_helpers_test.go`: the platform store helper accepts further registry entries.
- `test/fixtures/modules/README.md`, `AGENTS.md`.
- `.tasks/cascade/test.sh`: the S2 golden list names the two backup modules, which advance on every cascade like the rest of the fleet.
- Publishing: on merge, `publish-fixtures.yml` publishes the three new coordinates to GHCR. `task examples:bundle` picks up the two new `moduleinstance.yaml` files as release assets.
- Downstream: the opm-portal capture uses the published set (`backup_provider`, then `backup_consumer`) once an operator release carries library v1.0.0-beta.2 or later.
- No enhancement decision is implemented here, so there is no `enhancement.yaml`.
