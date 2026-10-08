# ssa-apply Specification

## Purpose

The `internal/apply` package applies rendered resources to the cluster with Server-Side Apply as the `opm-controller` field manager, in Flux's `ApplyAllStaged` stages, and reports how many resources it created, updated and left unchanged.

## Requirements

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
- **THEN** the apply succeeds (Flux always applies with ForceOwnership, so SSA ownership conflicts do not surface through this layer)

### Requirement: Staged apply ordering
Resources MUST be applied using Flux's `ApplyAllStaged`. It applies cluster definitions (CRDs, Namespaces, ClusterRoles) first and waits for them to become ready. It then applies class definitions and waits for them, then any custom-stage kinds, then everything else. A CRD is ready once its `Established` condition is True. Discovery of its kind can lag that condition; the requirement "Custom resources wait for discovery of a CRD in the same set" covers that lag.

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

### Requirement: Custom resources wait for discovery of a CRD in the same set
When a staged apply fails only because the API server does not serve a kind yet, and a `CustomResourceDefinition` in the same resource set defines that kind (or, when discovery reports only the group, a group that a CRD in the set defines), the `internal/apply` package MUST retry the staged apply. A CRD can report `Established` before API discovery serves its kind. It MUST retry at a fixed interval, MUST NOT start a new attempt once a bounded time (10 seconds) has passed since the first retryable failure, and MUST give every attempt the caller's context unchanged. It MUST NOT retry any other error. When the bound or the caller's context ends while the error is still a no-match, `Apply` MUST return that no-match error, wrapped as any other apply failure is.

#### Scenario: Discovery serves the new kind late
- **WHEN** the resource set contains a CRD and an instance of it, and API discovery does not serve the instance's kind until some time after the CRD is `Established`, but within the bound
- **THEN** `Apply` succeeds without returning an error
- **AND** the instance exists in the cluster

#### Scenario: Custom resource without its CRD in the set fails at once
- **WHEN** the resource set contains a custom resource whose kind the API server does not serve, and no CRD in the set defines that kind or its group
- **THEN** `Apply` returns the no-match error without waiting for the retry bound

#### Scenario: Discovery never serves the kind
- **WHEN** the resource set contains a CRD and an instance of it, and API discovery does not serve the instance's kind before the bound or the caller's context ends
- **THEN** `Apply` returns an error that wraps the no-match error for that kind
- **AND** the reconcile reports it as `ApplyFailed`, as for any other apply failure

#### Scenario: Other apply errors are not retried
- **WHEN** a staged apply fails with an error that is not a no-match error, even though the set contains a CRD
- **THEN** `Apply` returns that error after one attempt

#### Scenario: An attempt is not bounded by the retry window
- **WHEN** a staged apply attempt, first or retried, runs while the caller's context has no deadline
- **THEN** the context the attempt sees has no deadline either

### Requirement: Apply result counts across a discovery retry
When `Apply` retries a staged apply, the `ApplyResult` MUST count each object by the first attempt that created or configured it, so a retry never reports an object that this call created or configured as unchanged.

#### Scenario: CRD created by the first attempt
- **WHEN** the first attempt creates a CRD and fails on its instance, and the retry sees the CRD unchanged and creates the instance
- **THEN** the `ApplyResult` counts both the CRD and the instance as created

### Requirement: A forced recreate keeps PersistentVolumeClaims unless data deletion is allowed
The apply MUST take, beside `force`, an option that allows the deletion of data, and the option MUST be off at its zero value. When `force` is true and the option is off, the apply MUST NOT delete a PersistentVolumeClaim of the core API group. Before it applies or deletes any object, it MUST check every such claim of the resource set that exists in the cluster. When the API server refuses the update of one (the error the forced recreate would answer with a delete), the apply MUST return an error that names the claim's namespace and name, the refused field or fields as the API server reports them, and the API server's message. In that case no object of the resource set is applied, created or deleted.

When the option is on, a forced recreate MUST delete and recreate a claim as it does any other object. When `force` is false, the option has no effect. The forced recreate of every other kind MUST NOT change.

The resource manager MUST refuse on its own to delete a core PersistentVolumeClaim during an apply that does not allow the deletion of data, so that a claim that changes between the check and the apply is still kept.

#### Scenario: A claim with a changed immutable field is kept
- **GIVEN** a live PersistentVolumeClaim and a resource set that holds the claim with another `storageClassName` and a ConfigMap with new data
- **WHEN** the set is applied with `force` true and the data option off
- **THEN** the apply returns an error that names the claim and the field `spec`
- **AND** the claim has the UID it had, and no deletion timestamp
- **AND** the ConfigMap is unchanged

#### Scenario: The claim is recreated when data deletion is allowed
- **GIVEN** the same claim and resource set
- **WHEN** the set is applied with `force` true and the data option on
- **THEN** the apply succeeds and the claim has a new UID and the new `storageClassName`

#### Scenario: Another kind is recreated as before
- **GIVEN** a live immutable ConfigMap and a resource set that changes its data
- **WHEN** the set is applied with `force` true and the data option off
- **THEN** the apply succeeds and the ConfigMap has a new UID and the new data

#### Scenario: A claim that needs no recreate is applied
- **GIVEN** a live claim and a resource set that changes only a label of the claim
- **WHEN** the set is applied with `force` true and the data option off
- **THEN** the apply succeeds and the claim keeps its UID

#### Scenario: The resource manager refuses a claim delete
- **GIVEN** a resource manager built by the apply package
- **WHEN** its client is asked to delete a core PersistentVolumeClaim outside an apply that allows the deletion of data
- **THEN** the delete is refused and the claim is untouched
