## MODIFIED Requirements

### Requirement: ModulePackage fixture parity for example modules

Each example module of the workload fleet (`hello`, `hello_web`, `podinfo`, `redis`) SHALL have a sibling modulepackage fixture declaring `testing.opmodel.dev/releases/operator/<module>@v0`, an `instance.cue` that imports the published module, and a `cue.mod/module.cue` that pins the same `testing.opmodel.dev/modules/operator/<module>@v0` version the module declares. The backup fixture set (`backup_provider`, `backup_consumer`) has no modulepackage fixture: it exists to exercise a registration, and `backup_consumer` does not render on a platform without an active provider.

#### Scenario: Each module has a modulepackage fixture

- **WHEN** the modulepackage fixtures are enumerated
- **THEN** there SHALL be one per workload fleet module, declaring `testing.opmodel.dev/releases/operator/<module>@v0`

#### Scenario: instance.cue imports and embeds the published module

- **WHEN** a modulepackage fixture is loaded
- **THEN** it embeds `core.#ModuleInstance`, imports `testing.opmodel.dev/modules/operator/<module>@v0`, and sets `#module` to the imported module

#### Scenario: ModulePackage CR references its OCIRepository

- **WHEN** a modulepackage fixture's `ModulePackage` is inspected
- **THEN** its `spec.sourceRef` names the `OCIRepository` declared in the sibling `ocirepository.yaml`, whose `url` ends in `testing.opmodel.dev/releases/operator/<module>`

### Requirement: Fleet composition and identity shape

The example test module fleet SHALL be the workload fleet `hello`, `hello_web`, `podinfo` and `redis`, plus the backup fixture set `backup_provider` and `backup_consumer`. Each SHALL carry an `identity/identity.cue` package as the single source of its module path and version (core `#IdentityPackage`), and its `#Module.metadata` SHALL DERIVE from that package rather than restate it: `metadata.modulePath` is the identity package's `ModulePath`, `metadata.version` its `Version`, and `metadata.name` the path's leaf in snake case. Catalog imports stay on the versioned packages of `opmodel.dev/catalogs/opm@v4`. Each module's `moduleinstance.yaml` SHALL pin the module's current published `v`-prefixed version.

A version bump SHALL therefore be an edit to `identity/identity.cue` (directly or via `opm module version set`), never to the metadata block.

The hyphenated `hello-web` name is retired at the source: a core-v2 module cannot carry a hyphen, so the fixture publishes as `hello_web`. Artifacts previously published under `opmodel.dev/modules/test/*`, including the hyphenated `hello-web`, are unmodified by this change; their deletion is owned by enhancement 0011's `registry-cleanup`.

#### Scenario: Fleet renders on the v2 line

- **WHEN** each workload fleet member is loaded and rendered
- **THEN** it renders against `opmodel.dev/core@v2` and the versioned catalog packages without error

#### Scenario: Metadata derives from identity

- **WHEN** a fixture's `identity/identity.cue` declares a path and version
- **THEN** its `metadata.modulePath`, `metadata.version`, and `metadata.name` evaluate to values derived from that package
- **AND** `opm module publish` reports no derivation disagreement

#### Scenario: Hyphenated name absent from the fleet

- **WHEN** the fleet's module paths are enumerated
- **THEN** none contains a hyphen in its leaf segment

## ADDED Requirements

### Requirement: A provider catalog fixture implements a provider-fulfilled contract

The repo SHALL carry a catalog fixture under `test/fixtures/catalogs/backup`, declaring `testing.opmodel.dev/catalogs/operator/backup@v0`, whose transformers implement opm's provider-fulfilled backup trait (`opmodel.dev/catalogs/opm/traits/backup@v1alpha1`) and nothing else, so the provider contracts derived from it are exactly that trait. It SHALL render the trait without any CRD, publish through `hack/fixtures.sh` like the provider catalog fixture, and pin core and the opm catalog no newer than the build the registry-backed specs subscribe. `task deps:cascade` SHALL NOT be required to move it.

#### Scenario: The catalog passes the catalog publish gates

- **WHEN** `opm catalog publish --dry-run test/fixtures/catalogs/backup` runs against GHCR
- **THEN** the plan resolves with no refusals, checking one member through the member FQN gate

#### Scenario: The catalog provides exactly the backup trait

- **WHEN** the backup catalog fixture is acquired from the registry at its declared build and its provider contracts are derived
- **THEN** they are exactly `opmodel.dev/catalogs/opm/traits/backup@v1alpha1`

### Requirement: The backup fixture set exercises a registration end to end

`backup_provider` SHALL render exactly one `TransformerRegistration`, through opm's `transformer-registration` contract, that names the backup catalog fixture at the version its identity package declares and lists exactly the provider contracts that catalog implements, so a cluster running an operator that resolves the catalog accepts it (0015:D3, 0015:D11). The claim's `catalog`, `version` and `provides` are authored literals, a deliberate deviation from 0015:D11:R1: a module fixture cannot import a catalog fixture version that is new in the same pull request, so a derived claim could not land with a `backup` catalog bump. Its `moduleinstance.yaml` SHALL apply it under a ServiceAccount bound to a ClusterRole that may write `transformerregistrations`, since only a platform-team identity may register a provider (0015:D3:R2). `backup_consumer` SHALL attach opm's backup trait, and its render SHALL be refused naming that contract while no provider of it is in the platform's registry (0010:D28), and SHALL succeed with the backup catalog in the registry, rendering through the backup catalog's transformer. A registry-backed integration spec SHALL check all three.

#### Scenario: The rendered claim fits the backup catalog

- **WHEN** `backup_provider` is rendered as instance `backup-provider` in namespace `default`
- **THEN** the output is one `TransformerRegistration` named `default.backup-provider`
- **AND** its `spec.catalog` and `spec.version` equal the backup catalog fixture's `ModulePath` and `Version`
- **AND** the catalog resolves from the registry with the claim's own `spec.catalog` and bare `spec.version`, as acceptance resolves it
- **AND** its `spec.provides` equals the provider contracts derived from that catalog build
- **AND** its `spec.providerRef` names `default/backup-provider`

#### Scenario: The consumer is refused without a provider

- **WHEN** `backup_consumer` is rendered against a platform that subscribes only `opmodel.dev/catalogs/opm@v4`
- **THEN** the render is refused, naming `opmodel.dev/catalogs/opm/traits/backup@v1alpha1`

#### Scenario: The consumer renders through the provider

- **WHEN** `backup_consumer` is rendered against a platform whose registry also holds the backup catalog fixture
- **THEN** it renders one ConfigMap, produced by the backup catalog's transformer
- **AND** the instance's required contracts include `opmodel.dev/catalogs/opm/traits/backup@v1alpha1`

#### Scenario: A cluster accepts and activates the claim

- **WHEN** both `moduleinstance.yaml` files are applied to a cluster whose operator resolves the published fixtures and accepts a bare SemVer in `spec.version`
- **THEN** the claim `default.backup-provider` reports `accepted: true` and, once `backup-provider` is Ready, `active: true`
- **AND** `backup-consumer` reaches Ready
