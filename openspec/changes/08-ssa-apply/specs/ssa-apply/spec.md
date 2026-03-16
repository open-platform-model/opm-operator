## ADDED Requirements

### Requirement: SSA apply with opm-controller field manager
The `internal/apply` package MUST apply resources using Server-Side Apply with field manager name `opm-controller`.

#### Scenario: Successful apply
- **WHEN** a set of valid Kubernetes resources is applied
- **THEN** the resources exist in the cluster with `opm-controller` as the field manager

#### Scenario: Force conflicts enabled
- **WHEN** `forceConflicts` is true and another manager owns a conflicting field
- **THEN** the apply succeeds by taking ownership of the conflicting field

#### Scenario: Force conflicts disabled (default)
- **WHEN** `forceConflicts` is false and another manager owns a conflicting field
- **THEN** the apply returns a conflict error

### Requirement: Staged apply ordering
Resources MUST be applied in two stages: CRDs and Namespaces first, then all other resources.

#### Scenario: CRD applied before custom resource
- **WHEN** the resource set contains both a CRD and an instance of that CRD
- **THEN** the CRD is applied in stage 1 before the instance in stage 2

#### Scenario: Namespace applied before namespaced resource
- **WHEN** the resource set contains a Namespace and resources in that namespace
- **THEN** the Namespace is applied in stage 1 before the namespaced resources in stage 2

### Requirement: Apply result
The `Apply` function MUST return an `ApplyResult` with counts of created, updated, and unchanged resources.

#### Scenario: Mixed result
- **WHEN** applying a set where some resources are new and some already exist unchanged
- **THEN** the `ApplyResult` reflects the correct counts for each category
