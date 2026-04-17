## Why

`controller-runtime` v0.23 deprecated `manager.GetEventRecorderFor` in favor of `GetEventRecorder`, which returns the new `client-go/tools/events.EventRecorder` (events.k8s.io/v1) instead of the legacy `client-go/tools/record.EventRecorder` (core/v1). The CI lint job fails today on `cmd/main.go:242` (staticcheck SA1019), and the legacy events API is scheduled for removal in a future controller-runtime release. Migrate now while scope is contained to one controller and one set of emission sites.

## What Changes

- Replace `mgr.GetEventRecorderFor(...)` with `mgr.GetEventRecorder(...)` in `cmd/main.go`.
- **BREAKING (internal)**: Change the `EventRecorder` field on `ModuleReleaseReconciler` from `record.EventRecorder` to `events.EventRecorder`.
- Rewrite the 9 emission call sites in `internal/reconcile/modulerelease.go` to use the new `Eventf(regarding, related, eventtype, reason, action, note, args...)` signature. Pass `nil` for `related`; choose stable `action` verbs per emission site (e.g. `Apply`, `Prune`, `Suspend`, `Resume`, `Render`, `Reconcile`).
- Migrate every test fake-recorder usage in `internal/controller/*_test.go` from `record.NewFakeRecorder(N)` to `events.NewFakeRecorder(N)` and update assertion helpers for the new event-message format (events.k8s.io fakes format messages differently).
- Update imports across `cmd/`, `internal/controller/`, `internal/reconcile/`, and tests.
- No CRD changes, no RBAC changes, no API surface changes for end users.

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities
- `events-emission`: Emitted events now carry an `action` field (events.k8s.io/v1 schema) in addition to existing `type`/`reason`/`message`. Reason vocabulary is unchanged; action vocabulary is additive and stable.

## Impact

- **Code**: `cmd/main.go`, `internal/controller/modulerelease_controller.go`, `internal/reconcile/modulerelease.go`, `internal/controller/modulerelease_reconcile_test.go`, `internal/controller/modulerelease_controller_test.go`.
- **Dependencies**: No new modules; switches usage from `k8s.io/client-go/tools/record` to `k8s.io/client-go/tools/events` (already transitively present).
- **Observability**: Consumers reading events via `kubectl get events` or events.k8s.io/v1 API will now see an `action` field populated. Legacy core/v1 Event objects continue to be written by the broadcaster's bridge.
- **SemVer**: PATCH — internal refactor; no external API contract change.
- **CI**: Unblocks the failing `task dev:lint` job (staticcheck SA1019).
