# example-test-modules Specification

## Purpose
TBD - created by archiving change add-example-test-modules. Update Purpose after archive.
## Requirements
### Requirement: Public example module path

All example test modules SHALL be authored under the CUE module path `opmodel.dev/modules/test/<module>@v0`, where `<module>` is the module's short name. Each module's `cue.mod/module.cue` `module:` field and its `#Module.metadata.modulePath` SHALL be consistent with this path so the module resolves to `ghcr.io/open-platform-model` under the standard `opmodel.dev` registry mapping already used by `core` and `catalog`.

#### Scenario: New module declares public path
- **WHEN** the podinfo module is authored
- **THEN** its `cue.mod/module.cue` declares `module: "opmodel.dev/modules/test/podinfo@v0"` and `metadata.modulePath` is `"opmodel.dev/modules/test"`

#### Scenario: Migrated module changes path only
- **WHEN** the `hello` and `hello-web` modules are migrated
- **THEN** their `module:`, `metadata.modulePath`, dependent `release.cue` imports, and `modulerelease.yaml`/`release.yaml`/`ocirepository.yaml` path fields reference `opmodel.dev/modules/test/<m>` instead of `testing.opmodel.dev/modules/<m>`
- **AND** their pinned `opmodel.dev/core@v0` and `opmodel.dev/catalogs/opm@v0` dependency versions are unchanged

#### Scenario: Consumer resolves without extra config
- **WHEN** a user with the standard `CUE_REGISTRY` mapping `opmodel.dev=ghcr.io/open-platform-model,registry.cue.works` resolves `opmodel.dev/modules/test/podinfo@v0`
- **THEN** resolution succeeds against `ghcr.io/open-platform-model` with no additional registry mapping beyond what `core`/`catalog` already require

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

Each example module SHALL include a `ModuleRelease` manifest (and, where applicable, `Release`/`OCIRepository` manifests) that a user can apply against a running operator to deploy the example. The manifests SHALL reference the public `opmodel.dev/modules/test/<m>@v0` path and a concrete version.

#### Scenario: Manifest references public module
- **WHEN** the podinfo `modulerelease.yaml` is inspected
- **THEN** its `spec.module.path` is `opmodel.dev/modules/test/podinfo@v0` with an explicit `spec.module.version`

### Requirement: podinfo liveness/readiness e2e validation

An e2e test SHALL deploy the podinfo `ModuleRelease` against a Kind-backed operator and assert that the rendered probes function — the Deployment's pods become Ready, which requires both the liveness and readiness probes to pass.

#### Scenario: podinfo pods become ready
- **WHEN** the e2e suite applies the podinfo ModuleRelease and the controller reconciles it
- **THEN** the resulting Deployment's pods SHALL reach Ready within the test timeout, demonstrating the modelled probes succeed against the running container

#### Scenario: Rendered probe contract matches container
- **WHEN** the e2e suite inspects the deployed podinfo container
- **THEN** the container's liveness/readiness probe HTTP paths and port match the values declared in the module (`/healthz`, `/readyz`, 9898)

### Requirement: ModulePackage fixture parity for example modules

Every example test module (`hello`, `hello-web`, `podinfo`, `redis`) SHALL provide a
ModulePackage fixture under `test/fixtures/modulepackages/<module>/` that mirrors the
existing `hello` fixture: a `cue.mod/module.cue` declaring the release module
`opmodel.dev/releases/test/<module>@v0`, an `instance.cue` that imports the published
module and embeds it via `core.#ModuleInstance.#module`, a `values.cue` holding the
package's configuration, a `modulepackage.yaml` (ServiceAccount + RoleBinding + `ModulePackage`
CR referencing an `OCIRepository`), and an `ocirepository.yaml`. The release module's
`cue.mod/module.cue` SHALL pin the same `opmodel.dev/modules/test/<module>@v0` version that
the module's own `metadata.version` declares.

#### Scenario: Each module has a modulepackage fixture
- **WHEN** `test/fixtures/modulepackages/` is enumerated
- **THEN** it contains a `<module>/` directory for each of `hello`, `hello-web`, `podinfo`, `redis`
- **AND** each directory contains `cue.mod/module.cue`, `instance.cue`, `values.cue`, `modulepackage.yaml`, and `ocirepository.yaml`

#### Scenario: instance.cue imports and embeds the published module
- **WHEN** a module's `instance.cue` is inspected
- **THEN** it embeds `core.#ModuleInstance`, imports `opmodel.dev/modules/test/<module>@v0`, and sets `#module` to the imported module
- **AND** its `cue.mod/module.cue` pins that module at the version declared in the module's own `metadata.version`

#### Scenario: ModulePackage CR references its OCIRepository
- **WHEN** a module's `modulepackage.yaml` is inspected
- **THEN** its `ModulePackage` `spec.sourceRef` names the `OCIRepository` declared in the sibling `ocirepository.yaml`, whose `url` ends in `opmodel.dev/releases/test/<module>`

### Requirement: hello-web ready-to-apply ModuleInstance

The `hello-web` example module SHALL ship a `test/fixtures/modules/hello-web/moduleinstance.yaml`
bundle — a `ServiceAccount`, `Role`, and `RoleBinding` granting the applier just the
resource kinds the module renders (`apps/deployments`), plus a `ModuleInstance` referencing
the public `opmodel.dev/modules/test/hello-web@v0` path and a concrete version — so all four
example modules can be applied directly via a `ModuleInstance`.

#### Scenario: hello-web manifest references public module
- **WHEN** `test/fixtures/modules/hello-web/moduleinstance.yaml` is inspected
- **THEN** its `ModuleInstance` `spec.module.path` is `opmodel.dev/modules/test/hello-web@v0` with an explicit `spec.module.version`
- **AND** the bundle includes a `ServiceAccount`/`Role`/`RoleBinding` whose Role grants `apps/deployments`

### Requirement: ModulePackage render integration coverage

The `KernelPackageRenderer` integration test SHALL exercise every modulepackage fixture
(`hello`, `hello-web`, `podinfo`, `redis`), not only `hello`, asserting that each authored
package loads its imported `#Module` and renders at least one resource carrying the
controller's ownership labels. The coverage SHALL remain gated on the local test registry
(skipping when it is unavailable).

#### Scenario: Every modulepackage renders under a materialized platform
- **WHEN** the integration suite runs with a reachable test registry and a materialized platform
- **THEN** each of the `hello`, `hello-web`, `podinfo`, `redis` modulepackage fixtures loads without a "field not allowed" error and renders at least one resource
- **AND** each rendered resource carries the `managed-by` controller label and a non-empty module-instance UUID label

#### Scenario: Coverage skips without the test registry
- **WHEN** the integration suite runs without the local test registry configured
- **THEN** the modulepackage render cases skip rather than fail

