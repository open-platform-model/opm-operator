# release-kernel-rendering

## Purpose

The `Release` reconciler renders its Flux-fetched release package through the kernel-backed `KernelReleaseRenderer` against the materialized platform: `ModuleRelease` packages are loaded, constructed, and compiled in the kernel's context with no injected values; rendering blocks inertly when no platform is materialized, retries promptly when the platform becomes ready, and non-`ModuleRelease` packages are rejected.

## Requirements

### Requirement: Release renders through the kernel against the materialized platform

`KernelPackageRenderer` SHALL acquire the extracted package as a source-carrying instance through the kernel's on-disk acquisition and render it through the single-build render with the leased platform record and its skew policy.

#### Scenario: ModuleRelease package renders and applies

- **WHEN** a `ModulePackage` artifact holding a `#ModuleInstance` package is rendered while a generated platform is recorded
- **THEN** the rendered resources are applied and recorded as before

### Requirement: Block Release when no platform is materialized

When the store holds no generated-module record, the package renderer SHALL return `ErrPlatformNotReady` after kind detection and before any build, and the reconciler SHALL set `Ready=False` reason `PlatformNotReady`.

#### Scenario: No platform present blocks the release inertly

- **WHEN** a `ModulePackage` is reconciled while no platform is recorded
- **THEN** its status carries `PlatformNotReady` and nothing is applied

### Requirement: Re-enqueue Releases when the platform becomes ready

The `Release` reconciler SHALL watch the `Platform` resource and re-enqueue all `Releases` on a Platform change, so releases blocked on `PlatformNotReady` retry promptly rather than only on backoff.

#### Scenario: Blocked release retries when the platform materializes

- **WHEN** a `Release` is blocked with `PlatformNotReady` and a `Platform` is then applied and materializes
- **THEN** the reconciler re-enqueues the `Release`
- **AND** on the next reconcile it renders and applies against the materialized platform

### Requirement: Non-ModuleRelease packages are rejected

For a fetched package whose `kind` is anything other than `ModuleRelease`, the renderer SHALL return `ErrUnsupportedKind` and the reconciler SHALL surface `Ready=False` with reason `UnsupportedKind` and `Stalled=True`. The rejection SHALL NOT name speculative kinds: the kernel's `#ModuleRelease` load gate (`loaderfile.ErrWrongKind`) is the detection mechanism, and the resulting error is generic.

#### Scenario: Wrong-kind package is rejected

- **WHEN** a `Release` whose fetched package has a `kind` other than `ModuleRelease` is reconciled
- **THEN** rendering returns an unsupported-kind error
- **AND** the status reflects `UnsupportedKind` and nothing is applied

### Requirement: Unresolved platform demands classify as resolution failures

The reconciler SHALL classify a refused render by its typed cause: unresolved platform demands and unmatched components SHALL be `ResolutionFailed`; a catalog-skew refusal SHALL be `SkewRefused` with a message naming the module path, the module's required build and the platform's build; a transform failure, an over-subscribed provider contract, or any other refusal SHALL be `RenderFailed`.

#### Scenario: Skew refusal is distinct

- **WHEN** the platform's policy is `Refuse` and the package's module requires a newer catalog build than the platform pins
- **THEN** the package reports `Ready=False` reason `SkewRefused` naming the path and both versions, applies nothing, and requeues on the stalled recheck interval

#### Scenario: Over-subscribed provider contract is a render failure

- **WHEN** the build refuses because two enabled catalogs provide the same provider-fulfilled contract
- **THEN** the package reports `RenderFailed` with the kernel's message naming the contract key and both catalogs

#### Scenario: Package demands contracts the platform does not provide

- **WHEN** a package renders against a platform whose catalogs do not provide contracts the module demands
- **THEN** the ModulePackage reports `Ready=False` with reason `ResolutionFailed` and `Stalled=True`, and a Warning event carries the unresolved-demands message

#### Scenario: Ordinary evaluation error keeps RenderFailed

- **WHEN** the render fails for a cause that is neither a resolution-class failure nor a skew refusal
- **THEN** the ModulePackage reports `Ready=False` with reason `RenderFailed`
