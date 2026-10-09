## ADDED Requirements

### Requirement: Events emitted when objects are left behind
The controller MUST emit one event with reason `LeftBehind` when a prune or a deletion cleanup leaves one or more objects in the cluster because the delete verdict skipped them. The event type MUST be `Normal` when every object left is a Namespace or a CustomResourceDefinition that OPM never deletes, and `Warning` when at least one object was left because it is not managed by OPM, belongs to another instance or is adopted by another instance. The event MUST state the count and MUST carry, for each object, the message the library words for the skip, unchanged (at most ten objects, and fewer when the messages would take the event past 1024 characters; then the number of the rest), with the objects left for an ownership reason first. The `action` field MUST be `Prune` on the stale-prune path and `Delete` on the deletion path. The event text MUST NOT carry an enhancement reference.

The event MUST be emitted only by a run whose result is committed: on the stale-prune path by a prune that returned no error, and on the deletion path by the cleanup that removes the finalizer, before it removes it. A prune or a cleanup that failed for another entry MUST NOT emit it, because its retry judges the same entries again.

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

### Requirement: Events emitted when an identity change is refused
The controller MUST emit one `Warning` event with reason `IdentityChangeUnsettled` and action `Reconcile` when it refuses a second change of the instance identity. The message MUST be the message of the `Ready` condition. It MUST NOT emit the event again while `Ready` already carries that reason.

#### Scenario: Refused identity change event
- **GIVEN** a ModuleInstance whose render carries a third identity while an identity change is not settled
- **WHEN** the controller reconciles twice
- **THEN** one `Warning` event with reason `IdentityChangeUnsettled` is emitted
