## MODIFIED Requirements

### Requirement: Re-enqueue ModuleInstances when the platform becomes ready

The ModuleInstance reconciler SHALL watch the `Platform`. It SHALL re-enqueue ModuleInstances on a Platform update only when a field that a ModuleInstance render consumes differs between the old and the new object. Those fields are:

- the `Ready` condition's status or reason
- the pin set: `status.packageIdentity` or `status.registry`
- `spec.skewPolicy`
- `metadata.generation` or `status.observedGeneration`

The trigger includes the Platform reconciler's own status update, which does not bump the Platform's generation. A Platform update that changes none of these fields SHALL NOT enqueue any ModuleInstance. Examples of such updates are a change to the `Ready` message alone, an `operatorVersion` stamp, or a `ContractsFulfilled` update. Platform create and delete events SHALL re-enqueue.

The reconciler SHALL enqueue only the ModuleInstances that render against the changed Platform: those that are operator-managed (`spec.owner` absent, empty or `operator`) and not suspended. A ModuleInstance with `spec.owner: cli` or `spec.suspend: true` SHALL NOT be enqueued by a Platform event; a change to either field is a spec change and reconciles the instance through its own watch.

`status.packageIdentity` is the field that identifies the pin set an instance renders against.

#### Scenario: Blocked instances retry when the platform is generated

- **WHEN** a `ModuleInstance` is blocked with `PlatformNotReady` and a Platform is then applied and reaches `Generated`
- **THEN** the reconciler re-enqueues the instance and renders it on the next reconcile

#### Scenario: Instances re-render under a new pin set

- **WHEN** the Platform's `status.packageIdentity` changes because a claim became active, and the Platform's generation is unchanged
- **THEN** the reconciler re-enqueues every operator-managed, unsuspended `ModuleInstance`

#### Scenario: A message-only status write enqueues nothing

- **WHEN** the Platform reconciler rewrites the `Ready` condition's message, and its status, reason, `packageIdentity`, `registry` and generation are unchanged
- **THEN** no `ModuleInstance` is enqueued and no render runs

#### Scenario: CLI-owned and suspended instances are not enqueued

- **GIVEN** one `ModuleInstance` with `spec.owner: cli`, one with `spec.suspend: true` and one operator-managed instance
- **WHEN** the Platform's `Ready` condition moves to `True` with reason `Generated`
- **THEN** only the operator-managed instance is enqueued
