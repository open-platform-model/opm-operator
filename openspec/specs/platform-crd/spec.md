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
`registry` map keyed by catalog CUE module path. Each registry entry SHALL be a
`Subscription` with an optional `enable` flag (a pointer/optional field such that
an omitted value defers to the schema default of `true`) and an optional `filter`
carrying a SemVer `range`, an `allow` list, and a `deny` list. The field shapes
SHALL correspond to `synth.PlatformInput` (`Type`, `Subscriptions` map of
`{Enable, Filter{Range, Allow, Deny}}`) so a later reconciler can convert spec to
synth input without a translation layer.

#### Scenario: Minimal valid platform spec

- **WHEN** a `Platform` is applied with `spec.type` set and a `spec.registry` entry keyed by a catalog module path with no `enable` and no `filter`
- **THEN** the API server accepts it
- **AND** the omitted `enable` is understood downstream as the schema default (true)

#### Scenario: Subscription with a SemVer filter

- **WHEN** a `spec.registry` entry sets `filter.range`, `filter.allow`, and/or `filter.deny`
- **THEN** the API server accepts the values as the typed projection of `#SubscriptionFilter`

#### Scenario: type is required

- **WHEN** a `Platform` is applied without `spec.type`
- **THEN** the API server rejects it as a missing required field

### Requirement: PlatformStatus carries conditions and observedGeneration

`PlatformStatus` SHALL carry a `conditions []metav1.Condition` list (list-map
keyed by `type`) and an `observedGeneration` field, following the existing CRD
status conventions in this repo. The status SHALL accommodate a `Materialized`
condition that a later reconciler sets; this change defines the field shape only
and sets no conditions.

#### Scenario: Status subresource present

- **WHEN** the CRD is installed
- **THEN** `Platform` exposes a `/status` subresource
- **AND** `status.conditions` and `status.observedGeneration` are part of the schema

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
