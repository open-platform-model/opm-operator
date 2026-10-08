## ADDED Requirements

### Requirement: The deletion wait reasons
The `internal/status` package MUST define the reasons `DeletionInProgress` and `DeletionBlocked` for the `Ready` condition of a ModuleInstance or a ModulePackage that is being deleted.

`DeletionInProgress` MUST be set with `Ready=False` and `Reconciling=True`, never with `Stalled=True`: the cleanup deleted objects and waits for them to be gone. `DeletionBlocked` MUST be set with `Ready=False` and `Stalled=True`: an object has not gone for 10 minutes. Both messages MUST name the remaining objects and their finalizers; the `DeletionBlocked` message MUST also name the ways out. A message MUST list at most ten objects and say how many more remain.

Neither reason MUST appear on an object that is not being deleted.

#### Scenario: Waiting is not stalled
- **GIVEN** a deleting ModuleInstance whose deleted Deployment still exists and was deleted one minute ago
- **WHEN** the reconcile ends
- **THEN** `Ready` is False with reason `DeletionInProgress`, `Reconciling` is True and `Stalled` is absent or False

#### Scenario: Blocked is stalled and names the way out
- **GIVEN** a deleting ModuleInstance whose deleted Deployment has existed for more than 10 minutes
- **WHEN** the reconcile ends
- **THEN** `Ready` is False with reason `DeletionBlocked` and `Stalled` is True
- **AND** the message names the object, its finalizers, and `spec.prune=false` as a way to release the instance

#### Scenario: A long list is cut
- **GIVEN** a deleting ModuleInstance with fifteen deleted objects that still exist
- **WHEN** the reconcile ends
- **THEN** the message names ten of them and says that five more remain
