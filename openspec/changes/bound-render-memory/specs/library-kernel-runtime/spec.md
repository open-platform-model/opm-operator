## MODIFIED Requirements

### Requirement: Single long-lived library Kernel

The manager SHALL construct one library Kernel for the process lifetime and share it across controllers. The Kernel is safe for concurrent use across its methods: every kernel call (module acquisition, instance synthesis, on-disk instance acquisition, the platform build and the single-build render) evaluates in a context the library creates for that call and releases with it, so the operator SHALL NOT serialise any kernel call behind a mutex or other correctness gate of its own. No lock or ordering gate SHALL be held across a kernel call: a render holds its platform lease and one render slot from the process-wide pool sized by `--max-concurrent-renders`, and neither orders or excludes particular calls; the slot bounds memory only. Concurrency is bounded by that pool on the render paths, and to one platform generation at a time by the Platform reconciler's construction.

#### Scenario: Two renders overlap

- **WHEN** two ModuleInstances reconcile concurrently with `--max-concurrent-renders` above 1
- **THEN** their acquisition, synthesis and builds all overlap, and both render correctly

#### Scenario: Kernel constructed once at startup

- **WHEN** the manager process starts
- **THEN** exactly one Kernel is constructed before any controller is registered and shared by every reconciler that receives it

#### Scenario: Kernel survives across reconciles

- **WHEN** multiple reconcile loops execute over the process lifetime
- **THEN** no reconcile path constructs a new Kernel, and the core schema is fetched at most once

#### Scenario: No kernel gate

- **WHEN** a developer inspects the platform store, the render slots and the reconcilers
- **THEN** no mutex or ordering gate serialises kernel calls, and a render holds only its platform lease and one render slot, the slot taken before the lease and released when the renderer returns

### Requirement: Render concurrency is a manager flag bounded by memory

The manager SHALL accept `--max-concurrent-renders` (integer, default 1) and SHALL construct from it one pool of render slots shared by the ModuleInstance and ModulePackage reconcilers, so the flag is the maximum number of renders in flight across the whole process, both kinds together. A reconcile SHALL take a slot before it calls its renderer (platform lease, acquisition, synthesis and render) and SHALL release it when the renderer returns, on success, on error and on a panic that the controller runtime recovers. Each of the two controllers SHALL also use the flag as its maximum concurrent reconciles, so phases outside the render are not serialised behind the other kind's renders. A reconcile whose wait for a slot ends because its context is cancelled SHALL return the context error without calling the renderer and without patching the object's status, emitting an event or recording reconcile metrics. The Platform controller SHALL stay serial and SHALL take no slot. The flag's help SHALL state the memory sizing rule (per-render cost grows with component count) and that the bound is shared by both kinds, so the value is chosen against the pod's memory limit.

#### Scenario: Default keeps reconciles serial

- **WHEN** the manager starts without the flag
- **THEN** the ModuleInstance and ModulePackage controllers each reconcile one object at a time, and at most one render is in flight across both

#### Scenario: Raising the bound allows overlap

- **WHEN** the manager starts with `--max-concurrent-renders=4`
- **THEN** up to four ModuleInstances (and four ModulePackages) reconcile at once, and at most four renders are in flight across both kinds together

#### Scenario: Both kinds share the slots

- **WHEN** the manager runs with `--max-concurrent-renders=1` and a ModuleInstance and a ModulePackage are enqueued at the same time
- **THEN** one renders while the other waits for the slot, and both reach Ready

#### Scenario: Shutdown while waiting for a slot

- **WHEN** the manager is stopping and a reconcile is still waiting for a render slot
- **THEN** the reconcile returns the context error without calling the renderer, and the object's status is not patched

#### Scenario: A panicking render frees its slot

- **WHEN** a renderer panics while a reconcile holds a render slot and the controller runtime recovers the panic
- **THEN** the slot is free again, and the next render of either kind takes it without waiting

## ADDED Requirements

### Requirement: Rendered values are dropped after conversion

A rendered resource carries the CUE value it was read from, and a held value keeps the whole build reachable. The ModuleInstance and ModulePackage reconcilers SHALL therefore drop the render result's CUE-backed resources as soon as they have converted them to unstructured objects, before apply, so a reconcile holds a build only from the render to that conversion. Every later phase (shrink judgment, apply, prune, inventory and status) SHALL read the unstructured objects or the render result's plain data (inventory entries, warnings, required contracts, platform identity), never the dropped resources.

#### Scenario: A ModuleInstance reconcile drops the rendered resources

- **WHEN** a ModuleInstance renders and its resources are converted to unstructured objects
- **THEN** the render result's resources are nil from that point, and the instance still applies, records its inventory and reaches Ready

#### Scenario: A ModulePackage reconcile drops the rendered resources

- **WHEN** a ModulePackage renders and is not a no-op
- **THEN** the render result's resources are nil once converted for apply, and the package still applies, prunes and records its inventory

### Requirement: The shipped manager sets a Go soft memory limit

The shipped manager Deployment (`config/manager/manager.yaml`, and so `dist/install.yaml`) SHALL set `GOMEMLIMIT` on the manager container to about 80% of the container's memory limit, so the Go runtime collects harder as the heap approaches the limit instead of the container being killed at it. Because the downward API cannot scale a resource value, the value SHALL be a literal, and the manifest SHALL say beside it that it moves with `limits.memory`.

#### Scenario: The installer carries the soft limit

- **WHEN** `dist/install.yaml` is rendered from `config/default`
- **THEN** the manager container has `limits.memory: 4Gi` and the environment variable `GOMEMLIMIT=3276MiB`
