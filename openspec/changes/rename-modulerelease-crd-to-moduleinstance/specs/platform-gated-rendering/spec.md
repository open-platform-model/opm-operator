## ADDED Requirements

### Requirement: ModuleInstance renders through the kernel against the materialized platform

The ModuleInstance reconciler SHALL render via the kernel-backed renderer using the materialized platform held in the platform store. The production manager SHALL wire `KernelModuleRenderer` (carrying the shared Kernel, the platform store, the registry mapping, and a runtime identity) as the ModuleInstance reconciler's renderer. Successful rendering SHALL apply the resulting resources through the existing apply/inventory/prune path unchanged.

#### Scenario: ModuleInstance renders and applies when a platform is materialized

- **WHEN** a `Platform` named `cluster` is materialized (held in the store) and a `ModuleInstance` referencing a resolvable module is applied
- **THEN** the reconciler renders the module through the kernel against the materialized platform
- **AND** the rendered resources are applied and recorded in the ModuleInstance inventory and status as before

### Requirement: Block ModuleInstance when no platform is materialized

When rendering returns `ErrPlatformNotReady` (the store holds no materialized platform), the reconciler SHALL set the ModuleInstance `Ready=False` with reason `PlatformNotReady`, SHALL apply nothing and prune nothing, SHALL emit a warning event, and SHALL requeue. This is a blocked-on-dependency state, not a terminal stall.

#### Scenario: No platform present blocks the instance inertly

- **WHEN** a `ModuleInstance` is applied while no `Platform` is materialized
- **THEN** its status carries `Ready=False` with reason `PlatformNotReady`
- **AND** no resources are applied to the cluster and no existing resources are pruned

#### Scenario: Platform-not-ready is distinct from render failure

- **WHEN** rendering fails because no platform is materialized
- **THEN** the reason is `PlatformNotReady`, not `RenderFailed` or `ResolutionFailed`

### Requirement: Re-enqueue ModuleInstances when the platform becomes ready

The ModuleInstance reconciler SHALL watch the `Platform` resource and, on a Platform change (generation change / materialization), SHALL re-enqueue all `ModuleInstances` so that instances blocked on `PlatformNotReady` retry promptly rather than only on the stalled-recheck backoff.

#### Scenario: Blocked instances retry when the platform materializes

- **WHEN** a `ModuleInstance` is blocked with `PlatformNotReady` and a `Platform` is then applied and materializes
- **THEN** the reconciler re-enqueues the `ModuleInstance`
- **AND** on the next reconcile it renders and applies against the now-materialized platform

### Requirement: Provider input no longer drives ModuleInstance rendering

After the cut-over, the startup-loaded `*provider.Provider` SHALL NOT determine ModuleInstance rendering output; the materialized platform from the store SHALL be the sole transformer source. The provider parameter MAY remain on the interface and be passed by the reconciler, but the kernel renderer SHALL ignore it.

#### Scenario: Rendering is driven by the platform, not the startup provider

- **WHEN** a `ModuleInstance` renders after the cut-over
- **THEN** the transformers applied come from the materialized platform's registry subscriptions
- **AND** the startup provider does not affect the rendered output

## REMOVED Requirements

### Requirement: ModuleRelease renders through the kernel against the materialized platform

**Reason**: Renamed for Release→Instance vocabulary (enhancement 0002 D11/D12).
**Migration**: See the ADDED requirement "ModuleInstance renders through the kernel against the materialized platform".

### Requirement: Block ModuleRelease when no platform is materialized

**Reason**: Renamed for Release→Instance vocabulary (enhancement 0002 D11/D12).
**Migration**: See the ADDED requirement "Block ModuleInstance when no platform is materialized".

### Requirement: Re-enqueue ModuleReleases when the platform becomes ready

**Reason**: Renamed for Release→Instance vocabulary (enhancement 0002 D11/D12).
**Migration**: See the ADDED requirement "Re-enqueue ModuleInstances when the platform becomes ready".

### Requirement: Provider input no longer drives ModuleRelease rendering

**Reason**: Renamed for Release→Instance vocabulary (enhancement 0002 D11/D12).
**Migration**: See the ADDED requirement "Provider input no longer drives ModuleInstance rendering".
