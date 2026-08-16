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

`PlatformSpec` SHALL be a near-1:1 projection of the core `#Platform` definition.
It SHALL carry a required `type` string (the informational discriminator) and a
`registry` map keyed by major-suffixed catalog CUE module path. Each registry
entry SHALL be a `Subscription` with an optional `enable` flag (a pointer/optional
field such that an omitted value defers to the schema default of `true`) and a
`version` (see the Subscription shape requirement). The field shapes SHALL
correspond to `synth.PlatformInput` (`Type`, `Subscriptions` map of
`{Enable, Version}`) so the reconciler can convert spec to synth input without a
translation layer.

#### Scenario: Minimal valid platform spec

- **WHEN** a `Platform` is applied with `spec.type` set and a `spec.registry` entry keyed by a catalog module path carrying only a `version`
- **THEN** the API server accepts it
- **AND** the omitted `enable` is understood downstream as the schema default (true)

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

`PlatformStatus` SHALL carry a `conditions []metav1.Condition` list (list-map
keyed by `type`), an `observedGeneration` field, and an optional
`operatorVersion` string field the reconciler stamps with the running
operator's version (enhancement 0006 D24 — read by the CLI's version-skew
ceiling), following the existing CRD status conventions in this repo. The
status SHALL accommodate a `Materialized` condition that a later reconciler
sets. The CRD SHALL expose an `Operator` printcolumn sourced from
`.status.operatorVersion`.

#### Scenario: Status subresource present

- **WHEN** the CRD is installed
- **THEN** `Platform` exposes a `/status` subresource
- **AND** `status.conditions`, `status.observedGeneration`, and `status.operatorVersion` are part of the schema

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
