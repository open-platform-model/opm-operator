## ADDED Requirements

### Requirement: Events emitted when objects are left behind
The controller MUST emit one `Warning` event with reason `LeftBehind` when a prune or a deletion cleanup leaves one or more objects in the cluster because the delete verdict skipped them, a Namespace or a CustomResourceDefinition included. The event MUST state the count and MUST carry, for each object, the message the library words for the skip, unchanged (at most ten objects, and fewer when the messages would take the event past 1024 characters; then the number of the rest). The `action` field MUST be `Prune` on the stale-prune path and `Delete` on the deletion path. On the deletion path the event MUST be emitted before the finalizer is removed. The event text MUST NOT carry an enhancement reference.

No `LeftBehind` event is emitted for an object that was already absent or for a kept PersistentVolumeClaim, which has its own event.

#### Scenario: An object of another instance left on prune
- **GIVEN** a ModuleInstance whose prune left ConfigMap `team-a/example` behind as another instance's
- **WHEN** the reconcile completes
- **THEN** a `Warning` event with reason `LeftBehind` and action `Prune` is emitted
- **AND** its message contains the library's message for `ConfigMap/team-a/example`

#### Scenario: Objects left on deletion
- **GIVEN** a ModuleInstance being deleted whose inventory holds a core Namespace and a ConfigMap annotated for another instance
- **WHEN** the deletion cleanup completes
- **THEN** a `Warning` event with reason `LeftBehind` and action `Delete` is emitted, naming both objects

#### Scenario: Nothing left, no event
- **GIVEN** a prune that deleted every stale object
- **WHEN** the reconcile completes
- **THEN** no event with reason `LeftBehind` is emitted

### Requirement: Events emitted when a recreate is refused
The controller MUST emit one `Warning` event with reason `RecreateRefused` and action `Apply` when an apply is refused because a forced recreate would delete an object the delete verdict skips. The message MUST be the message of the `Ready` condition.

#### Scenario: Refused recreate event
- **GIVEN** a ModuleInstance with `spec.rollout.forceConflicts: true` whose apply is refused for a Job annotated for another instance
- **WHEN** the reconcile completes
- **THEN** a `Warning` event with reason `RecreateRefused` and action `Apply` is emitted, naming the Job
