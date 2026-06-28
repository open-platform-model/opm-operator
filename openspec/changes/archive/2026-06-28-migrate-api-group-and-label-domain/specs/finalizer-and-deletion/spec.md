## MODIFIED Requirements

### Requirement: Finalizer registration
The controller MUST add the finalizer `opmodel.dev/cleanup` to a ModuleInstance during Phase 0 if it is not already present. The reconcile call that adds the finalizer MUST request an immediate requeue (`ctrl.Result{Requeue: true}`) rather than relying on the watch event produced by the finalizer patch — that watch event is filtered by the controller's `predicate.GenerationChangedPredicate`, because finalizer patches modify `metadata.finalizers` but do not bump `metadata.generation`. Without an explicit requeue, the next reconcile would be deferred to the periodic resync (default 10h), leaving freshly-created ModuleInstances without status conditions for an unacceptable duration.

#### Scenario: First reconcile adds finalizer
- **GIVEN** a ModuleInstance without the `opmodel.dev/cleanup` finalizer
- **WHEN** the controller reconciles the resource
- **THEN** the finalizer is added to `metadata.finalizers`

#### Scenario: Subsequent reconciles preserve finalizer
- **GIVEN** a ModuleInstance that already has the `opmodel.dev/cleanup` finalizer
- **WHEN** the controller reconciles the resource
- **THEN** the finalizer remains unchanged

#### Scenario: Finalizer-add reconcile requests immediate requeue
- **GIVEN** a freshly-created ModuleInstance without the `opmodel.dev/cleanup` finalizer
- **WHEN** the controller's first `Reconcile` call adds the finalizer
- **THEN** the reconcile returns a result with `Requeue=true` so the workqueue re-enqueues the request directly (bypassing the predicate)
- **AND** the next reconcile starts within the rate limiter's normal cadence (typically under one second), not the periodic resync window

#### Scenario: Status conditions appear after creation without manual triggers
- **GIVEN** a ModuleInstance created via the API server
- **WHEN** the controller observes the create event and reconciles via the manager
- **THEN** within a small bounded window (e.g., 10 seconds in envtest, single-digit seconds in production) `status.conditions` contains at least one of `Ready`, `Reconciling`, or `Stalled`
- **AND** no manual `kubectl edit`, periodic-resync, or other external trigger is required to produce these conditions

### Requirement: Deletion cleanup with prune enabled
When a ModuleInstance with `spec.prune=true` is deleted, the controller MUST delete all resources listed in `status.inventory.entries`, respecting safety exclusions.

#### Scenario: Delete all owned resources on CR deletion
- **GIVEN** a ModuleInstance with `spec.prune=true`, a non-zero `DeletionTimestamp`, and inventory entries for ConfigMap `foo` and Deployment `bar`
- **WHEN** the controller reconciles the resource
- **THEN** ConfigMap `foo` and Deployment `bar` are deleted from the cluster
- **AND** the `opmodel.dev/cleanup` finalizer is removed
- **AND** the ModuleInstance deletion completes

#### Scenario: Safety exclusions during deletion
- **GIVEN** a ModuleInstance with `spec.prune=true`, a non-zero `DeletionTimestamp`, and inventory entries including a Namespace and a CRD
- **WHEN** the controller reconciles the resource
- **THEN** the Namespace and CRD are NOT deleted
- **AND** all other inventory entries are deleted
- **AND** the finalizer is removed

### Requirement: Deletion with prune disabled orphans resources
When a ModuleInstance with `spec.prune=false` is deleted, the controller MUST remove the finalizer without deleting any resources.

#### Scenario: Orphan resources when prune is false
- **GIVEN** a ModuleInstance with `spec.prune=false` and a non-zero `DeletionTimestamp`
- **WHEN** the controller reconciles the resource
- **THEN** no resources are deleted
- **AND** the `opmodel.dev/cleanup` finalizer is removed
- **AND** the ModuleInstance deletion completes

### Requirement: Suspend does not block deletion
The controller MUST perform deletion cleanup even when `spec.suspend=true`.

#### Scenario: Cleanup proceeds despite suspend
- **GIVEN** a ModuleInstance with `spec.suspend=true` and a non-zero `DeletionTimestamp`
- **WHEN** the controller reconciles the resource
- **THEN** deletion cleanup proceeds normally (prune if enabled, then remove finalizer)

### Requirement: Partial cleanup failure blocks finalizer removal
If some resources fail to delete, the controller MUST NOT remove the finalizer.

#### Scenario: Failed cleanup retains finalizer
- **GIVEN** a ModuleInstance being deleted with inventory entries, where one resource fails to delete (e.g., RBAC insufficient)
- **WHEN** the controller reconciles the resource
- **THEN** successfully deletable resources are deleted
- **AND** the finalizer is NOT removed
- **AND** the controller requeues for retry

### Requirement: Finalizer retained on DeletionSAMissing stall
While an instance is stalled with reason `DeletionSAMissing`, the finalizer MUST remain on the object. The instance remains blocked from garbage collection by the apiserver until either:

- The ServiceAccount is restored in the instance's namespace and the next reconcile succeeds in pruning the inventory, OR
- The operator sets annotation `opm.dev/force-delete-orphan=true` on the instance and the next reconcile removes the finalizer via the orphan-exit path, OR
- The operator sets `spec.prune=false` on the instance and the next reconcile's deletion cleanup detects prune is disabled (existing behavior: orphan without SA impersonation).

#### Scenario: Instance not garbage-collected while stalled on DeletionSAMissing
- **GIVEN** a ModuleInstance with a deletionTimestamp set and Ready condition False with reason `DeletionSAMissing`
- **WHEN** a caller queries the instance via the K8s API
- **THEN** the instance object still exists
- **AND** `metadata.finalizers` contains the controller's finalizer

### Requirement: Finalizer removed on orphan-exit
When the orphan annotation path executes, the finalizer MUST be removed in the same reconcile pass that emits the `OrphanedOnDeletion` event. The finalizer removal MUST happen even if the annotation was observed and handled without performing any delete API calls.

#### Scenario: Orphan-exit removes finalizer in single reconcile
- **GIVEN** a ModuleInstance stalled with `DeletionSAMissing` and the orphan annotation set
- **WHEN** the reconcile handling the annotation runs to completion
- **THEN** the OrphanedOnDeletion event is emitted
- **AND** the finalizer has been removed from `metadata.finalizers`
- **AND** no prune API calls were made against the impersonation SA or the controller client
