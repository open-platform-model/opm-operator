## Purpose

Define the local OCI registry lifecycle used by integration and e2e tests that
exercise the real `KernelModuleRenderer`. This keeps CUE-native module
resolution testable in developer and CI environments without depending on the
public `ghcr.io/open-platform-model` registry, while letting stub-based specs
run in minimal environments that lack a container runtime.

## Requirements

### Requirement: Local OCI registry for e2e tests

A local OCI registry SHALL be available for flows that iterate on an **unpublished** fixture. It SHALL NOT be a prerequisite of the ordinary test tiers: the fixtures are published to GHCR, so unit, integration, and e2e runs resolve them from a public registry with nothing running locally.

The registry lifecycle SHALL remain operable through the Taskfile (start, connect, publish), and specs that require it SHALL opt in through the guard helper rather than assuming it.

#### Scenario: Operator starts registry via Taskfile

- **WHEN** a developer needs a local registry for an unpublished fixture
- **THEN** the Taskfile starts a container named `opm-registry` publishing port 5000

#### Scenario: Integration tier needs no local registry

- **WHEN** the integration tier runs with only the canonical GHCR mapping configured and no registry container running
- **THEN** the registry-backed specs SHALL execute rather than skip

### Requirement: CUE_REGISTRY configuration for tests

Tests SHALL resolve the fixture modules from whatever registry the configured mapping routes `testing.opmodel.dev` to, and their core and catalog dependencies from wherever it routes `opmodel.dev`. The canonical mapping routes both to `ghcr.io/open-platform-model`.

Because the fixtures and their dependencies now occupy SEPARATE domains, a local-iteration mapping SHALL redirect only the fixture prefix — `testing.opmodel.dev=localhost:5000+insecure` — while core and the catalogs continue to resolve from GHCR. Mapping all of `opmodel.dev` to a local registry SHALL NOT be required, and mirroring core or the catalogs locally SHALL NOT be required.

Tooling that invokes the publish CLI SHALL export `OPM_REGISTRY` as well as `CUE_REGISTRY`: the CLI resolves `--registry` > `OPM_REGISTRY` > its config file and never reads `CUE_REGISTRY`, so exporting only the latter silently leaves it on the caller's personal configuration.

#### Scenario: Fixture prefix redirected alone

- **WHEN** `CUE_REGISTRY` maps `testing.opmodel.dev` to the local registry and `opmodel.dev` to GHCR
- **THEN** the fixture resolves locally and core and the catalogs resolve from GHCR

#### Scenario: Catalog present enables kernel materialization

- **WHEN** the configured mapping resolves the subscribed catalog build
- **THEN** kernel materialization proceeds

### Requirement: Test module publication

A fixture module MUST be publishable to a registry as `testing.opmodel.dev/modules/operator/<name>@v0` at the version its own identity package declares. The publish task SHALL force its registry mapping in-script so an ambient environment cannot redirect it, and SHALL derive the version from the identity package rather than accepting it as a parameter.

#### Scenario: Publish task uses task-local registry mapping

- **WHEN** the publish task runs with an unrelated mapping exported in the shell
- **THEN** it publishes against its own forced mapping

#### Scenario: Fixture published at its declared coordinates

- **WHEN** the publish task completes for `hello`
- **THEN** `testing.opmodel.dev/modules/operator/hello@v0` is available at the version its identity package declares

#### Scenario: Stale old-era artifact is inert

- **WHEN** an artifact published under a retired path remains in a registry
- **THEN** no test resolves it

### Requirement: Test fixture module path

The fixture modules MUST use module paths `testing.opmodel.dev/modules/operator/<name>@v0`, and each `#Module.metadata.modulePath` MUST equal that full major-suffixed address, derived from the module's identity package.

#### Scenario: Fixture published under the fixture namespace

- **WHEN** a fixture is resolved by a consumer
- **THEN** it resolves under the `testing.opmodel.dev` domain, which is reserved for fixtures and experiments and is routed to GHCR by the canonical mapping

#### Scenario: Fixture is concrete-valid standalone

- **WHEN** a fixture module is evaluated on its own
- **THEN** it evaluates without error and its identity package yields a concrete version

### Requirement: End-to-end integration tests
At least one integration test MUST exercise the real renderer
(`render.KernelModuleRenderer`) against the local OCI registry, materializing a
platform from the real catalog, to validate the registry-backed render pipeline:
module acquisition → kernel `SynthesizeRelease` → `Compile` → rendered resources
with inventory entries. The test MUST resolve the catalog from the materialized
platform (the same path the `PlatformReconciler` uses) rather than copying
catalog sources into `test/fixtures/`, so it tracks production composition
automatically. Full apply → `Ready=True` on a live cluster is covered by the
Kind-backed `test/e2e` suite, not this integration-tier test.

#### Scenario: Real-renderer pipeline validated against the registry
- **WHEN** the integration test runs with the local registry available
- **THEN** it constructs `render.KernelModuleRenderer` with a kernel-materialized platform, renders a ModuleRelease, and the rendered resources carry inventory entries and the runtime-identity labels (`managed-by = opm-controller`, non-empty release uuid)

#### Scenario: Catalog resolved from the materialized platform
- **WHEN** the integration test materializes the platform
- **THEN** the catalog is resolved from the registry via the kernel rather than a copy under `test/fixtures/`, so the test automatically tracks production composition

### Requirement: Skip when registry unavailable

Registry-backed specs SHALL skip only when NO registry mapping is configured, or when a configured localhost mapping has no container tool available to validate it. They SHALL NOT skip merely because the configured mapping points at a remote registry — the fixtures are published, so a remote mapping is the ordinary case.

Under `OPM_TEST_REGISTRY_FORCE=1` a missing prerequisite SHALL fail instead of skipping.

#### Scenario: No mapping configured

- **WHEN** `CUE_REGISTRY` is unset or maps no `opmodel.dev` domain
- **THEN** the registry-backed specs skip with a message naming the canonical mapping

#### Scenario: Remote mapping runs the specs

- **WHEN** `CUE_REGISTRY` maps the domains to GHCR and no local registry is running
- **THEN** the registry-backed specs execute

#### Scenario: Forced mode fails instead of skipping

- **WHEN** `OPM_TEST_REGISTRY_FORCE=1` and a prerequisite is missing
- **THEN** the spec fails, naming the missing prerequisite
