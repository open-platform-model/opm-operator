# modulepackage-kernel-rendering

## Purpose

The `ModulePackage` reconciler renders its Flux-fetched package through the kernel-backed `KernelPackageRenderer` against the generated platform: a `#ModuleInstance` package that carries its own values is acquired through the kernel's on-disk acquisition and rendered through the single-build render with the leased platform record and its skew policy; rendering blocks inertly when no platform is generated, retries promptly when the platform changes, and packages of any other kind are rejected.

## Requirements

### Requirement: ModulePackage renders through the kernel against the generated platform

`KernelPackageRenderer` SHALL acquire the extracted package as a source-carrying instance through the kernel's on-disk acquisition and render it through the single-build render with the leased platform record and its skew policy.

#### Scenario: ModuleInstance package renders and applies

- **WHEN** a `ModulePackage` artifact holding a `#ModuleInstance` package is rendered while a generated platform is recorded
- **THEN** the rendered resources are applied and recorded as before

### Requirement: Block ModulePackage when no platform is generated

When the store holds no generated-module record, the package renderer SHALL return `ErrPlatformNotReady` after kind detection and before any build, and the reconciler SHALL set `Ready=False` reason `PlatformNotReady`.

#### Scenario: No platform present blocks the package inertly

- **WHEN** a `ModulePackage` is reconciled while no platform is recorded
- **THEN** its status carries `PlatformNotReady` and nothing is applied

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

### Requirement: Packages of any other kind are rejected

For a fetched package whose `kind` is anything other than `ModuleInstance`, the renderer SHALL return `ErrUnsupportedKind` and the reconciler SHALL surface `Ready=False` with reason `UnsupportedKind` and `Stalled=True`. The rejection SHALL NOT name speculative kinds: the kernel's `#ModuleInstance` shape gate (`oerrors.ErrWrongKind`, the sentinel the library's `opm/errors` package declares) is the detection mechanism, and the resulting error is generic.

#### Scenario: Wrong-kind package is rejected

- **WHEN** a `ModulePackage` whose fetched package has a `kind` other than `ModuleInstance` is reconciled
- **THEN** rendering returns an unsupported-kind error
- **AND** the status reflects `UnsupportedKind` and nothing is applied

### Requirement: Unresolved platform demands classify as resolution failures

The reconciler SHALL classify a refused render by its typed cause: unresolved platform demands and unmatched components SHALL be `ResolutionFailed`; a catalog-skew refusal SHALL be `SkewRefused` with a message naming the module path, the module's required build and the platform's build; a render whose compiled objects share one apply identity SHALL be `DuplicateIdentities` with the library's message naming each identity and every producing component and transformer (enhancement 0015 D15); a transform failure, an over-subscribed provider contract, or any other refusal SHALL be `RenderFailed`. Every classified refusal SHALL set `Ready=False` and `Stalled=True`, emit a Warning event and requeue on the stalled recheck interval; the ModuleInstance and ModulePackage classifiers SHALL route typed causes through one shared function so they cannot drift. A registry fetch failure (the library's typed `*oerrors.FetchError`) with no typed terminal cause is not a refusal: in any phase of the render it SHALL report `ResolutionFailed` without `Stalled` and retry on the bounded backoff, per the `reconcile-backoff` capability.

#### Scenario: Skew refusal is distinct

- **WHEN** the platform's policy is `Refuse` and the package's module requires a newer catalog build than the platform pins
- **THEN** the package reports `Ready=False` reason `SkewRefused` naming the path and both versions, applies nothing, and requeues on the stalled recheck interval

#### Scenario: Over-subscribed provider contract is a render failure

- **WHEN** the build refuses because two enabled catalogs provide the same provider-fulfilled contract
- **THEN** the package reports `RenderFailed` with the kernel's message naming the contract key and both catalogs

#### Scenario: Package demands contracts the platform does not provide

- **WHEN** a package renders against a platform whose catalogs do not provide contracts the module demands
- **THEN** the ModulePackage reports `Ready=False` with reason `ResolutionFailed` and `Stalled=True`, and a Warning event carries the unresolved-demands message

#### Scenario: Duplicate identities are a refusal of their own

- **WHEN** a render's compiled objects share one apply identity
- **THEN** the object reports `Ready=False` with reason `DuplicateIdentities` and `Stalled=True`, the message names the identity and both producing components, nothing is applied, and the same reason is produced whether the render was a ModuleInstance's or a ModulePackage's

#### Scenario: Ordinary evaluation error keeps RenderFailed

- **WHEN** the render fails for a cause that is neither a resolution-class failure, a registry fetch failure, a skew refusal nor a duplicate identity
- **THEN** the ModulePackage reports `Ready=False` with reason `RenderFailed`

#### Scenario: Registry fetch failure during the render retries

- **WHEN** the render build fails with the library's typed registry fetch failure and no typed terminal cause
- **THEN** the ModulePackage reports `Ready=False` with reason `ResolutionFailed` and no `Stalled` condition, and requeues on the bounded backoff
