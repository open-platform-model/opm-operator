## ADDED Requirements

### Requirement: Deletion cleanup runs the library's deletion plan
With `spec.prune` true, the deletion cleanup of a ModuleInstance or a ModulePackage MUST delete an inventory object only when the library's deletion transition (`opm/k8s/lifecycle`) names that delete as the next action. Source: 0012:D4:R1.

The cleanup MUST send each delete in the order the plan gives, descending kind weight, and MUST add no ordering of its own. Each delete MUST carry the propagation policy the action names, Foreground, and the action's UID precondition.

When the object records two identities, or none on a ModulePackage, the cleanup MUST run one plan per identity, in the order the delete verdict is asked today: the entries a plan skips because the object belongs to, or is being adopted by, another instance form the plan of the next identity. An entry that no plan deletes MUST be reported with the reason of the first plan.

A PersistentVolumeClaim that `spec.dataPolicy` keeps MUST NOT be an entry of any plan.

#### Scenario: Higher kind weight is deleted first
- **GIVEN** a ModuleInstance with `spec.prune=true` being deleted, whose inventory lists a ConfigMap before a Deployment that uses it
- **WHEN** the deletion cleanup runs
- **THEN** the DELETE of the Deployment is sent before the DELETE of the ConfigMap

#### Scenario: Deletes use Foreground propagation
- **GIVEN** a ModuleInstance with `spec.prune=true` being deleted, whose inventory holds a Deployment of its own
- **WHEN** the deletion cleanup runs
- **THEN** the DELETE request of the Deployment carries the propagation policy `Foreground` and the UID of the object that was read

#### Scenario: A kept claim is never sent to the plan
- **GIVEN** a ModuleInstance with `spec.prune=true` and no `spec.dataPolicy` being deleted, whose inventory holds a PersistentVolumeClaim of its own
- **WHEN** the deletion cleanup runs
- **THEN** no DELETE is sent for the claim and one `ClaimsKept` event names it

#### Scenario: An object of the previous identity is deleted by the second plan
- **GIVEN** a ModuleInstance with `status.instanceUUID` `B` and `status.previousInstanceUUID` `A` being deleted, whose inventory holds ConfigMap `team-a/old` labelled `A`
- **WHEN** the deletion cleanup runs
- **THEN** ConfigMap `team-a/old` is deleted

### Requirement: The cleanup finalizer follows the library's hold verdict
The controller MUST remove the cleanup finalizer of a deleting ModuleInstance or ModulePackage only after the library's hold verdict (`lifecycle.MayReleaseHold`) says release for every plan of the cleanup. Source: 0012:D4:R1.

The verdict's inputs MUST be: prune from `spec.prune`; force-orphan true only when the annotation `opm.dev/force-delete-orphan` is the literal `"true"`; the deleting identity as available, missing (the ServiceAccount does not exist) or failed (any other impersonation error).

Each verdict MUST keep the status it has today: a missing identity without force-orphan stalls with `DeletionSAMissing`; a failed identity, and a step refused as Forbidden under impersonation, stall with `ImpersonationFailed`; any other failed step keeps the finalizer and is retried; force-orphan emits `OrphanedOnDeletion` and clears the inventory.

#### Scenario: Prune disabled releases without a read
- **GIVEN** a ModuleInstance with `spec.prune=false` and a non-zero `DeletionTimestamp`
- **WHEN** the controller reconciles it
- **THEN** no object is read or deleted and the finalizer is removed

#### Scenario: A failed step holds the finalizer
- **GIVEN** a deletion cleanup in which the DELETE of one ConfigMap fails with a server error
- **WHEN** the reconcile ends
- **THEN** every other entry was still attempted
- **AND** the finalizer is still present and the cleanup is retried

#### Scenario: A missing ServiceAccount holds without a plan
- **GIVEN** a ModuleInstance being deleted whose impersonated ServiceAccount does not exist, without the orphan annotation
- **WHEN** the controller reconciles it
- **THEN** no object is read or deleted, the finalizer stays and `Ready` is False with reason `DeletionSAMissing`

#### Scenario: Force-orphan lifts only a missing ServiceAccount
- **GIVEN** a ModuleInstance being deleted with the annotation `opm.dev/force-delete-orphan=true`, whose ServiceAccount exists and whose cleanup has a failed step
- **WHEN** the reconcile ends
- **THEN** the finalizer is still present

### Requirement: The finalizer is kept until the deleted objects are gone
After a release verdict, the controller MUST remove the cleanup finalizer only when every object this cleanup deleted no longer exists: its read returns NotFound, or another object (a different UID) holds its name. A kept PersistentVolumeClaim, an object left behind and an object that was already absent MUST NOT be waited for.

While at least one deleted object still exists, the reconcile MUST end without removing the finalizer, MUST set `Ready=False` and `Reconciling=True` with reason `DeletionInProgress`, and MUST request a requeue after a short fixed interval. It MUST NOT wait inside the reconcile.

Setting `spec.prune` to false on the deleting object MUST release the finalizer at the next reconcile, whatever is still terminating.

#### Scenario: A terminating Deployment holds the finalizer
- **GIVEN** a ModuleInstance with `spec.prune=true` being deleted, whose Deployment was deleted with Foreground propagation and still exists with a `deletionTimestamp`
- **WHEN** the controller reconciles the instance
- **THEN** the finalizer is still present
- **AND** `Ready` is False with reason `DeletionInProgress` and the reconcile asks for a requeue

#### Scenario: The finalizer goes when the objects are gone
- **GIVEN** the same instance, after the Deployment no longer exists
- **WHEN** the controller reconciles the instance
- **THEN** the finalizer is removed and the ModuleInstance deletion completes

#### Scenario: A name taken by a new object does not hold the finalizer
- **GIVEN** a deleted ConfigMap whose name is held by a new object with another UID
- **WHEN** the controller checks the deleted objects
- **THEN** that ConfigMap counts as gone and the new object is not deleted

#### Scenario: Turning prune off releases a waiting deletion
- **GIVEN** a ModuleInstance being deleted with reason `DeletionInProgress`
- **WHEN** a user sets `spec.prune` to false
- **THEN** the next reconcile removes the finalizer and deletes nothing more

### Requirement: A deletion that does not finish is reported as blocked
When a deleted object still exists 10 minutes after its `deletionTimestamp`, the controller MUST set `Ready=False` and `Stalled=True` with reason `DeletionBlocked` on the deleting ModuleInstance or ModulePackage. The message MUST name each remaining object by kind, namespace and name, with its finalizers, and MUST name the ways out. The controller MUST keep the finalizer, MUST keep rechecking at a fixed interval of at most one minute, and MUST NOT give up or remove the finalizer on a timeout.

#### Scenario: A Pod that cannot terminate blocks the deletion visibly
- **GIVEN** a ModuleInstance being deleted whose Deployment `media/jellyfin` has existed with a `deletionTimestamp` for more than 10 minutes
- **WHEN** the controller reconciles the instance
- **THEN** `Ready` is False and `Stalled` is True with reason `DeletionBlocked`
- **AND** the message names `Deployment/media/jellyfin` and its finalizers
- **AND** the finalizer is still present

#### Scenario: A blocked deletion completes when the object goes
- **GIVEN** a ModuleInstance with reason `DeletionBlocked`
- **WHEN** the remaining object is removed from the cluster
- **THEN** within one recheck interval the finalizer is removed

## MODIFIED Requirements

### Requirement: A kept claim never holds the finalizer
A PersistentVolumeClaim that the deletion cleanup keeps MUST NOT keep the cleanup finalizer on the object and MUST NOT put the object into a stalled or failed state. The finalizer MUST be removed in the reconcile in which every other inventory entry is gone from the cluster or skipped (requirement "The finalizer is kept until the deleted objects are gone"). A failure to delete another entry MUST still keep the finalizer, as before, and the retry MUST keep the claims again.

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
