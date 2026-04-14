## Why

The controller currently emits no Kubernetes Events. Events are the primary mechanism for operators to see what happened via `kubectl describe`. Without events, operators must read controller logs or inspect status conditions to understand reconcile activity. Events provide immediate, resource-scoped visibility into apply outcomes, failures, prune actions, and stalled conditions.

## What Changes

- Add `record.EventRecorder` to the reconciler struct.
- Emit `Normal` events on: successful apply, successful prune, no-op reconcile, suspend/resume.
- Emit `Warning` events on: source not ready, artifact fetch failure, render failure, apply failure, prune failure, stalled condition.
- Event reasons match the condition reason constants from change 07.
- Event messages are concise and actionable.

## Capabilities

### New Capabilities
- `events-emission`: Kubernetes Event emission for reconcile lifecycle milestones and failures.

### Modified Capabilities

## Impact

- `internal/controller/modulerelease_controller.go` — Add `EventRecorder` field, pass to reconciler.
- `internal/reconcile/modulerelease.go` — Emit events at key points in the reconcile loop.
- `cmd/main.go` — Wire event recorder from manager into controller.
- No API changes. Events are a Kubernetes core mechanism, not CRD-specific.
- SemVer: MINOR — new capability.
