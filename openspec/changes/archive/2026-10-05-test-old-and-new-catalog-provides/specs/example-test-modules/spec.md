## MODIFIED Requirements

### Requirement: A provider catalog fixture implements a provider-fulfilled contract

The repo SHALL carry a catalog fixture under `test/fixtures/catalogs/backup`, declaring `testing.opmodel.dev/catalogs/operator/backup@v0`, whose transformers implement opm's provider-fulfilled backup trait (`opmodel.dev/catalogs/opm/traits/backup@v1alpha1`) and nothing else, so the provider contracts derived from it are exactly that trait. It SHALL render the trait without any CRD, publish through `hack/fixtures.sh` like the provider catalog fixture, and pin core and the opm catalog no newer than the build the registry-backed specs subscribe. `task deps:cascade` SHALL NOT be required to move it. While the library keeps its deprecated provider-set fold, its core pin SHALL stay older than the library's `ProvidesSince`, since the operator's old-catalog acceptance spec relies on it.

#### Scenario: The catalog passes the catalog publish gates

- **WHEN** `opm catalog publish --dry-run test/fixtures/catalogs/backup` runs against GHCR
- **THEN** the plan resolves with no refusals, checking one member through the member FQN gate

#### Scenario: The catalog provides exactly the backup trait

- **WHEN** the backup catalog fixture is acquired from the registry at its declared build and its provider contracts are derived
- **THEN** they are exactly `opmodel.dev/catalogs/opm/traits/backup@v1alpha1`
