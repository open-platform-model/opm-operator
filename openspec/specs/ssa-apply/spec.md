### Requirement: SSA apply with opm-controller field manager
The `internal/apply` package MUST apply resources using Server-Side Apply with field manager name `opm-controller`.

#### Scenario: Successful apply
- **WHEN** a set of valid Kubernetes resources is applied
- **THEN** the resources exist in the cluster with `opm-controller` as the field manager

#### Scenario: Force enables immutable field recreation
- **WHEN** `force` is true and an object has an immutable field change
- **THEN** the apply succeeds by deleting and recreating the object

#### Scenario: Different field manager can overwrite fields
- **WHEN** another field manager owns a field and a second manager applies a change
- **THEN** the apply succeeds (Flux always uses ForceOwnership; SSA ownership conflicts do not surface through this layer — see `docs/design/flux-ssa-staging.md`)

### Requirement: Staged apply ordering
Resources MUST be applied using Flux's `ApplyAllStaged`, which applies cluster definitions (CRDs, Namespaces, ClusterRoles) first with readiness waits, then class definitions, then everything else. See `docs/design/flux-ssa-staging.md` for the full 4-stage model.

#### Scenario: CRD applied before custom resource
- **WHEN** the resource set contains both a CRD and an instance of that CRD
- **THEN** the CRD is applied in the cluster definitions stage before the instance in the default stage

#### Scenario: Namespace applied before namespaced resource
- **WHEN** the resource set contains a Namespace and resources in that namespace
- **THEN** the Namespace is applied in the cluster definitions stage before the namespaced resources

### Requirement: Apply result
The `Apply` function MUST return an `ApplyResult` with counts of created, updated, and unchanged resources.

#### Scenario: Mixed result
- **WHEN** applying a set where some resources are new and some already exist unchanged
- **THEN** the `ApplyResult` reflects the correct counts for each category
