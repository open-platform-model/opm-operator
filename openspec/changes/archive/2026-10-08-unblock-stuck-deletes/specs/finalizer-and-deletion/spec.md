## ADDED Requirements

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
