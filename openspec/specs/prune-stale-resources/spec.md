## ADDED Requirements

### Requirement: Delete stale resources
The `internal/apply` package MUST provide a `Prune` function that deletes resources identified as stale (present in previous inventory, absent from current desired set).

#### Scenario: Stale resource deleted
- **WHEN** a resource is in the stale set and exists in the cluster
- **THEN** the resource is deleted from the cluster

#### Scenario: Stale resource already gone
- **WHEN** a resource is in the stale set but does not exist in the cluster
- **THEN** the prune treats this as success (no error)

#### Scenario: Empty stale set
- **WHEN** the stale set is empty
- **THEN** the prune is a no-op and returns zero deleted

### Requirement: Namespace safety exclusion
The `Prune` function MUST NOT delete resources of kind `Namespace`, regardless of stale set membership.

#### Scenario: Namespace in stale set
- **WHEN** a Namespace resource is in the stale set
- **THEN** the Namespace is skipped and counted in the skipped total

### Requirement: CRD safety exclusion
The `Prune` function MUST NOT delete resources of kind `CustomResourceDefinition`, regardless of stale set membership.

#### Scenario: CRD in stale set
- **WHEN** a CustomResourceDefinition resource is in the stale set
- **THEN** the CRD is skipped and counted in the skipped total

### Requirement: Prune result
The `Prune` function MUST return a `PruneResult` with counts of deleted and skipped resources.

#### Scenario: Mixed prune
- **WHEN** the stale set contains both pruneable and excluded resources
- **THEN** the `PruneResult` correctly reflects deleted and skipped counts

### Requirement: Continue on individual delete failure
The `Prune` function MUST continue deleting remaining stale resources if one delete fails (fail-slow).

#### Scenario: Partial failure
- **WHEN** one stale resource deletion fails but others succeed
- **THEN** all remaining deletions are attempted and the error is returned alongside the partial result
