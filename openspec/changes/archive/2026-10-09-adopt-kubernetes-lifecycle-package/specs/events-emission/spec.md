## ADDED Requirements

### Requirement: Events emitted while a deletion waits
The controller MUST emit one Normal event with reason `DeletionInProgress` and action `Delete` when a deletion cleanup starts to wait for deleted objects, and one Warning event with reason `DeletionBlocked` and action `Delete` when the wait becomes blocked. Each event MUST carry the message of the condition. The controller MUST NOT repeat either event at a later reconcile while `Ready` already carries that reason. A deletion that is blocked at its first reconcile emits the `DeletionBlocked` event only.

#### Scenario: One event when the wait starts
- **GIVEN** a deleting ModuleInstance whose cleanup deleted a Deployment that still exists
- **WHEN** three reconciles run while it still exists
- **THEN** exactly one Normal event with reason `DeletionInProgress` was emitted

#### Scenario: One warning when the wait is blocked
- **GIVEN** a deleting ModuleInstance that has reason `DeletionInProgress` and whose Deployment passes 10 minutes of termination
- **WHEN** the next reconciles run
- **THEN** exactly one Warning event with reason `DeletionBlocked` was emitted, and it names the Deployment

### Requirement: Kept claims are reported once per deletion
On the deletion path the `ClaimsKept` event MUST be emitted by the first reconcile of the cleanup that reaches a release verdict: the one that removes the finalizer, or the one that starts to wait for deleted objects. A later reconcile of the same wait MUST NOT emit it again.

#### Scenario: A waiting deletion reports its kept claims once
- **GIVEN** a ModuleInstance being deleted whose inventory holds a PersistentVolumeClaim that is kept and a Deployment that takes three rechecks to disappear
- **WHEN** the deletion completes
- **THEN** exactly one event with reason `ClaimsKept` and action `Delete` was emitted

### Requirement: Events emitted when a deletion is released without confirmation
The controller MUST emit one Warning event with reason `DeletionUnconfirmed` and action `Delete` in the reconcile that removes the cleanup finalizer without having read every object: after the ServiceAccount was deleted during the wait, and for an inventory of kept claims only whose identity is missing or failed. The message MUST state the number of objects that were not read, MUST say why (the ServiceAccount is missing or cannot be impersonated), and MUST say that the objects may still exist. It MUST be emitted before the finalizer is removed. The event text MUST NOT carry an enhancement reference.

#### Scenario: Released after the ServiceAccount went
- **GIVEN** a ModuleInstance with reason `DeletionInProgress` and three inventory objects, whose ServiceAccount no longer exists
- **WHEN** the controller reconciles it
- **THEN** one Warning event with reason `DeletionUnconfirmed` is emitted that names the count 3 and the missing ServiceAccount

#### Scenario: A confirmed deletion emits no such event
- **GIVEN** a deletion whose every deleted object was read as gone
- **WHEN** the finalizer is removed
- **THEN** no event with reason `DeletionUnconfirmed` is emitted

## MODIFIED Requirements

### Requirement: Events emitted when objects are left behind
The controller MUST emit one event with reason `LeftBehind` when a prune or a deletion cleanup leaves one or more objects in the cluster because the delete verdict skipped them. The event type MUST be `Normal` when every object left is a Namespace or a CustomResourceDefinition that OPM never deletes, and `Warning` when at least one object was left because it is not managed by OPM, belongs to another instance or is adopted by another instance. The event MUST state the count and MUST carry, for each object, the message the library words for the skip, unchanged (at most ten objects, and fewer when the messages would take the event past 1024 characters; then the number of the rest), with the objects left for an ownership reason first. The `action` field MUST be `Prune` on the stale-prune path and `Delete` on the deletion path. The event text MUST NOT carry an enhancement reference.

The event MUST be emitted only by a run whose result is committed: on the stale-prune path by a prune that returned no error, and on the deletion path by the first reconcile of the cleanup that reaches a release verdict: the one that removes the finalizer, or the one that starts to wait for deleted objects. A later reconcile of the same wait MUST NOT emit it again. A prune or a cleanup that failed for another entry MUST NOT emit it, because its retry judges the same entries again.

No `LeftBehind` event is emitted for an object that was already absent or for a kept PersistentVolumeClaim, which has its own event.

#### Scenario: An object of another instance left on prune
- **GIVEN** a ModuleInstance whose prune left ConfigMap `team-a/example` behind as another instance's
- **WHEN** the reconcile completes
- **THEN** a `Warning` event with reason `LeftBehind` and action `Prune` is emitted
- **AND** its message contains the library's message for `ConfigMap/team-a/example`

#### Scenario: Only a Namespace left on deletion
- **GIVEN** a ModuleInstance being deleted whose inventory holds a core Namespace and a Deployment of its own
- **WHEN** the deletion cleanup completes
- **THEN** a `Normal` event with reason `LeftBehind` and action `Delete` is emitted, naming the Namespace

#### Scenario: A Namespace and an adopted object left on deletion
- **GIVEN** a ModuleInstance being deleted whose inventory holds a core Namespace and a ConfigMap annotated for another instance
- **WHEN** the deletion cleanup completes
- **THEN** a `Warning` event with reason `LeftBehind` and action `Delete` is emitted, naming both objects, the ConfigMap first

#### Scenario: A failed prune reports nothing
- **GIVEN** a prune that left one object behind and failed to delete another
- **WHEN** the reconcile ends
- **THEN** no event with reason `LeftBehind` is emitted
- **AND** the reconcile that later prunes with success emits one

#### Scenario: Nothing left, no event
- **GIVEN** a prune that deleted every stale object
- **WHEN** the reconcile completes
- **THEN** no event with reason `LeftBehind` is emitted

#### Scenario: A waiting deletion reports what it left once
- **GIVEN** a ModuleInstance being deleted whose inventory holds a core Namespace and a Deployment that takes three rechecks to disappear
- **WHEN** the deletion completes
- **THEN** exactly one event with reason `LeftBehind` and action `Delete` was emitted
