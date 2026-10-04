## MODIFIED Requirements

### Requirement: Re-enqueue ModulePackages when the platform becomes ready

The `ModulePackage` reconciler SHALL watch the `Platform` resource. On a Platform update, it SHALL re-enqueue all `ModulePackages` only when a field that a package render consumes differs between the old and the new object. A package render reads the same generated platform record as a ModuleInstance render, so the fields are the same ones the ModuleInstance Platform watch compares:

- the `Ready` condition's status
- the pin set: `status.packageIdentity` or `status.registry`
- `spec.skewPolicy`
- `status.observedGeneration`
- `status.operatorVersion`

The trigger includes the Platform reconciler's own status update, which does not bump the Platform's generation. This lets packages blocked on `PlatformNotReady` retry promptly, not only on their interval requeue. A `status.operatorVersion` change re-enqueues because, after an operator upgrade, it is the only status change the regenerated Platform writes. A Platform update that changes none of these fields SHALL NOT enqueue any `ModulePackage`. Examples are a change to the `Ready` message, a change to its reason while it stays `False`, a `ContractsFulfilled` update, or a bump of `metadata.generation` alone. Platform create and delete events SHALL re-enqueue. A `spec.skewPolicy` edit can render each package once under the previous policy before the `observedGeneration` write renders it under the new one.

#### Scenario: Blocked package retries when the platform is generated

- **WHEN** a `ModulePackage` is blocked with `PlatformNotReady` and a `Platform` is then applied and generated
- **THEN** the reconciler re-enqueues the `ModulePackage`
- **AND** on the next reconcile it renders and applies against the generated platform

#### Scenario: A message-only status write enqueues no package

- **WHEN** the Platform reconciler rewrites the `Ready` condition's message or updates `ContractsFulfilled`, and the `Ready` status, `packageIdentity`, `registry`, `skewPolicy`, `observedGeneration` and `operatorVersion` are unchanged
- **THEN** no `ModulePackage` is enqueued and no package renders

#### Scenario: Packages re-render under a new pin set

- **WHEN** the Platform's `status.packageIdentity` changes because a claim became active, and the Platform's generation is unchanged
- **THEN** the reconciler re-enqueues every `ModulePackage`

#### Scenario: An operator upgrade into an already-Ready Platform recovers blocked packages

- **GIVEN** a Platform that is already `Ready=True` with reason `Generated`, and an operator that restarts under a new version with an empty platform store
- **WHEN** the packages render before the platform is regenerated and report `PlatformNotReady`, and the Platform reconciler then regenerates the platform and writes a status whose only change is `status.operatorVersion`
- **THEN** the reconciler re-enqueues every `ModulePackage` and renders it on the next reconcile, without waiting for the interval requeue
