# example-test-modules Specification

## Purpose
TBD - created by archiving change add-example-test-modules. Update Purpose after archive.
## Requirements
### Requirement: Public example module path

All example test modules SHALL be authored under the CUE module path `testing.opmodel.dev/modules/operator/<module>@v0`, where `<module>` is the module's short name. Each module's `cue.mod/module.cue` `module:` field and its `#Module.metadata.modulePath` SHALL agree with this path, and SHALL be published to `ghcr.io/open-platform-model` so the module resolves under the canonical registry mapping, which routes both `opmodel.dev` (core, catalogs) and `testing.opmodel.dev` (these fixtures) to GHCR.

Fixtures SHALL NOT be authored under `opmodel.dev/*`. CUE resolves modules by longest-prefix match on the module path, so a fixture in the production namespace forces that entire prefix — core and the catalogs included — onto whatever registry serves the fixture. The publish gates enforce this independently: a nested path under `opmodel.dev` is refused.

#### Scenario: New module declares public path

- **WHEN** a new example module is added
- **THEN** its `cue.mod/module.cue` declares `module: "testing.opmodel.dev/modules/operator/podinfo@v0"` and `metadata.modulePath` resolves to the same value

#### Scenario: Consumer resolves without extra config

- **WHEN** a user with the canonical `CUE_REGISTRY` mapping resolves `testing.opmodel.dev/modules/operator/podinfo@v0`
- **THEN** it resolves from `ghcr.io/open-platform-model` with no additional registry configuration

#### Scenario: Production namespace is refused

- **WHEN** a fixture declares a path under `opmodel.dev/modules/test/`
- **THEN** `opm module publish` refuses it as not fitting the namespace the domain publishes

### Requirement: podinfo web example module

The repo SHALL provide a podinfo example module modelling a stateless web workload. It SHALL render a Deployment and a Service exposing the podinfo HTTP port (9898), and SHALL declare an HTTP `livenessProbe` against `/healthz` and an HTTP `readinessProbe` against `/readyz` on that port.

#### Scenario: Renders deployment with probes
- **WHEN** the podinfo module is compiled and materialized
- **THEN** the output includes a Deployment whose container declares a `livenessProbe.httpGet` path `/healthz` and a `readinessProbe.httpGet` path `/readyz`, both on port 9898
- **AND** the output includes a Service targeting port 9898

#### Scenario: Configurable replicas and image
- **WHEN** a ModuleRelease overrides the podinfo replica count or image tag
- **THEN** the rendered Deployment reflects the overridden values

### Requirement: redis stateful example module

The repo SHALL provide a redis example module modelling a stateful workload. It SHALL render a StatefulSet, a headless Service, and a PersistentVolumeClaim (or volumeClaimTemplate), and SHALL declare an exec readiness probe running `redis-cli ping`.

#### Scenario: Renders statefulset with persistence and probe
- **WHEN** the redis module is compiled and materialized
- **THEN** the output includes a StatefulSet with a volume claim and a headless Service
- **AND** the container declares an exec readiness probe invoking `redis-cli ping`

#### Scenario: Persistence default is documented and overridable
- **WHEN** the redis module is authored
- **THEN** its persistence behavior (ephemeral vs PVC) has an explicit documented default and is overridable via module config

### Requirement: Example modules ship ready-to-apply manifests

Each example module SHALL include a `ModuleInstance` manifest (and, where applicable, `ModulePackage`/`OCIRepository` manifests) that a user can apply against a running operator to deploy the example. The manifests SHALL reference the `testing.opmodel.dev/modules/operator/<m>@v0` path and a concrete version.

#### Scenario: Manifest references public module

- **WHEN** a user inspects an example module's `moduleinstance.yaml`
- **THEN** its `spec.module.path` is `testing.opmodel.dev/modules/operator/podinfo@v0` with an explicit `spec.module.version`

### Requirement: podinfo liveness/readiness e2e validation

An e2e test SHALL deploy the podinfo `ModuleRelease` against a Kind-backed operator and assert that the rendered probes function — the Deployment's pods become Ready, which requires both the liveness and readiness probes to pass.

#### Scenario: podinfo pods become ready
- **WHEN** the e2e suite applies the podinfo ModuleRelease and the controller reconciles it
- **THEN** the resulting Deployment's pods SHALL reach Ready within the test timeout, demonstrating the modelled probes succeed against the running container

#### Scenario: Rendered probe contract matches container
- **WHEN** the e2e suite inspects the deployed podinfo container
- **THEN** the container's liveness/readiness probe HTTP paths and port match the values declared in the module (`/healthz`, `/readyz`, 9898)

### Requirement: ModulePackage fixture parity for example modules

Each example module SHALL have a sibling modulepackage fixture declaring `testing.opmodel.dev/releases/operator/<module>@v0`, an `instance.cue` that imports the published module, and a `cue.mod/module.cue` that pins the same `testing.opmodel.dev/modules/operator/<module>@v0` version the module declares.

#### Scenario: Each module has a modulepackage fixture

- **WHEN** the modulepackage fixtures are enumerated
- **THEN** there SHALL be one per example module, declaring `testing.opmodel.dev/releases/operator/<module>@v0`

#### Scenario: instance.cue imports and embeds the published module

- **WHEN** a modulepackage fixture is loaded
- **THEN** it embeds `core.#ModuleInstance`, imports `testing.opmodel.dev/modules/operator/<module>@v0`, and sets `#module` to the imported module

#### Scenario: ModulePackage CR references its OCIRepository

- **WHEN** a modulepackage fixture's `ModulePackage` is inspected
- **THEN** its `spec.sourceRef` names the `OCIRepository` declared in the sibling `ocirepository.yaml`, whose `url` ends in `testing.opmodel.dev/releases/operator/<module>`

### Requirement: hello_web ready-to-apply ModuleInstance

The `hello_web` example SHALL ship a `ModuleInstance` manifest referencing the `testing.opmodel.dev/modules/operator/hello_web@v0` path and a concrete version, so all four fleet members are applyable.

#### Scenario: hello_web manifest references public module

- **WHEN** a user inspects `hello_web`'s `moduleinstance.yaml`
- **THEN** its `spec.module.path` is `testing.opmodel.dev/modules/operator/hello_web@v0` with an explicit `spec.module.version`

### Requirement: ModulePackage render integration coverage

The `KernelPackageRenderer` integration test SHALL exercise every modulepackage fixture
(`hello`, `hello_web`, `podinfo`, `redis`), not only `hello`, asserting that each authored
package loads its imported `#Module` and renders at least one resource carrying the
controller's ownership labels. The coverage SHALL remain gated on the local test registry
(skipping when it is unavailable).

#### Scenario: Every modulepackage renders under a materialized platform
- **WHEN** the integration suite runs with a reachable test registry and a materialized platform
- **THEN** each of the `hello`, `hello_web`, `podinfo`, `redis` modulepackage fixtures loads without a "field not allowed" error and renders at least one resource
- **AND** each rendered resource carries the `managed-by` controller label and a non-empty module-instance UUID label

#### Scenario: Coverage skips without the test registry
- **WHEN** the integration suite runs without the local test registry configured
- **THEN** the modulepackage render cases skip rather than fail

### Requirement: Fleet composition and identity shape

The example test module fleet SHALL be `hello`, `hello_web`, `podinfo`, and `redis`. Each SHALL carry an `identity/identity.cue` package as the single source of its module path and version (core `#IdentityPackage`), and its `#Module.metadata` SHALL DERIVE from that package rather than restate it: `metadata.modulePath` is the identity package's `ModulePath`, `metadata.version` its `Version`, and `metadata.name` the path's leaf in snake case. Catalog imports stay on the versioned `v1beta1` packages of `opmodel.dev/catalogs/opm@v2`. Each module's `moduleinstance.yaml` SHALL pin the module's current published `v`-prefixed version.

A version bump SHALL therefore be an edit to `identity/identity.cue` (directly or via `opm module version set`), never to the metadata block.

The hyphenated `hello-web` name is retired at the source: a core-v2 module cannot carry a hyphen, so the fixture publishes as `hello_web`. Artifacts previously published under `opmodel.dev/modules/test/*`, including the hyphenated `hello-web`, are unmodified by this change; their deletion is owned by enhancement 0011's `registry-cleanup`.

#### Scenario: Fleet renders on the v2 line

- **WHEN** each fleet member is loaded and rendered
- **THEN** it renders against `opmodel.dev/core@v2` and the versioned catalog packages without error

#### Scenario: Metadata derives from identity

- **WHEN** a fixture's `identity/identity.cue` declares a path and version
- **THEN** its `metadata.modulePath`, `metadata.version`, and `metadata.name` evaluate to values derived from that package
- **AND** `opm module publish` reports no derivation disagreement

#### Scenario: Hyphenated name absent from the fleet

- **WHEN** the fleet's module paths are enumerated
- **THEN** none contains a hyphen in its leaf segment
