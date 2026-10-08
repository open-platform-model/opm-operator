## ADDED Requirements

### Requirement: The deletion wait reasons
The `internal/status` package MUST define the reasons `DeletionInProgress` and `DeletionBlocked` for the `Ready` condition of a ModuleInstance or a ModulePackage that is being deleted, and the event reason `DeletionUnconfirmed`.

`DeletionInProgress` MUST be set with `Ready=False` and `Reconciling=True`, never with `Stalled=True`: the cleanup sent every delete and waits for the objects to be gone. `DeletionBlocked` MUST be set with `Ready=False` and `Stalled=True`: an object has not gone for 10 minutes. Both messages MUST name the remaining objects and their finalizers; the `DeletionBlocked` message MUST also name the ways out.

A message MUST list at most ten objects and say how many more remain. For each object it MUST name at most three finalizers and count the rest, and it MUST name a finalizer other than `foregroundDeletion` first: `foregroundDeletion` is the garbage collector's and is set again by every repeated delete, so it is rarely what holds the object. When `foregroundDeletion` is the only finalizer, the message MUST say that the object waits for its dependents.

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

#### Scenario: The finalizer that holds the object is named first
- **GIVEN** a terminating object with the finalizers `foregroundDeletion`, `example.com/a`, `example.com/b`, `example.com/c` and `example.com/d`
- **WHEN** the message is built
- **THEN** it names `example.com/a`, `example.com/b` and `example.com/c` and says that two more finalizers are set

#### Scenario: Only the collector's finalizer
- **GIVEN** a terminating Deployment whose only finalizer is `foregroundDeletion`
- **WHEN** the message is built
- **THEN** it says that the Deployment waits for its dependents to be deleted
