## MODIFIED Requirements

### Requirement: Deletion cleanup with prune enabled
When a ModuleRelease with `spec.prune=true` is deleted, the controller MUST delete all resources listed in `status.inventory.entries`, respecting safety exclusions and the protection of PersistentVolumeClaims: a `PersistentVolumeClaim` of the core API group is deleted only when `spec.dataPolicy` is `Delete`.

#### Scenario: Delete all owned resources on CR deletion
- **GIVEN** a ModuleRelease with `spec.prune=true`, a non-zero `DeletionTimestamp`, and inventory entries for ConfigMap `foo` and Deployment `bar`
- **WHEN** the controller reconciles the resource
- **THEN** ConfigMap `foo` and Deployment `bar` are deleted from the cluster
- **AND** the `releases.opmodel.dev/cleanup` finalizer is removed
- **AND** the ModuleRelease deletion completes

#### Scenario: Safety exclusions during deletion
- **GIVEN** a ModuleRelease with `spec.prune=true`, a non-zero `DeletionTimestamp`, and inventory entries including a Namespace and a CRD
- **WHEN** the controller reconciles the resource
- **THEN** the Namespace and CRD are NOT deleted
- **AND** all other inventory entries are deleted
- **AND** the finalizer is removed

#### Scenario: Claims are kept on deletion by default
- **GIVEN** a ModuleInstance with `spec.prune=true`, no `spec.dataPolicy`, a non-zero `DeletionTimestamp`, and inventory entries for Deployment `media/jellyfin` and PersistentVolumeClaim `media/config`
- **WHEN** the controller reconciles the resource
- **THEN** the Deployment is deleted
- **AND** the PersistentVolumeClaim still exists in the cluster
- **AND** the cleanup finalizer is removed and the ModuleInstance deletion completes

#### Scenario: Claims are deleted on deletion when the instance opts out
- **GIVEN** the same ModuleInstance with `spec.dataPolicy=Delete`
- **WHEN** the controller reconciles the resource
- **THEN** the Deployment and the PersistentVolumeClaim are both deleted
- **AND** the cleanup finalizer is removed

## ADDED Requirements

### Requirement: A kept claim never holds the finalizer
A PersistentVolumeClaim that the deletion cleanup keeps MUST NOT keep the cleanup finalizer on the object and MUST NOT put the object into a stalled or failed state. The finalizer MUST be removed in the reconcile in which every other inventory entry is deleted or skipped. A failure to delete another entry MUST still keep the finalizer, as before, and the retry MUST keep the claims again.

#### Scenario: Deletion with only claims left completes
- **GIVEN** a ModuleInstance with `spec.prune=true`, no `spec.dataPolicy`, a non-zero `DeletionTimestamp`, and an inventory that holds only PersistentVolumeClaims
- **WHEN** the controller reconciles the resource
- **THEN** no resource is deleted
- **AND** the cleanup finalizer is removed and the ModuleInstance deletion completes

#### Scenario: Another failed delete still holds the finalizer
- **GIVEN** a ModuleInstance being deleted with inventory entries for a PersistentVolumeClaim and a ConfigMap, where the delete of the ConfigMap fails
- **WHEN** the controller reconciles the resource
- **THEN** the PersistentVolumeClaim is kept
- **AND** the finalizer is NOT removed and the controller requeues
