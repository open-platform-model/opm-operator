## ADDED Requirements

### Requirement: DependsOn ordering
The `ReleaseReconciler` MUST check `spec.dependsOn` references before proceeding with reconciliation. If any referenced Release is not `Ready=True`, the reconciler MUST requeue without proceeding.

#### Scenario: All dependencies ready
- **WHEN** a Release CR has `spec.dependsOn` listing other Release CRs, and all referenced Releases have `Ready=True`
- **THEN** the reconciler proceeds with normal reconciliation

#### Scenario: Dependency not ready
- **WHEN** a Release CR has `spec.dependsOn` listing a Release that does not have `Ready=True`
- **THEN** the reconciler sets `Ready=False` with reason `DependenciesNotReady`, emits an event naming the blocking dependency, and requeues with interval

#### Scenario: Dependency not found
- **WHEN** a Release CR has `spec.dependsOn` referencing a Release that does not exist
- **THEN** the reconciler sets `Ready=False` with reason `DependenciesNotReady` and requeues with interval

#### Scenario: No dependencies
- **WHEN** a Release CR has no `spec.dependsOn` entries (empty or nil)
- **THEN** the reconciler proceeds with normal reconciliation without dependency checks

### Requirement: DependsOn references same-namespace Releases
The `spec.dependsOn` field MUST reference Release CRs in the same namespace as the Release CR itself. Cross-namespace dependencies are not supported.

#### Scenario: Same-namespace dependency
- **WHEN** `spec.dependsOn` references a Release by name without a namespace
- **THEN** the reconciler looks up the dependency in the same namespace as the Release CR

#### Scenario: Cross-namespace dependency specified
- **WHEN** `spec.dependsOn` references a Release with a namespace different from the Release CR
- **THEN** the reconciler sets `Ready=False` with reason `DependenciesNotReady` and a message indicating cross-namespace dependencies are not supported
