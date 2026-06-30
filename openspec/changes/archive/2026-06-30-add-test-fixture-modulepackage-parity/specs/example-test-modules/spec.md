## ADDED Requirements

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
