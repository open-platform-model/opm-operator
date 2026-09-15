## Purpose

Define the cluster-scoped `TransformerRegistration` custom resource: the shape a provider module's rendered claim lands as, the fields the API server requires of it, the name it must carry, and the RBAC that decides who may create one. This capability covers the kind and its admission-level guarantees only; accepting, activating and acting on a claim are separate capabilities (enhancement 0015 D3, D12, D15).

## Requirements

### Requirement: Cluster-scoped TransformerRegistration resource

The operator SHALL define a `TransformerRegistration` custom resource in group `opmodel.dev`, version `v1alpha1`, with `scope: Cluster` and a status subresource — `apiVersion: opmodel.dev/v1alpha1`, `kind: TransformerRegistration`. Nothing in a catalog can derive a CRD this repo owns, so the `catalog_opm` renderer SHALL emit these three literals exactly; the operator's group is authoritative. The types SHALL be registered in the runtime scheme.

#### Scenario: The kind is installable and cluster-scoped

- **WHEN** the CRD is installed
- **THEN** `TransformerRegistration` is registered with `scope: Cluster`
- **AND** a `TransformerRegistration` object carries no namespace

#### Scenario: A rendered claim is accepted by the API server

- **WHEN** an object carrying the group, version and kind the catalog renderer emits is applied, with every required field present
- **THEN** the API server accepts and stores it

### Requirement: Every claim field is required at admission

`TransformerRegistrationSpec` SHALL require `catalog` (the provider catalog's module path), `version` (its build), `provides` (the contract keys it claims to implement) and `providerRef` (the namespace and name of the claiming instance). The API server SHALL reject a claim missing any of them. This validation SHALL NOT be delegated to the authoring catalog: a missing required field in CUE is an incomplete value rather than an error, so a claim can reach the cluster with a field absent even though the contract declares it required.

#### Scenario: A claim missing its catalog is rejected

- **WHEN** a `TransformerRegistration` is applied with `spec.version`, `spec.provides` and `spec.providerRef` but no `spec.catalog`
- **THEN** the API server rejects it, naming the missing field

#### Scenario: A claim with an empty provides list is accepted

- **WHEN** a `TransformerRegistration` is applied whose `spec.provides` is an empty list
- **THEN** the API server accepts it, because a provider catalog implementing no provider-fulfilled contract is a claim that later acceptance refuses on its merits, not a malformed object

### Requirement: The claim's name is instance-derived

`metadata.name` SHALL be constrained to the dot-joined `<namespace>.<name>` of the claiming instance, the name the catalog renderer emits (enhancement 0015 D12). A namespace cannot contain a dot, so the join is collision-free and two instances of one provider module produce two distinct objects rather than contending for one.

#### Scenario: An instance-derived name is accepted

- **WHEN** a claim named `backup-system.k8up` is applied
- **THEN** the API server accepts it

#### Scenario: A name that is not instance-derived is rejected

- **WHEN** a claim named `k8up` is applied, carrying no dot
- **THEN** the API server rejects it with a message identifying the required name shape

### Requirement: Claim status separates acceptance from activation

`TransformerRegistrationStatus` SHALL carry `conditions`, `accepted` and `active`, so that a stored claim can report the three states enhancement 0015 D3 defines: not yet judged, accepted but inactive, and active. A controller SHALL watch the kind and record a verdict on every claim, setting `conditions`, `accepted` and `observedGeneration`, and SHALL set `active` when an accepted claim's provider is ready. All three states are now reachable.

#### Scenario: A stored claim receives a verdict

- **WHEN** a valid `TransformerRegistration` is applied and the manager is running
- **THEN** a reconcile is triggered for it and its status reports whether it was accepted
- **AND** `observedGeneration` matches the claim's generation

#### Scenario: A stored claim is inert

- **WHEN** a `TransformerRegistration` is applied and reconciled to any state, refused, accepted or active
- **THEN** nothing in the cluster renders differently as a result: no workload, no `Platform` field and no generated platform module reflects the claim

### Requirement: Creating a claim requires platform-admin RBAC

The operator SHALL ship a ClusterRole granting create, update and delete on `transformerregistrations`, and SHALL NOT grant those verbs to any tenant-facing role it ships. A module applied under an impersonated tenant ServiceAccount therefore cannot create a claim unless a cluster administrator has deliberately bound that role. The operator's own manager role SHALL carry the read and status verbs it needs.

#### Scenario: A tenant ServiceAccount cannot create a claim

- **WHEN** a rendered `TransformerRegistration` is applied through the impersonated tenant ServiceAccount of a ModulePackage whose subject holds no platform-admin binding
- **THEN** the API server refuses the create as forbidden, and the refusal surfaces on the package as an impersonation failure

#### Scenario: A platform-admin subject may create a claim

- **WHEN** the same object is applied by a subject bound to the shipped platform-admin ClusterRole
- **THEN** the API server accepts the create
