# platform-gated-rendering

## Purpose

ModuleRelease rendering is gated on a generated platform: the reconciler renders through the kernel-backed renderer against the platform held in the platform store, blocks inertly when no platform is generated, retries promptly when the platform becomes ready, and no longer derives rendering output from the startup-loaded provider.

## Requirements

### Requirement: ModuleRelease renders through the kernel against the generated platform

The ModuleInstance reconciler SHALL render via the kernel-backed renderer through the single-build render, against the generated platform module the store records: the renderer takes a lease on the record, renders with the instance's staged source and the platform's on-disk module, and releases the lease when the render returns. Successful rendering SHALL apply the resulting resources through the existing apply/inventory/prune path unchanged.

#### Scenario: ModuleRelease renders and applies when a platform is generated

- **WHEN** the `cluster` Platform is `Ready` (reason `Generated`) and a `ModuleInstance` referencing a resolvable module is applied
- **THEN** the reconciler renders through the single-build render against the recorded module and applies the rendered resources as before

### Requirement: Block ModuleRelease when no platform is generated

When the store holds no generated-module record, the reconciler SHALL set the ModuleInstance `Ready=False` with reason `PlatformNotReady`, apply nothing, prune nothing, emit a warning event and requeue.

#### Scenario: No platform present blocks the release inertly

- **WHEN** a `ModuleInstance` is applied while the Platform has not been generated and built
- **THEN** its status carries `Ready=False` with reason `PlatformNotReady` and nothing is applied or pruned

#### Scenario: Platform-not-ready is distinct from render failure

- **WHEN** rendering is blocked because no platform module is recorded
- **THEN** the reason is `PlatformNotReady`, not `RenderFailed`, `ResolutionFailed` or `SkewRefused`

### Requirement: Re-enqueue ModuleInstances when the platform becomes ready

The ModuleInstance reconciler SHALL watch the `Platform`. It SHALL re-enqueue ModuleInstances on a Platform update only when a field that a ModuleInstance render consumes differs between the old and the new object. Those fields are:

- the `Ready` condition's status
- the pin set: `status.packageIdentity` or `status.registry`
- `spec.skewPolicy`
- `status.observedGeneration`

The trigger includes the Platform reconciler's own status update, which does not bump the Platform's generation. A change to `status.operatorVersion` SHALL also re-enqueue: after an operator upgrade it is the only status change the regenerated Platform writes, and it is what recovers instances that rendered into `PlatformNotReady` while the new process's platform store was empty. A Platform update that changes none of these fields SHALL NOT enqueue any ModuleInstance. Examples of such updates are a change to the `Ready` message or to its reason while it stays `False`, a `ContractsFulfilled` update, or a bump of `metadata.generation` alone: the platform has not been regenerated yet, so a render on that edge would run against the previous package, and the `status.observedGeneration` write that follows carries the new one. Platform create and delete events SHALL re-enqueue.

The reconciler SHALL enqueue only the ModuleInstances that render against the changed Platform: those that are operator-managed (`spec.owner` absent, empty or `operator`) and not suspended. A ModuleInstance with `spec.owner: cli` or `spec.suspend: true` SHALL NOT be enqueued by a Platform event; a change to either field is a spec change and reconciles the instance through its own watch.

`status.packageIdentity` is the field that identifies the pin set an instance renders against.

#### Scenario: Blocked instances retry when the platform is generated

- **WHEN** a `ModuleInstance` is blocked with `PlatformNotReady` and a Platform is then applied and reaches `Generated`
- **THEN** the reconciler re-enqueues the instance and renders it on the next reconcile

#### Scenario: Instances re-render under a new pin set

- **WHEN** the Platform's `status.packageIdentity` changes because a claim became active, and the Platform's generation is unchanged
- **THEN** the reconciler re-enqueues every operator-managed, unsuspended `ModuleInstance`

#### Scenario: A message-only status write enqueues nothing

- **WHEN** the Platform reconciler rewrites the `Ready` condition's message, and its status, `packageIdentity`, `registry`, `operatorVersion` and `observedGeneration` are unchanged
- **THEN** no `ModuleInstance` is enqueued and no render runs

#### Scenario: A spec edit renders once the regenerated package lands

- **WHEN** a Platform spec edit bumps `metadata.generation`, and the Platform reconciler later writes the regenerated package's `status.observedGeneration` and `status.packageIdentity`
- **THEN** the generation bump alone enqueues no `ModuleInstance`, and the status write re-enqueues every operator-managed, unsuspended `ModuleInstance`

#### Scenario: CLI-owned and suspended instances are not enqueued

- **GIVEN** one `ModuleInstance` with `spec.owner: cli`, one with `spec.suspend: true` and one operator-managed instance
- **WHEN** the Platform's `Ready` condition moves to `True` with reason `Generated`
- **THEN** only the operator-managed instance is enqueued

#### Scenario: An operator upgrade into an already-Ready Platform recovers blocked instances

- **GIVEN** a Platform that is already `Ready=True` with reason `Generated`, and an operator that restarts under a new version with an empty platform store
- **WHEN** the operator-managed instances render before the platform is regenerated and report `PlatformNotReady`, and the Platform reconciler then regenerates the platform and writes a status whose only change is `status.operatorVersion`
- **THEN** the reconciler re-enqueues every operator-managed, unsuspended `ModuleInstance` and renders it on the next reconcile, without waiting for the transient backoff
