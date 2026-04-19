## ADDED Requirements

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
