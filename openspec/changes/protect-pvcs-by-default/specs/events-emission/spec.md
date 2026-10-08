## ADDED Requirements

### Requirement: Events emitted when claims are kept
The controller MUST emit one `Normal` event with reason `ClaimsKept` when a prune or a deletion cleanup keeps one or more PersistentVolumeClaims. The event MUST state the count, name the kept claims as `<namespace>/<name>` (at most ten, and fewer when the names would take the message past 1024 characters; then the number of the rest), say that the claims are no longer tracked, and say how to delete a claim. The `action` field MUST be `Prune` on the stale-prune path and `Delete` on the deletion path. On the deletion path the event MUST be emitted before the finalizer is removed. The event text MUST NOT carry an enhancement reference.

No `ClaimsKept` event is emitted when nothing was kept: when `spec.prune` is not true, when `spec.dataPolicy` is `Delete`, or when the stale set or the inventory holds no live claim owned by the instance.

#### Scenario: Kept claims on prune
- **GIVEN** a ModuleInstance with `spec.prune=true` whose prune kept PersistentVolumeClaim `media/old-cache`
- **WHEN** the reconcile completes
- **THEN** a `Normal` event with reason `ClaimsKept` and action `Prune` is emitted
- **AND** its message contains `media/old-cache` and the count 1

#### Scenario: Kept claims on deletion
- **GIVEN** a ModuleInstance with `spec.prune=true` being deleted, whose inventory holds PersistentVolumeClaims `media/config` and `media/cache`
- **WHEN** the deletion cleanup completes
- **THEN** a `Normal` event with reason `ClaimsKept` and action `Delete` is emitted, naming both claims

#### Scenario: Nothing kept, no event
- **GIVEN** a ModuleInstance with `spec.prune=true` and `spec.dataPolicy=Delete` whose prune deleted a stale claim
- **WHEN** the reconcile completes
- **THEN** no event with reason `ClaimsKept` is emitted

#### Scenario: More than ten kept claims
- **GIVEN** a deletion cleanup that keeps twelve claims
- **WHEN** the event is emitted
- **THEN** its message names ten claims and says that two more were kept
