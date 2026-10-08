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
While a release is stalled with reason `DeletionSAMissing`, the finalizer MUST remain on the object. The release remains blocked from garbage collection by the apiserver until either:

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
- **AND** the inventory is pruned as that ServiceAccount, the finalizer is removed and the apiserver deletes the instance

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
