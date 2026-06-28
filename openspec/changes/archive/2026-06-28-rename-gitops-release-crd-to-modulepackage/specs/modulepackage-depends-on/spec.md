## ADDED Requirements

<!-- Renamed from `release-depends-on` (0002 D2/D10); the spec dir is `git mv`'d at archive. -->

### Requirement: DependsOn ordering
The `ModulePackageReconciler` MUST check `spec.dependsOn` references before proceeding with reconciliation. If any referenced ModulePackage is not `Ready=True`, the reconciler MUST requeue without proceeding.

#### Scenario: All dependencies ready
- **WHEN** a ModulePackage CR has `spec.dependsOn` listing other ModulePackage CRs, and all referenced ModulePackages have `Ready=True`
- **THEN** the reconciler proceeds with normal reconciliation

#### Scenario: Dependency not ready
- **WHEN** a ModulePackage CR has `spec.dependsOn` listing a ModulePackage that does not have `Ready=True`
- **THEN** the reconciler sets `Ready=False` with reason `DependenciesNotReady`, emits an event naming the blocking dependency, and requeues with interval

#### Scenario: Dependency not found
- **WHEN** a ModulePackage CR has `spec.dependsOn` referencing a ModulePackage that does not exist
- **THEN** the reconciler sets `Ready=False` with reason `DependenciesNotReady` and requeues with interval

#### Scenario: No dependencies
- **WHEN** a ModulePackage CR has no `spec.dependsOn` entries (empty or nil)
- **THEN** the reconciler proceeds with normal reconciliation without dependency checks

### Requirement: DependsOn references same-namespace ModulePackages
The `spec.dependsOn` field MUST reference ModulePackage CRs in the same namespace as the ModulePackage CR itself. Cross-namespace dependencies are not supported.

#### Scenario: Same-namespace dependency
- **WHEN** `spec.dependsOn` references a ModulePackage by name without a namespace
- **THEN** the reconciler looks up the dependency in the same namespace as the ModulePackage CR

#### Scenario: Cross-namespace dependency specified
- **WHEN** `spec.dependsOn` references a ModulePackage with a namespace different from the ModulePackage CR
- **THEN** the reconciler sets `Ready=False` with reason `DependenciesNotReady` and a message indicating cross-namespace dependencies are not supported
