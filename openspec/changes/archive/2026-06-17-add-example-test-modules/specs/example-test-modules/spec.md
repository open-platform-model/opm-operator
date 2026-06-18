## ADDED Requirements

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
