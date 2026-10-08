## ADDED Requirements

### Requirement: ModulePackage keeps PersistentVolumeClaims by default
The ModulePackage reconciler MUST apply the same protection as the ModuleInstance reconciler: with `spec.prune` true it MUST keep core `PersistentVolumeClaim` resources on the prune of stale resources and on deletion cleanup, unless `spec.deleteData` on the ModulePackage is true. It MUST emit the same `ClaimsKept` event, a kept stale claim MUST leave `status.inventory`, and a kept claim MUST NOT hold the finalizer.

#### Scenario: Stale claim of a ModulePackage kept
- **GIVEN** a ModulePackage with `spec.prune=true` and no `spec.deleteData`, whose render drops a PersistentVolumeClaim it applied before
- **WHEN** the controller reconciles
- **THEN** the claim still exists in the cluster and is not in `status.inventory`
- **AND** a `Normal` event with reason `ClaimsKept` is emitted

#### Scenario: Deletion of a ModulePackage keeps its claims
- **GIVEN** a ModulePackage with `spec.prune=true` and no `spec.deleteData` that is being deleted, with a PersistentVolumeClaim in its inventory
- **WHEN** the controller reconciles
- **THEN** the claim still exists, every other entry is deleted, and the finalizer is removed

#### Scenario: A ModulePackage that opts out deletes its claims
- **GIVEN** a ModulePackage with `spec.prune=true` and `spec.deleteData=true` that is being deleted, with a PersistentVolumeClaim in its inventory
- **WHEN** the controller reconciles
- **THEN** the claim is deleted with the other entries
