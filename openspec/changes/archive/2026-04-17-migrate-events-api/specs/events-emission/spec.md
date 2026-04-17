## ADDED Requirements

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
