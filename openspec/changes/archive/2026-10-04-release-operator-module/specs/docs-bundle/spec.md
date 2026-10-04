## MODIFIED Requirements

### Requirement: Every operator release publishes its docs bundle

`release.yml` SHALL run a `publish-docs` job that calls docs-kit's `publish.yml` with `project: opm-operator`, `mode: release` and the operator release's tag, in the workflow run of the push that merged the operator's release PR, only when the operator package `"."` created a release and only after `image-release` succeeded. A release created only by the operator module's package SHALL NOT run it. `publish-release` SHALL NOT wait for it.

#### Scenario: A release publishes its bundle

- **WHEN** the release PR for `v1.0.0-beta.5` merges and `image-release` succeeds
- **THEN** `publish-docs` publishes `ghcr.io/open-platform-model/docs/opm-operator` for `1.0.0-beta.5`, signed by the operator's workflow

#### Scenario: A failed image publishes no docs

- **WHEN** `image-release` fails
- **THEN** `publish-docs` is skipped, and `docs.yml` dispatched with `mode: release` and the tag recovers the bundle once the release is fixed

#### Scenario: A module release publishes no operator bundle

- **WHEN** only `opm_operator-v0.2.0` is created
- **THEN** `publish-docs` is skipped
