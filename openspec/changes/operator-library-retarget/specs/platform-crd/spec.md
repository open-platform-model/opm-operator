# platform-crd — Delta

## MODIFIED Requirements

### Requirement: Subscription shape

`PlatformSpec.Registry` SHALL map major-suffixed catalog module paths (`opmodel.dev/catalogs/opm@v2`) to subscriptions of the form `{enable?, version}`. `version` SHALL name exactly one published catalog build as a bare SemVer string (`MinLength=1`); the filter vocabulary (`filter.range`, `filter.allow`, `filter.deny`) SHALL NOT exist in the schema. A subscription without a version SHALL never materialize anything. Whether `version` is schema-`required` or schema-optional follows the recorded ratcheting measurement; the posture changes only the rejecting actor — admission in the required posture, platform synthesis at reconcile (surfaced as `MaterializeFailed` naming the subscription path) in the optional posture.

#### Scenario: Scalar subscription accepted

- **WHEN** a Platform is applied with `registry: {"opmodel.dev/catalogs/opm@v2": {version: "2.0.0-alpha.3"}}`
- **THEN** the API server accepts it and the reconciler materializes exactly that build

#### Scenario: Filter rejected

- **WHEN** a Platform is applied carrying `filter` under a subscription
- **THEN** the structural schema prunes or rejects the field; it never reaches the reconciler

#### Scenario: Stored legacy singleton remains serviceable

- **WHEN** the stored `cluster` Platform predates the schema change (carries `filter`, lacks `version`)
- **THEN** the operator's status patches against it continue to succeed
- **AND** its subscriptions fail materialization with a message naming the missing version until the spec is re-applied
