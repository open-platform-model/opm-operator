## Purpose

Define the Kubernetes events the controllers emit across the reconcile phases:
which outcomes produce a `Normal` or `Warning` event, their reasons and action
verbs, the events.k8s.io/v1 recorder they go through, and when a successful
render's advisory findings are surfaced as events.

## Requirements

### Requirement: Events emitted on successful apply
The controller MUST emit a `Normal` event when apply succeeds.

#### Scenario: Apply success event
- **GIVEN** a ModuleRelease whose reconcile reaches Phase 5 and applies resources
- **WHEN** apply completes successfully
- **THEN** a `Normal` event with reason `Applied` is emitted
- **AND** the event message includes the count of created/updated/unchanged resources

### Requirement: Events emitted on apply failure
The controller MUST emit a `Warning` event when apply fails.

#### Scenario: Apply failure event
- **GIVEN** a ModuleRelease whose Phase 5 apply fails
- **WHEN** the reconcile completes
- **THEN** a `Warning` event with reason `ApplyFailed` is emitted
- **AND** the event message includes the error description

### Requirement: Events emitted on prune
The controller MUST emit a `Normal` event when prune succeeds.

#### Scenario: Prune success event
- **GIVEN** a ModuleRelease with stale resources pruned in Phase 6
- **WHEN** prune completes successfully
- **THEN** a `Normal` event with reason `Pruned` is emitted with deleted count

### Requirement: Events emitted on source not ready
The controller MUST emit a `Warning` event when the source is not ready.

#### Scenario: Source not ready event
- **GIVEN** a ModuleRelease whose OCIRepository source is not ready
- **WHEN** Phase 1 detects the source is not ready
- **THEN** a `Warning` event with reason `SourceNotReady` is emitted

### Requirement: Events emitted on render failure
The controller MUST emit a `Warning` event when CUE rendering fails.

#### Scenario: Render failure event
- **GIVEN** a ModuleRelease whose CUE evaluation fails in Phase 3
- **WHEN** the reconcile completes
- **THEN** a `Warning` event with reason `RenderFailed` is emitted

### Requirement: Events emitted on suspend/resume
The controller MUST emit `Normal` events when entering or exiting suspend.

#### Scenario: Suspend event
- **GIVEN** a ModuleRelease with `spec.suspend=true`
- **WHEN** the controller reconciles and detects suspend
- **THEN** a `Normal` event with reason `Suspended` is emitted

### Requirement: Events emitted on overall success
The controller MUST emit a `Normal` event on full reconcile success.

#### Scenario: Reconciliation succeeded event
- **GIVEN** a ModuleRelease whose full reconcile (phases 0-7) completes successfully
- **WHEN** Phase 7 commits status
- **THEN** a `Normal` event with reason `ReconciliationSucceeded` is emitted

### Requirement: Events carry a stable action verb
Every event emitted by the controller MUST include a non-empty `action` field (events.k8s.io/v1) drawn from a fixed vocabulary tied to the reconcile phase rather than the outcome.

#### Scenario: Apply phase events share the Apply action
- **GIVEN** a ModuleRelease whose Phase 5 emits either `Applied` or `ApplyFailed`
- **WHEN** the event is observed via the events.k8s.io/v1 API
- **THEN** the `action` field equals `Apply` for both success and failure

#### Scenario: Prune phase events share the Prune action
- **GIVEN** a ModuleRelease whose Phase 6 emits either `Pruned` or `PruneFailed`
- **WHEN** the event is observed
- **THEN** the `action` field equals `Prune`

#### Scenario: Suspend and resume use distinct actions
- **GIVEN** a ModuleRelease entering or exiting suspend
- **WHEN** the corresponding `Suspended` or `Resumed` event is emitted
- **THEN** the `action` field equals `Suspend` or `Resume` respectively

#### Scenario: NoOp and overall success use the Reconcile action
- **GIVEN** a ModuleRelease whose reconcile completes with no drift, or completes Phase 7 successfully
- **WHEN** the corresponding `NoOp` or `ReconciliationSucceeded` event is emitted
- **THEN** the `action` field equals `Reconcile`

#### Scenario: Render-phase warnings use the Render action
- **GIVEN** a ModuleRelease whose Phase 1–4 emits `SourceNotReady`, `RenderFailed`, or a comparable warning
- **WHEN** the event is emitted
- **THEN** the `action` field equals `Render`

### Requirement: Controller uses the events.k8s.io/v1 EventRecorder
The controller MUST obtain its event recorder from `manager.GetEventRecorder` and emit events via the `client-go/tools/events.EventRecorder` interface; the legacy `client-go/tools/record.EventRecorder` MUST NOT be referenced from controller production code.

#### Scenario: No legacy event recorder references in production code
- **WHEN** `golangci-lint` runs against `cmd/` and `internal/`
- **THEN** no `staticcheck` SA1019 warning is reported for `GetEventRecorderFor`
- **AND** no production source file imports `k8s.io/client-go/tools/record`

### Requirement: Render warnings are emitted as events on transition

When a render succeeds with advisory findings (catalog skew under the `Warn` policy, unhandled optional traits), the reconciler SHALL emit one Warning event per distinct finding with reason `RenderWarning` and action `Render`, and SHALL emit them only when the object's set of findings changes between reconciles, not on every reconcile. A render with no findings SHALL emit none.

The event text SHALL be authored by the operator from the render's advisory diagnostic rows; the render carries no message string to pass through. The transition check SHALL be keyed on the facts those rows carry — for skew, the path and both versions; for an unhandled trait, the component and the trait — so that rewording an event does not present an unchanged finding set as changed.

#### Scenario: Skew under Warn is reported once

- **WHEN** a ModuleInstance's module requires a newer catalog build than the platform pins and the policy is `Warn`
- **THEN** the instance renders, reaches `Ready=True`, and one Warning event names the path and both versions

#### Scenario: Unchanged warnings do not repeat

- **WHEN** the same instance reconciles again with the same warnings
- **THEN** no new warning event is emitted

#### Scenario: Rewording does not re-emit

- **WHEN** the operator's warning wording changes while an object's advisory rows are unchanged between reconciles
- **THEN** no new warning event is emitted for that object

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

### Requirement: Events emitted when a claim recreate is refused
The controller MUST emit one `Warning` event with reason `ClaimConflict` and action `Apply` when an apply refused the forced recreate of a PersistentVolumeClaim. The event MUST name the claim as `<namespace>/<name>` and carry the same text as the `Ready` condition; it therefore names the refused field when the check before the apply refused the claim, and no field when the resource manager's delete guard kept it. The controller MUST NOT also emit an `ApplyFailed` event for that apply. The event text MUST NOT carry an enhancement reference.

#### Scenario: Refused claim recreate
- **GIVEN** a ModuleInstance with `spec.rollout.forceConflicts: true` and no `spec.dataPolicy`, whose render changes an immutable field of the live PersistentVolumeClaim `media/config`
- **WHEN** the reconcile completes
- **THEN** a `Warning` event with reason `ClaimConflict` and action `Apply` is emitted
- **AND** its message contains `media/config` and the field `spec`
- **AND** no `ApplyFailed` event is emitted

#### Scenario: Claim kept by the delete guard
- **GIVEN** an apply whose staged apply reached for the delete of the PersistentVolumeClaim `media/config` after its check passed
- **WHEN** the reconcile completes
- **THEN** a `Warning` event with reason `ClaimConflict` and action `Apply` is emitted
- **AND** its message contains `media/config`, names no refused field and does not say that nothing was applied

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
