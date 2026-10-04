## ADDED Requirements

### Requirement: The operator module publishes its docs bundle at its own version

`docs-kit.cue` SHALL declare a second docs-kit project, `opm-operator-module`, besides `opm-operator`. It SHALL be versioned from tags with the prefix `opm_operator-v` and placed in a site version's `/docs/` tree. It SHALL be built from authored pages in the module's docs directory (`modules/opm_operator/docs/`) that describe the module's `#config`, the operator version a module version deploys and the install manifest. The bundle SHALL hold only pages under its own path, so it owns no page of the `opm-operator` bundle. The release workflow SHALL publish it with docs-kit's `publish.yml` (`project: opm-operator-module`, `mode: release`, the module tag) in the run that created the module release, only after the module publish job succeeded. The module's final publish job SHALL NOT wait for it. `docs.yml` SHALL check it on every pull request and publish its `edge` from `main` like the operator's bundle, and a dispatch with a module tag SHALL recover a missed release. The module's `#config` is documented at the module's own version, not at the operator's, because two module versions can deploy the same operator with different `#config` schemas.

#### Scenario: A module release publishes its bundle

- **WHEN** `opm_operator-v0.2.0` is created and the module publish job succeeds
- **THEN** `ghcr.io/open-platform-model/docs/opm-operator-module` is published for `0.2.0`

#### Scenario: An operator release publishes no module bundle

- **WHEN** only `v1.0.0-beta.6` is created
- **THEN** only the `opm-operator` bundle is published

#### Scenario: Both bundles are checked

- **WHEN** a pull request changes a page of the module's docs
- **THEN** `Docs / check` builds and lints both bundles and fails naming a broken page in either
