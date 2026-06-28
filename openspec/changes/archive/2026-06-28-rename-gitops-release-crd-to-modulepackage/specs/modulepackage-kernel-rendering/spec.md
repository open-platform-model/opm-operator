## ADDED Requirements

<!-- Renamed from `release-kernel-rendering` (0002 D2/D10); the spec dir is `git mv`'d at archive. -->

### Requirement: ModulePackage renders through the kernel against the materialized platform

The `ModulePackage` reconciler SHALL render its Flux-fetched package through the kernel-backed `KernelPackageRenderer`. For a package whose `kind` is `ModuleInstance`, the renderer SHALL load the package in the kernel's context (`Kernel.LoadInstancePackage`), construct the instance (`Kernel.NewInstanceFromValue`), read the materialized platform from the store, and `Kernel.Compile` against it, adapting the compiled output to operator resources and inventory entries. No values SHALL be injected — the authored package carries its own values. Successful rendering SHALL flow through the existing apply/inventory/prune path unchanged.

#### Scenario: ModuleInstance package renders and applies

- **WHEN** a platform is materialized and a `ModulePackage` whose fetched package has `kind: ModuleInstance` is reconciled
- **THEN** the package is rendered through the kernel against the materialized platform
- **AND** the rendered resources are applied and recorded in inventory and status as before

### Requirement: Block ModulePackage when no platform is materialized

When rendering returns `ErrPlatformNotReady`, the `ModulePackage` reconciler SHALL set `Ready=False` with reason `PlatformNotReady`, SHALL apply and prune nothing, SHALL emit a warning event, and SHALL requeue. This is a blocked-on-dependency state, distinct from a render failure or a stall.

#### Scenario: No platform present blocks the package inertly

- **WHEN** a `ModulePackage` is reconciled while no `Platform` is materialized
- **THEN** its status carries `Ready=False` with reason `PlatformNotReady`
- **AND** nothing is applied to or pruned from the cluster

### Requirement: Re-enqueue ModulePackages when the platform becomes ready

The `ModulePackage` reconciler SHALL watch the `Platform` resource and re-enqueue all `ModulePackages` on a Platform change, so packages blocked on `PlatformNotReady` retry promptly rather than only on backoff.

#### Scenario: Blocked package retries when the platform materializes

- **WHEN** a `ModulePackage` is blocked with `PlatformNotReady` and a `Platform` is then applied and materializes
- **THEN** the reconciler re-enqueues the `ModulePackage`
- **AND** on the next reconcile it renders and applies against the materialized platform

### Requirement: Non-ModuleInstance packages are rejected

For a fetched package whose `kind` is anything other than `ModuleInstance`, the renderer SHALL return `ErrUnsupportedKind` and the reconciler SHALL surface `Ready=False` with reason `UnsupportedKind` and `Stalled=True`. The rejection SHALL NOT name speculative kinds: the kernel's `#ModuleInstance` load gate (`loaderfile.ErrWrongKind`) is the detection mechanism, and the resulting error is generic.

#### Scenario: Wrong-kind package is rejected

- **WHEN** a `ModulePackage` whose fetched package has a `kind` other than `ModuleInstance` is reconciled
- **THEN** rendering returns an unsupported-kind error
- **AND** the status reflects `UnsupportedKind` and nothing is applied
