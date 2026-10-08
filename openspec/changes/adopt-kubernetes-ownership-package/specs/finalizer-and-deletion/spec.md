## ADDED Requirements

### Requirement: Deletion cleanup judges every object with the delete verdict
With `spec.prune` true, the deletion cleanup of a ModuleInstance or a ModulePackage MUST ask the library's delete verdict for every inventory entry, with the identity recorded in `status.instanceUUID`, and MUST delete an object only when the verdict says proceed, with a precondition on the UID of the object that was judged. With an empty `status.instanceUUID` the verdict compares no identity, as the library defines. Source: 0012:D4:R1, 0012:D8:R8.

An object the verdict skips MUST be left in the cluster and MUST NOT hold the finalizer. A read that fails with an error other than NotFound, and a DELETE that fails, a DELETE refused on the UID precondition included, MUST hold the finalizer, and the cleanup MUST be retried. The rules for PersistentVolumeClaims and for a missing ServiceAccount are not changed.

#### Scenario: Object adopted by another instance survives the deletion
- **GIVEN** a ModuleInstance with `spec.prune=true` being deleted, whose inventory holds ConfigMap `team-a/shared` carrying the annotation `opmodel.dev/adopt` with the UUID of another instance, and Deployment `team-a/app` of its own
- **WHEN** the deletion cleanup runs
- **THEN** the Deployment is deleted and the ConfigMap still exists
- **AND** the finalizer is removed

#### Scenario: Object of another instance survives the deletion
- **GIVEN** a ModuleInstance being deleted with `status.instanceUUID` `A`, whose inventory holds a ConfigMap that carries the UUID label `B`
- **WHEN** the deletion cleanup runs
- **THEN** the ConfigMap still exists and the finalizer is removed

#### Scenario: A replaced object holds the finalizer for one more attempt
- **GIVEN** a deletion cleanup whose DELETE of an inventory object is refused on the UID precondition
- **WHEN** the reconcile ends
- **THEN** the finalizer is still present
- **AND** the next cleanup reads the object that now holds the name and judges it
