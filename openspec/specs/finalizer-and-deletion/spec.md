## Purpose

Define how the operator registers its cleanup finalizer on a release, what the deletion cleanup prunes or orphans, when the finalizer is kept, and which events make a stalled deletion run again.

## Requirements

### Requirement: Finalizer registration
The controller MUST add the finalizer `releases.opmodel.dev/cleanup` to a ModuleRelease during Phase 0 if it is not already present. The reconcile call that adds the finalizer MUST request an immediate requeue (`ctrl.Result{Requeue: true}`) rather than relying on the watch event produced by the finalizer patch — that watch event is filtered by the controller's `predicate.GenerationChangedPredicate`, because finalizer patches modify `metadata.finalizers` but do not bump `metadata.generation`. Without an explicit requeue, the next reconcile would be deferred to the periodic resync (default 10h), leaving freshly-created ModuleReleases without status conditions for an unacceptable duration.

#### Scenario: First reconcile adds finalizer
- **GIVEN** a ModuleRelease without the `releases.opmodel.dev/cleanup` finalizer
- **WHEN** the controller reconciles the resource
- **THEN** the finalizer is added to `metadata.finalizers`

#### Scenario: Subsequent reconciles preserve finalizer
- **GIVEN** a ModuleRelease that already has the `releases.opmodel.dev/cleanup` finalizer
- **WHEN** the controller reconciles the resource
- **THEN** the finalizer remains unchanged

#### Scenario: Finalizer-add reconcile requests immediate requeue
- **GIVEN** a freshly-created ModuleRelease without the `releases.opmodel.dev/cleanup` finalizer
- **WHEN** the controller's first `Reconcile` call adds the finalizer
- **THEN** the reconcile returns a result with `Requeue=true` so the workqueue re-enqueues the request directly (bypassing the predicate)
- **AND** the next reconcile starts within the rate limiter's normal cadence (typically under one second), not the periodic resync window

#### Scenario: Status conditions appear after creation without manual triggers
- **GIVEN** a ModuleRelease created via the API server
- **WHEN** the controller observes the create event and reconciles via the manager
- **THEN** within a small bounded window (e.g., 10 seconds in envtest, single-digit seconds in production) `status.conditions` contains at least one of `Ready`, `Reconciling`, or `Stalled`
- **AND** no manual `kubectl edit`, periodic-resync, or other external trigger is required to produce these conditions

### Requirement: Deletion cleanup with prune enabled
When a ModuleRelease with `spec.prune=true` is deleted, the controller MUST delete every resource listed in `status.inventory.entries` that the delete verdict lets it delete (requirement "Deletion cleanup judges every object with the delete verdict"), respecting safety exclusions and the protection of PersistentVolumeClaims: a `PersistentVolumeClaim` of the core API group is deleted only when `spec.dataPolicy` is `Delete`. The finalizer is removed, and the deletion completes, once every object the cleanup deleted is gone (requirement "The finalizer is kept until the deleted objects are gone"); that can be a later reconcile than the one that sends the deletes.

#### Scenario: Delete all owned resources on CR deletion
- **GIVEN** a ModuleRelease with `spec.prune=true`, a non-zero `DeletionTimestamp`, and inventory entries for ConfigMap `foo` and Deployment `bar`
- **WHEN** the controller reconciles the resource
- **THEN** ConfigMap `foo` and Deployment `bar` are deleted from the cluster
- **AND** once every object the cleanup deleted is gone, the `releases.opmodel.dev/cleanup` finalizer is removed and the ModuleRelease deletion completes

#### Scenario: Safety exclusions during deletion
- **GIVEN** a ModuleRelease with `spec.prune=true`, a non-zero `DeletionTimestamp`, and inventory entries including a Namespace and a CRD
- **WHEN** the controller reconciles the resource
- **THEN** the Namespace and CRD are NOT deleted
- **AND** all other inventory entries are deleted
- **AND** the finalizer is removed once every object the cleanup deleted is gone, without waiting for the Namespace or the CRD

#### Scenario: Claims are kept on deletion by default
- **GIVEN** a ModuleInstance with `spec.prune=true`, no `spec.dataPolicy`, a non-zero `DeletionTimestamp`, and inventory entries for Deployment `media/jellyfin` and PersistentVolumeClaim `media/config`
- **WHEN** the controller reconciles the resource
- **THEN** the Deployment is deleted
- **AND** the PersistentVolumeClaim still exists in the cluster
- **AND** the cleanup finalizer is removed and the ModuleInstance deletion completes once the Deployment is gone, without waiting for the claim

#### Scenario: Claims are deleted on deletion when the instance opts out
- **GIVEN** the same ModuleInstance with `spec.dataPolicy=Delete`
- **WHEN** the controller reconciles the resource
- **THEN** the Deployment and the PersistentVolumeClaim are both deleted
- **AND** the cleanup finalizer is removed once both are gone

### Requirement: Deletion with prune disabled orphans resources
When a ModuleRelease with `spec.prune=false` is deleted, the controller MUST remove the finalizer without deleting any resources.

#### Scenario: Orphan resources when prune is false
- **GIVEN** a ModuleRelease with `spec.prune=false` and a non-zero `DeletionTimestamp`
- **WHEN** the controller reconciles the resource
- **THEN** no resources are deleted
- **AND** the `releases.opmodel.dev/cleanup` finalizer is removed
- **AND** the ModuleRelease deletion completes

### Requirement: Suspend does not block deletion
The controller MUST perform deletion cleanup even when `spec.suspend=true`.

#### Scenario: Cleanup proceeds despite suspend
- **GIVEN** a ModuleRelease with `spec.suspend=true` and a non-zero `DeletionTimestamp`
- **WHEN** the controller reconciles the resource
- **THEN** deletion cleanup proceeds normally (prune if enabled, then remove finalizer)

### Requirement: Partial cleanup failure blocks finalizer removal
If some resources fail to delete, the controller MUST NOT remove the finalizer.

#### Scenario: Failed cleanup retains finalizer
- **GIVEN** a ModuleRelease being deleted with inventory entries, where one resource fails to delete (e.g., RBAC insufficient)
- **WHEN** the controller reconciles the resource
- **THEN** successfully deletable resources are deleted
- **AND** the finalizer is NOT removed
- **AND** the controller requeues for retry

### Requirement: Finalizer retained on DeletionSAMissing stall
While a release is stalled with reason `DeletionSAMissing`, the finalizer MUST remain on the object. A deletion never enters this stall when its inventory holds nothing the cleanup would delete, or after it sent every delete (requirements "The cleanup finalizer follows the library's hold verdict" and "A wait survives the loss of the deleting identity"). The release remains blocked from garbage collection by the apiserver until either:

- The ServiceAccount is restored in the release's namespace and the next reconcile succeeds in pruning the inventory, OR
- The operator sets annotation `opm.dev/force-delete-orphan=true` on the release and the next reconcile removes the finalizer via the orphan-exit path, OR
- The operator sets `spec.prune=false` on the release and the next reconcile's deletion cleanup detects prune is disabled (existing behavior: orphan without SA impersonation).

#### Scenario: Release not garbage-collected while stalled on DeletionSAMissing
- **GIVEN** a ModuleRelease with a deletionTimestamp set and Ready condition False with reason `DeletionSAMissing`
- **WHEN** a caller queries the release via the K8s API
- **THEN** the release object still exists
- **AND** `metadata.finalizers` contains the controller's finalizer

### Requirement: Finalizer removed on orphan-exit
When the orphan annotation path executes, the finalizer MUST be removed in the same reconcile pass that emits the `OrphanedOnDeletion` event. The finalizer removal MUST happen even if the annotation was observed and handled without performing any delete API calls.

#### Scenario: Orphan-exit removes finalizer in single reconcile
- **GIVEN** a ModuleRelease stalled with `DeletionSAMissing` and the orphan annotation set
- **WHEN** the reconcile handling the annotation runs to completion
- **THEN** the OrphanedOnDeletion event is emitted
- **AND** the finalizer has been removed from `metadata.finalizers`
- **AND** no prune API calls were made against the impersonation SA or the controller client

### Requirement: Recovery signals of a stalled deletion trigger a reconcile

A `ModuleInstance` whose deletion is stalled with reason `DeletionSAMissing` MUST be reconciled promptly, not at the next stalled recheck, when either of these happens:

- its annotation `opm.dev/force-delete-orphan` becomes the literal `"true"`;
- a ServiceAccount with the name the instance impersonates is created in the instance's namespace.

The creation of the ServiceAccount is the only trigger of the second case. When the ServiceAccount is created before the RBAC that lets it delete the inventory, the reconcile it triggers is forbidden and the deletion stalls with `ImpersonationFailed` until the stalled recheck.

"Promptly" means that the event itself enqueues the instance. The reconcile that follows applies the existing deletion rules unchanged: the orphan-exit path for the annotation, the prune as the ServiceAccount for its return.

An annotation change on an instance that is not being deleted, and a change of any other annotation, MUST NOT trigger a reconcile. A status-only write MUST NOT trigger a reconcile, as before.

This requirement covers `ModuleInstance`. A `ModulePackage` stalled the same way still waits for its stalled recheck.

#### Scenario: Setting the orphan annotation releases a stalled deletion without waiting

- **GIVEN** a running controller and a `ModuleInstance` being deleted, stalled with reason `DeletionSAMissing`
- **WHEN** a user sets the annotation `opm.dev/force-delete-orphan=true` on it
- **THEN** the controller reconciles the instance within seconds
- **AND** the finalizer is removed and the apiserver deletes the instance

#### Scenario: The return of the ServiceAccount completes a stalled deletion without waiting

- **GIVEN** a running controller and a `ModuleInstance` being deleted, stalled with reason `DeletionSAMissing` on the ServiceAccount `deploy-sa`
- **WHEN** the ServiceAccount `deploy-sa` is created in the instance's namespace with the rights to delete the inventory
- **THEN** the controller reconciles the instance within seconds
- **AND** the inventory is pruned as that ServiceAccount, and once every object the cleanup deleted is gone the finalizer is removed and the apiserver deletes the instance

#### Scenario: An annotation change on a live instance is not a trigger

- **GIVEN** a `ModuleInstance` that is not being deleted
- **WHEN** its `opm.dev/force-delete-orphan` annotation is set, or any other annotation changes
- **THEN** that update alone enqueues no reconcile

#### Scenario: A ServiceAccount that returns before its RBAC does not complete the deletion

- **GIVEN** a running controller and a `ModuleInstance` being deleted, stalled with reason `DeletionSAMissing` on the ServiceAccount `deploy-sa`
- **WHEN** the ServiceAccount `deploy-sa` is created with no right to delete the inventory
- **THEN** the controller reconciles the instance within seconds and the instance stalls with reason `ImpersonationFailed`
- **AND** the finalizer stays, and a binding created afterwards takes effect at the next stalled recheck

### Requirement: A kept claim never holds the finalizer
A PersistentVolumeClaim that the deletion cleanup keeps MUST NOT keep the cleanup finalizer on the object and MUST NOT put the object into a stalled or failed state. The finalizer MUST be removed in the reconcile in which every other inventory entry is gone from the cluster or skipped (requirement "The finalizer is kept until the deleted objects are gone"). An inventory that holds only kept claims MUST release the finalizer without a delete, also when the deleting identity is missing (requirement "The cleanup finalizer follows the library's hold verdict"). A failure to delete another entry MUST still keep the finalizer, as before, and the retry MUST keep the claims again.

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

### Requirement: Deletion cleanup judges every object with the delete verdict
With `spec.prune` true, the deletion cleanup of a ModuleInstance or a ModulePackage MUST ask the library's delete verdict for every inventory entry and MUST delete an object only when the verdict says proceed, with a precondition on the UID of the object that was judged. It MUST judge with `status.instanceUUID`, and with `status.previousInstanceUUID` when it is set: an object that carries either identity is the instance's own. Source: 0012:D4:R1, 0012:D8:R8; owner decision of 2026-10-08 (both identities are kept until the prune succeeded).

With an empty `status.instanceUUID` the cleanup MUST ask the verdict with no identity. The verdict then compares no UUID label, and it leaves in place every object that carries an adopt annotation, also one that names this instance, because no identity is known to compare it with.

An object the verdict skips MUST be left in the cluster and MUST NOT hold the finalizer. In the scenarios below "the finalizer is removed" means: once every object the cleanup deleted is gone. A read that fails with an error other than NotFound, and a DELETE that fails, a DELETE refused on the UID precondition included, MUST hold the finalizer, and the cleanup MUST be retried. The rules for PersistentVolumeClaims and for a missing ServiceAccount are not changed.

#### Scenario: Object adopted by another instance survives the deletion
- **GIVEN** a ModuleInstance with `spec.prune=true` being deleted, whose inventory holds ConfigMap `team-a/shared` carrying the annotation `opmodel.dev/adopt` with the UUID of another instance, and Deployment `team-a/app` of its own
- **WHEN** the deletion cleanup runs
- **THEN** the Deployment is deleted and the ConfigMap still exists
- **AND** the finalizer is removed once the Deployment is gone

#### Scenario: Object of another instance survives the deletion
- **GIVEN** a ModuleInstance being deleted with `status.instanceUUID` `A` and no `status.previousInstanceUUID`, whose inventory holds a ConfigMap that carries the UUID label `B`
- **WHEN** the deletion cleanup runs
- **THEN** the ConfigMap still exists and the finalizer is removed in the same reconcile, because nothing was deleted

#### Scenario: Deletion before an identity change is settled removes the live workload
- **GIVEN** a ModuleInstance with `spec.prune=true`, `status.instanceUUID` `B` and `status.previousInstanceUUID` `A`, whose inventory holds Deployment `team-a/app`, already relabelled to `B`, and ConfigMap `team-a/old`, still labelled `A`
- **WHEN** the instance is deleted and the deletion cleanup runs
- **THEN** the Deployment and the ConfigMap are both deleted
- **AND** the finalizer is removed once both are gone

#### Scenario: Deletion with no recorded identity leaves an annotated object
- **GIVEN** a ModulePackage with `spec.prune=true` and no `status.instanceUUID` being deleted, whose inventory holds a ConfigMap that is managed by OPM and has no adopt annotation, and a Secret that carries an adopt annotation
- **WHEN** the deletion cleanup runs
- **THEN** the ConfigMap is deleted and the Secret still exists
- **AND** the finalizer is removed once the ConfigMap is gone

#### Scenario: A replaced object holds the finalizer for one more attempt
- **GIVEN** a deletion cleanup whose DELETE of an inventory object is refused on the UID precondition
- **WHEN** the reconcile ends
- **THEN** the finalizer is still present
- **AND** the next cleanup reads the object that now holds the name and judges it

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
The controller MUST remove the cleanup finalizer of a deleting ModuleInstance or ModulePackage only after the library's hold verdict (`lifecycle.MayReleaseHold`) said release for every plan of the cleanup, in this reconcile or in an earlier reconcile of the same deletion (requirement "A wait survives the loss of the deleting identity"). Source: 0012:D4:R1.

The verdict's inputs MUST be: prune from `spec.prune`; force-orphan true only when the annotation `opm.dev/force-delete-orphan` is the literal `"true"`; the deleting identity as available, missing (the ServiceAccount does not exist) or failed (any other impersonation error). The verdict MUST be asked with the plans as they are built, that is without the PersistentVolumeClaims that `spec.dataPolicy` keeps.

Each verdict MUST keep the status it has today: a missing identity without force-orphan stalls with `DeletionSAMissing`; a failed identity, and a step refused as Forbidden under impersonation, stall with `ImpersonationFailed`; any other failed step keeps the finalizer and is retried; force-orphan emits `OrphanedOnDeletion` and clears the inventory.

One outcome differs from the behaviour before this requirement. When the inventory holds only PersistentVolumeClaims that `spec.dataPolicy` keeps, the plan is empty and the verdict releases before it looks at the identity. The controller MUST then remove the finalizer without a read, also when the deleting identity is missing or failed, because the cleanup would delete nothing. It MUST NOT emit a `ClaimsKept` event for claims it did not read; it MUST emit one `Warning` event with reason `DeletionUnconfirmed` and action `Delete` that states the number of claims left in place without a read.

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
- **GIVEN** a ModuleInstance being deleted whose inventory holds a Deployment, whose impersonated ServiceAccount does not exist, without the orphan annotation, and whose `Ready` condition carries no deletion wait reason
- **WHEN** the controller reconciles it
- **THEN** no object is read or deleted, the finalizer stays and `Ready` is False with reason `DeletionSAMissing`

#### Scenario: Force-orphan lifts only a missing ServiceAccount
- **GIVEN** a ModuleInstance being deleted with the annotation `opm.dev/force-delete-orphan=true`, whose ServiceAccount exists and whose cleanup has a failed step
- **WHEN** the reconcile ends
- **THEN** the finalizer is still present

#### Scenario: Only kept claims and a missing ServiceAccount
- **GIVEN** a ModuleInstance with `spec.prune=true` and no `spec.dataPolicy` being deleted, whose inventory holds only PersistentVolumeClaims and whose ServiceAccount does not exist
- **WHEN** the controller reconciles it
- **THEN** no object is read or deleted, the claims still exist and the finalizer is removed
- **AND** one `Warning` event with reason `DeletionUnconfirmed` is emitted and no event with reason `ClaimsKept`

#### Scenario: A claim the instance may delete still needs the ServiceAccount
- **GIVEN** the same instance with `spec.dataPolicy=Delete`
- **WHEN** the controller reconciles it
- **THEN** the finalizer stays and `Ready` is False with reason `DeletionSAMissing`

### Requirement: The finalizer is kept until the deleted objects are gone
After a release verdict, the controller MUST remove the cleanup finalizer only when every object this cleanup deleted no longer exists: its read returns NotFound, or another object (a different UID) holds its name. A kept PersistentVolumeClaim, an object left behind and an object that was already absent MUST NOT be waited for. Source: owner decision of 2026-10-09 (the finalizer waits until the deleted objects are gone).

While at least one deleted object still exists, the reconcile MUST end without removing the finalizer, MUST set `Ready=False` and `Reconciling=True` with reason `DeletionInProgress`, and MUST request a requeue after an interval of at least 1 second and at most 60 seconds. It MUST NOT wait inside the reconcile. Each such reconcile MUST judge every inventory entry again from the cluster and MUST NOT rely on a stored list of what was deleted.

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

#### Scenario: Nothing left at the check releases in the same reconcile
- **GIVEN** a ModuleInstance being deleted whose one deleted ConfigMap no longer exists when the cleanup reads it again in the same reconcile
- **WHEN** the controller reconciles the instance
- **THEN** the finalizer is removed in that reconcile and `DeletionInProgress` is never set

#### Scenario: An object the garbage collector has not removed yet is waited for
- **GIVEN** a ModuleInstance being deleted whose one ConfigMap was deleted with Foreground propagation and still carries the `foregroundDeletion` finalizer when the cleanup reads it again
- **WHEN** the controller reconciles the instance
- **THEN** the finalizer stays, `Ready` is False with reason `DeletionInProgress`, and the reconcile asks for a requeue after 1 second

#### Scenario: A name taken by a new object does not hold the finalizer
- **GIVEN** a deleted ConfigMap whose name is held by a new object with another UID
- **WHEN** the controller checks the deleted objects
- **THEN** that ConfigMap counts as gone and the new object is not deleted

#### Scenario: Turning prune off releases a waiting deletion
- **GIVEN** a ModuleInstance being deleted with reason `DeletionInProgress`
- **WHEN** a user sets `spec.prune` to false
- **THEN** the next reconcile removes the finalizer and deletes nothing more

### Requirement: A wait survives the loss of the deleting identity
A deletion cleanup has sent every delete when one reconcile of it ended with a release verdict for every plan: each plan step ended as deleted (the API server accepted its DELETE) or as skipped, and no step failed. That reconcile records the fact by setting the `Ready` reason `DeletionInProgress`, or it removes the finalizer at once when nothing is left. The reason `DeletionBlocked` carries the same fact. No other field records it. Source: owner decision of 2026-10-09 (when the ServiceAccount or its permissions disappear during the wait, release the finalizer; the `Ready` reason is the record, with no new CRD field).

The controller MUST read the record only from the `Ready` condition the object carried when the reconcile started, and only on an object that has a `deletionTimestamp`. It MUST use the record for one decision: what to do when the deleting identity is gone. The identity is gone when the ServiceAccount does not exist (the API server answers NotFound), or when a read or a repeated delete the cleanup sends as that ServiceAccount is refused as Forbidden and the refusal is the ServiceAccount's own. A Forbidden answer alone does not show that: the API server answers a controller that may no longer impersonate the ServiceAccount with the same status. Before it treats a Forbidden answer as a lost identity, the controller MUST ask the API server, as itself, whether it may impersonate that ServiceAccount (a SelfSubjectAccessReview with the verb `impersonate` on the resource `serviceaccounts`, with its name and namespace), and MUST count the refusal as the ServiceAccount's only on a clear answer that it is allowed. The controller MUST decide all of this from typed API answers and MUST NOT read message text. The review MUST NOT need a rule in the role the operator ships for itself. While the identity is available and every read succeeds, the record MUST NOT change any outcome.

With the record present:

- When the ServiceAccount does not exist, the controller MUST remove the finalizer without a read or a delete, and MUST emit one `Warning` event with reason `DeletionUnconfirmed` and action `Delete` that states how many inventory objects it could not confirm as gone.
- When the identity is available, reads are refused as Forbidden and the refusal is the ServiceAccount's own, each entry whose read was refused counts as not confirmed and MUST NOT hold the finalizer. An object that was read and still exists MUST keep the wait, also when its repeated DELETE is refused as Forbidden. When no readable deleted object is left, the controller MUST remove the finalizer and emit the same event.
- Any other failure says nothing about the identity of the instance: a ServiceAccount that could not be looked up (a server error, a timeout, a throttle, a connection error, a cancelled context, a refusal of the controller's own read); a read or a delete of the cleanup that failed for such a cause, or that was answered Unauthorized (a 401 answers the controller's own credential, before the API server looks at the impersonation); and a Forbidden answer when the controller deletes as itself, when the review says that the controller may not impersonate the ServiceAccount, or when the review fails. The controller MUST keep the finalizer, MUST retry with its normal backoff, MUST NOT replace the wait reason with a stall reason (the wait reason is the record), and MUST say in the `Ready` message of that reconcile what could not be checked and why. Once the object has been deleting for 10 minutes by the controller's clock, the controller MUST report such a deletion with reason `DeletionBlocked` and `Stalled=True`, with one `Warning` event and the ways out, as it reports a deletion whose objects do not go. A DELETE refused as Forbidden of an object that is not being deleted MUST hold the finalizer as without the record.

Without the record, a missing or failed identity and a Forbidden step MUST hold the finalizer with `DeletionSAMissing` or `ImpersonationFailed`, however many deletes an earlier, failed reconcile already sent.

The controller MUST NOT set the reasons `DeletionInProgress` and `DeletionBlocked` anywhere else, and MUST NOT set them on an object that is not being deleted. The roles the operator ships MUST NOT grant a user a write verb on `moduleinstances/status` or `modulepackages/status`.

#### Scenario: ServiceAccount deleted during the wait
- **GIVEN** a ModuleInstance being deleted whose `Ready` condition carries the reason `DeletionInProgress` and whose Deployment is still terminating
- **WHEN** its ServiceAccount is deleted and the controller reconciles the instance
- **THEN** the finalizer is removed without a read or a delete
- **AND** one `Warning` event with reason `DeletionUnconfirmed` is emitted
- **AND** `Ready` never carries the reason `DeletionSAMissing`

#### Scenario: RBAC removed during the wait
- **GIVEN** the same instance, whose ServiceAccount exists and has lost every right
- **WHEN** the controller reconciles the instance, every read is refused as Forbidden, and the API server says that the controller may impersonate the ServiceAccount
- **THEN** the finalizer is removed and one `Warning` event with reason `DeletionUnconfirmed` is emitted
- **AND** `Ready` never carries the reason `ImpersonationFailed`

#### Scenario: A blocked deletion is released the same way
- **GIVEN** a ModuleInstance being deleted whose `Ready` condition carries the reason `DeletionBlocked`
- **WHEN** its ServiceAccount is deleted and the controller reconciles the instance
- **THEN** the finalizer is removed

#### Scenario: A failed lookup of the ServiceAccount during the wait keeps the finalizer
- **GIVEN** a ModuleInstance being deleted with reason `DeletionBlocked`, whose ServiceAccount exists
- **WHEN** the controller's read of the ServiceAccount fails with a server error or a timeout
- **THEN** the finalizer stays, `Ready` keeps the reason `DeletionBlocked` and says that the check failed, and the reconcile is retried
- **AND** no event with reason `DeletionUnconfirmed` is emitted

#### Scenario: A lost right to impersonate does not release
- **GIVEN** a ModuleInstance being deleted with reason `DeletionInProgress`, whose ServiceAccount exists and keeps its rights, and a controller that may no longer impersonate that ServiceAccount
- **WHEN** the controller reconciles the instance and its read as the ServiceAccount is refused as Forbidden
- **THEN** the controller asks whether it may impersonate the ServiceAccount, the answer is not "allowed", and the finalizer stays
- **AND** `Ready` keeps the reason `DeletionInProgress` and says that the controller may not impersonate the ServiceAccount

#### Scenario: An Unauthorized answer does not release
- **GIVEN** a ModuleInstance being deleted with reason `DeletionBlocked`
- **WHEN** a read of the cleanup is answered Unauthorized
- **THEN** the finalizer stays, `Ready` keeps the reason `DeletionBlocked`, and the reconcile is retried

#### Scenario: A wait whose rechecks keep failing is reported as blocked
- **GIVEN** a ModuleInstance being deleted with reason `DeletionInProgress`, whose rechecks fail with a server error
- **WHEN** the controller reconciles it before and after 10 minutes of deletion, by the controller's clock
- **THEN** the first reconcile keeps `DeletionInProgress` and its message says that the deleted objects could not be checked, with the cause
- **AND** the later one sets `DeletionBlocked` with `Stalled=True`, names `spec.prune=false` as a way out and emits one `Warning` event

#### Scenario: Identity lost before every delete was sent
- **GIVEN** a ModuleInstance being deleted whose first cleanup reconcile deleted one ConfigMap and failed on a second, so that `Ready` carries no deletion wait reason
- **WHEN** its ServiceAccount is deleted and the controller reconciles the instance
- **THEN** the finalizer stays and `Ready` is False with reason `DeletionSAMissing`

#### Scenario: A readable terminating object still holds
- **GIVEN** a ModuleInstance with reason `DeletionInProgress` whose ServiceAccount may read Deployments and may no longer delete them, and a Deployment that is still terminating
- **WHEN** the controller reconciles the instance
- **THEN** the finalizer stays and `Ready` keeps the reason `DeletionInProgress`

#### Scenario: The record is ignored on a live object
- **GIVEN** a ModuleInstance that is not being deleted and whose `Ready` condition was written by another client with the reason `DeletionInProgress`
- **WHEN** the controller reconciles it
- **THEN** the reconcile renders and applies as usual and replaces the condition

#### Scenario: The shipped roles cannot write the record
- **GIVEN** every role under `config/rbac` that the operator ships for users (the editor, admin and viewer roles)
- **WHEN** their rules are read
- **THEN** no rule grants `update` or `patch` on a `status` subresource

### Requirement: A deletion that does not finish is reported as blocked
When a deleted object still exists 10 minutes after its `deletionTimestamp`, by the controller's clock, the controller MUST set `Ready=False` and `Stalled=True` with reason `DeletionBlocked` on the deleting ModuleInstance or ModulePackage. The message MUST name each remaining object by kind, namespace and name, with its finalizers, and MUST name the ways out. The controller MUST keep the finalizer, MUST keep rechecking at an interval of at most 60 seconds, and MUST NOT give up or remove the finalizer on a timeout. An object that had been terminating for more than 10 minutes before the instance was deleted is reported as blocked by the first reconcile, with no `DeletionInProgress` before it.

The threshold and the clock MUST be values a test can set, so that this requirement is tested without a sleep. They MUST NOT be command-line flags.

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

#### Scenario: The threshold is reached without a sleep
- **GIVEN** a test whose controller clock is set 11 minutes ahead of the `deletionTimestamp` of a terminating Deployment
- **WHEN** the controller reconciles the deleting instance
- **THEN** the reason is `DeletionBlocked`
