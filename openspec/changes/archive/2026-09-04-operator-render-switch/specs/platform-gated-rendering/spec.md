## MODIFIED Requirements

### Requirement: ModuleRelease renders through the kernel against the materialized platform

The ModuleInstance reconciler SHALL render via the kernel-backed renderer through the single-build render, against the generated platform module the store records: the renderer takes a lease on the record, renders with the instance's staged source and the platform's on-disk module, and releases the lease when the render returns. Successful rendering SHALL apply the resulting resources through the existing apply/inventory/prune path unchanged.

#### Scenario: ModuleRelease renders and applies when a platform is materialized

- **WHEN** the `cluster` Platform is `Ready` (reason `Generated`) and a `ModuleInstance` referencing a resolvable module is applied
- **THEN** the reconciler renders through the single-build render against the recorded module and applies the rendered resources as before

### Requirement: Block ModuleRelease when no platform is materialized

When the store holds no generated-module record, the reconciler SHALL set the ModuleInstance `Ready=False` with reason `PlatformNotReady`, apply nothing, prune nothing, emit a warning event and requeue.

#### Scenario: No platform present blocks the release inertly

- **WHEN** a `ModuleInstance` is applied while the Platform has not been generated and built
- **THEN** its status carries `Ready=False` with reason `PlatformNotReady` and nothing is applied or pruned

#### Scenario: Platform-not-ready is distinct from render failure
- **WHEN** rendering is blocked because no platform module is recorded
- **THEN** the reason is `PlatformNotReady`, not `RenderFailed`, `ResolutionFailed` or `SkewRefused`

### Requirement: Re-enqueue ModuleReleases when the platform becomes ready

The ModuleInstance reconciler SHALL watch the `Platform` and, on a generation change, re-enqueue all `ModuleInstances` so that instances blocked on `PlatformNotReady`, and instances rendered under a superseded policy or pin set, retry promptly.

#### Scenario: Blocked releases retry when the platform materializes

- **WHEN** a `ModuleInstance` is blocked with `PlatformNotReady` and a Platform is then applied and reaches `Generated`
- **THEN** the reconciler re-enqueues the instance and renders it on the next reconcile

## REMOVED Requirements

### Requirement: Provider input no longer drives ModuleRelease rendering

**Reason**: the startup-loaded provider was removed in an earlier wave; the only transformer source is the platform module's registry, which the single-build render evaluates inside the build.

**Migration**: none.
