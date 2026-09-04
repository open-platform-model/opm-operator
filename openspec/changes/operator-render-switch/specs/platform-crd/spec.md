## MODIFIED Requirements

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
