## MODIFIED Requirements

### Requirement: Deletion cleanup with prune enabled
When a ModuleRelease with `spec.prune=true` is deleted, the controller MUST delete every resource listed in `status.inventory.entries` that the delete verdict lets it delete (requirement "Deletion cleanup judges every object with the delete verdict"), respecting safety exclusions and the protection of PersistentVolumeClaims: a `PersistentVolumeClaim` of the core API group is deleted only when `spec.dataPolicy` is `Delete`.

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

### Requirement: Deletion cleanup judges every object with the delete verdict
With `spec.prune` true, the deletion cleanup of a ModuleInstance or a ModulePackage MUST ask the library's delete verdict for every inventory entry and MUST delete an object only when the verdict says proceed, with a precondition on the UID of the object that was judged. It MUST judge with `status.instanceUUID`, and with `status.previousInstanceUUID` when it is set: an object that carries either identity is the instance's own. Source: 0012:D4:R1, 0012:D8:R8; owner decision of 2026-10-08 (both identities are kept until the prune succeeded).

With an empty `status.instanceUUID` the cleanup MUST ask the verdict with no identity. The verdict then compares no UUID label, and it leaves in place every object that carries an adopt annotation, also one that names this instance, because no identity is known to compare it with.

An object the verdict skips MUST be left in the cluster and MUST NOT hold the finalizer. A read that fails with an error other than NotFound, and a DELETE that fails, a DELETE refused on the UID precondition included, MUST hold the finalizer, and the cleanup MUST be retried. The rules for PersistentVolumeClaims and for a missing ServiceAccount are not changed.

#### Scenario: Object adopted by another instance survives the deletion
- **GIVEN** a ModuleInstance with `spec.prune=true` being deleted, whose inventory holds ConfigMap `team-a/shared` carrying the annotation `opmodel.dev/adopt` with the UUID of another instance, and Deployment `team-a/app` of its own
- **WHEN** the deletion cleanup runs
- **THEN** the Deployment is deleted and the ConfigMap still exists
- **AND** the finalizer is removed

#### Scenario: Object of another instance survives the deletion
- **GIVEN** a ModuleInstance being deleted with `status.instanceUUID` `A` and no `status.previousInstanceUUID`, whose inventory holds a ConfigMap that carries the UUID label `B`
- **WHEN** the deletion cleanup runs
- **THEN** the ConfigMap still exists and the finalizer is removed

#### Scenario: Deletion before an identity change is settled removes the live workload
- **GIVEN** a ModuleInstance with `spec.prune=true`, `status.instanceUUID` `B` and `status.previousInstanceUUID` `A`, whose inventory holds Deployment `team-a/app`, already relabelled to `B`, and ConfigMap `team-a/old`, still labelled `A`
- **WHEN** the instance is deleted and the deletion cleanup runs
- **THEN** the Deployment and the ConfigMap are both deleted
- **AND** the finalizer is removed

#### Scenario: Deletion with no recorded identity leaves an annotated object
- **GIVEN** a ModulePackage with `spec.prune=true` and no `status.instanceUUID` being deleted, whose inventory holds a ConfigMap that is managed by OPM and has no adopt annotation, and a Secret that carries an adopt annotation
- **WHEN** the deletion cleanup runs
- **THEN** the ConfigMap is deleted and the Secret still exists
- **AND** the finalizer is removed

#### Scenario: A replaced object holds the finalizer for one more attempt
- **GIVEN** a deletion cleanup whose DELETE of an inventory object is refused on the UID precondition
- **WHEN** the reconcile ends
- **THEN** the finalizer is still present
- **AND** the next cleanup reads the object that now holds the name and judges it
