## ADDED Requirements

### Requirement: Release renders through the kernel against the materialized platform

The `Release` reconciler SHALL render its Flux-fetched release package through the kernel-backed `KernelReleaseRenderer`. For a package whose `kind` is `ModuleRelease`, the renderer SHALL load the package in the kernel's context (`Kernel.LoadReleasePackage`), construct the release (`Kernel.NewReleaseFromValue`), read the materialized platform from the store, and `Kernel.Compile` against it, adapting the compiled output to operator resources and inventory entries. No values SHALL be injected — the authored release package carries its own values. Successful rendering SHALL flow through the existing apply/inventory/prune path unchanged.

#### Scenario: ModuleRelease package renders and applies

- **WHEN** a platform is materialized and a `Release` whose fetched package has `kind: ModuleRelease` is reconciled
- **THEN** the package is rendered through the kernel against the materialized platform
- **AND** the rendered resources are applied and recorded in inventory and status as before

### Requirement: Block Release when no platform is materialized

When rendering returns `ErrPlatformNotReady`, the `Release` reconciler SHALL set `Ready=False` with reason `PlatformNotReady`, SHALL apply and prune nothing, SHALL emit a warning event, and SHALL requeue. This is a blocked-on-dependency state, distinct from a render failure or a stall.

#### Scenario: No platform present blocks the release inertly

- **WHEN** a `Release` is reconciled while no `Platform` is materialized
- **THEN** its status carries `Ready=False` with reason `PlatformNotReady`
- **AND** nothing is applied to or pruned from the cluster

### Requirement: Re-enqueue Releases when the platform becomes ready

The `Release` reconciler SHALL watch the `Platform` resource and re-enqueue all `Releases` on a Platform change, so releases blocked on `PlatformNotReady` retry promptly rather than only on backoff.

#### Scenario: Blocked release retries when the platform materializes

- **WHEN** a `Release` is blocked with `PlatformNotReady` and a `Platform` is then applied and materializes
- **THEN** the reconciler re-enqueues the `Release`
- **AND** on the next reconcile it renders and applies against the materialized platform

### Requirement: BundleRelease remains unsupported

For a fetched package whose `kind` is `BundleRelease`, the renderer SHALL return `ErrUnsupportedKind` and the reconciler SHALL surface `UnsupportedKind`, unchanged from current behavior. BundleRelease rendering is not implemented in this slice.

#### Scenario: BundleRelease package is rejected

- **WHEN** a `Release` whose fetched package has `kind: BundleRelease` is reconciled
- **THEN** rendering returns an unsupported-kind error
- **AND** the status reflects `UnsupportedKind` and nothing is applied
