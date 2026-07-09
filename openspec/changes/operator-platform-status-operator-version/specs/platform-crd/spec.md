# platform-crd (delta)

`PlatformStatus` gains `operatorVersion` (slice A6 of enhancement 0006, D24).

## MODIFIED Requirements

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
