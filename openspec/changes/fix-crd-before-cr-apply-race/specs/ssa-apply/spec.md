## ADDED Requirements

### Requirement: Custom resources wait for discovery of a CRD in the same set
When a staged apply fails only because the API server does not serve a kind yet, and a `CustomResourceDefinition` in the same resource set defines that kind, the `internal/apply` package MUST retry the staged apply. It MUST retry at a fixed interval, for no longer than a bounded time (10 seconds), and never past the caller's context. A CRD can report `Established` before API discovery serves its kind, so this retry is part of applying a CRD and its instances together. The package MUST NOT retry any other error. That includes a no-match error for a kind that no CRD in the set defines. When the bound or the context ends while the error is still a no-match, `Apply` MUST return that no-match error, wrapped as any other apply failure is. The `ApplyResult` MUST count each object by the first attempt that created or configured it, so a retry never reports a CRD that this call created as unchanged.

#### Scenario: Discovery serves the new kind late
- **WHEN** the resource set contains a CRD and an instance of it, and API discovery does not serve the instance's kind until some time after the CRD is `Established`, but within the bound
- **THEN** `Apply` succeeds without returning an error
- **AND** the instance exists in the cluster
- **AND** the `ApplyResult` counts both the CRD and the instance as created

#### Scenario: Custom resource without its CRD in the set fails at once
- **WHEN** the resource set contains a custom resource whose kind the API server does not serve, and no CRD in the set defines that kind
- **THEN** `Apply` returns the no-match error without waiting for the retry bound

#### Scenario: Discovery never serves the kind
- **WHEN** the resource set contains a CRD and an instance of it, and API discovery does not serve the instance's kind before the bound or the caller's context ends
- **THEN** `Apply` returns an error that wraps the no-match error for that kind
- **AND** the reconcile reports it as `ApplyFailed`, as for any other apply failure

#### Scenario: Other apply errors are not retried
- **WHEN** a staged apply fails with an error that is not a no-match error, even though the set contains a CRD
- **THEN** `Apply` returns that error after one attempt

## MODIFIED Requirements

### Requirement: Staged apply ordering
Resources MUST be applied using Flux's `ApplyAllStaged`. It applies cluster definitions (CRDs, Namespaces, ClusterRoles) first and waits for them to become ready. It then applies class definitions and waits for them, then any custom-stage kinds, then everything else. A CRD is ready once its `Established` condition is True. Discovery of its kind can lag that condition; the requirement "Custom resources wait for discovery of a CRD in the same set" covers that lag.

#### Scenario: CRD applied before custom resource
- **WHEN** the resource set contains both a CRD and an instance of that CRD
- **THEN** the CRD is applied in the cluster definitions stage before the instance in the default stage

#### Scenario: Namespace applied before namespaced resource
- **WHEN** the resource set contains a Namespace and resources in that namespace
- **THEN** the Namespace is applied in the cluster definitions stage before the namespaced resources
