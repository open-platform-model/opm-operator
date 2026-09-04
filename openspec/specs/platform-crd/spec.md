## Purpose

Define the cluster-scoped singleton `Platform` custom resource: its CRD shape
(spec projecting core `#Platform`, status carrying conditions and
`observedGeneration`), its singleton enforcement, and its registration in the
runtime scheme without a reconciler.

## Requirements

### Requirement: Cluster-scoped singleton Platform resource

The operator SHALL define a `Platform` custom resource that is cluster-scoped and
constrained to a single instance. The only permitted `metadata.name` SHALL be
`cluster`, enforced declaratively by a CEL validation rule on the resource root
(no admission webhook). Because the resource is cluster-scoped, name uniqueness
guarantees at most one `Platform` can exist.

#### Scenario: Platform named cluster is accepted

- **WHEN** a `Platform` with `metadata.name: cluster` is applied
- **THEN** the API server accepts it

#### Scenario: Platform with any other name is rejected

- **WHEN** a `Platform` with `metadata.name` other than `cluster` is applied
- **THEN** the API server rejects it with the CEL validation message identifying the singleton constraint

#### Scenario: Platform is cluster-scoped

- **WHEN** the CRD is installed
- **THEN** `Platform` is registered with `scope: Cluster`
- **AND** `Platform` objects carry no namespace

### Requirement: PlatformSpec projects core #Platform

`PlatformSpec` SHALL carry `type` (required, informational discriminator), `registry` (path-keyed catalog subscriptions) and `skewPolicy` (optional enum `Warn` | `Refuse`; unset means `Warn`). `skewPolicy` is the operator's response when a module's declared catalog requirement is newer than the build the platform pins: `Warn` renders and reports the skew; `Refuse` refuses the render. The field SHALL be validated at admission by an enum constraint. The operator generates a platform CUE module from `type` and `registry`; `skewPolicy` is not part of the module.

#### Scenario: Skew policy defaults to Warn

- **WHEN** a Platform is applied without `spec.skewPolicy`
- **THEN** it is admitted and reconciles as `Warn`

#### Scenario: Invalid skew policy is rejected at admission

- **WHEN** a Platform sets `spec.skewPolicy: Strict`
- **THEN** the API server rejects it

#### Scenario: Minimal valid platform spec

- **WHEN** a `Platform` is applied with `spec.type` set and a `spec.registry` entry keyed by a catalog module path carrying only a `version`
- **THEN** the API server accepts it, the omitted `enable` is understood as the schema default (true) and the omitted `skewPolicy` as `Warn`

#### Scenario: type is required

- **WHEN** a `Platform` is applied without `spec.type`
- **THEN** the API server rejects it as a missing required field

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

### Requirement: PlatformStatus carries conditions and observedGeneration

`PlatformStatus` SHALL carry `conditions` (a `metav1.Condition` list keyed by type), `observedGeneration` and `operatorVersion`. The Ready condition SHALL summarise module generation: `Ready=True` reason `Generated`; `Ready=False` reason `BuildFailed` or `GenerateFailed`.

#### Scenario: Status reflects the generate-and-build outcome

- **WHEN** the Platform reconciles
- **THEN** `status.conditions` carries a Ready condition with one of the reasons `Generated`, `BuildFailed` or `GenerateFailed`

#### Scenario: Status subresource present

- **WHEN** the CRD is installed
- **THEN** `Platform` exposes a `/status` subresource, and `status.conditions`, `status.observedGeneration` and `status.operatorVersion` are part of the schema

#### Scenario: Operator printcolumn

- **WHEN** `kubectl get platform` runs against a reconciled Platform
- **THEN** the output includes an `Operator` column showing `.status.operatorVersion`

### Requirement: Platform registered in the scheme without a reconciler

The operator SHALL register `Platform` and `PlatformList` with the runtime scheme
so the types are serializable and installable. This change SHALL NOT register any
controller/reconciler for `Platform`; applying a `Platform` resource produces no
reconciliation and no cluster mutations.

#### Scenario: Types registered, no reconcile

- **WHEN** the manager starts with this change
- **THEN** `Platform`/`PlatformList` are registered in the scheme
- **AND** no controller watches `Platform`
- **AND** applying a `Platform` named `cluster` triggers no reconcile and changes nothing in the cluster
