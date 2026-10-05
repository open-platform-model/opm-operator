## Purpose

Define how the operator constructs and shares the `open-platform-model/library`
Kernel as a single long-lived, process-wide runtime. The Kernel owns the schema
cache and core-schema resolution, is configured from inputs the process already
accepts, is verified at startup, and is injected into reconcilers as the seam
later enhancement-0001 render-path slices consume — without changing existing
reconcile behavior.

## Requirements

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
- **THEN** no mutex or ordering gate serialises kernel calls, and a render holds only its platform lease and one render slot, the slot taken before the lease and released once the rendered set has been exported for apply

### Requirement: Kernel configured from existing inputs

The operator SHALL configure the Kernel from inputs the process already accepts:
the registry mapping from the `--registry` flag (falling back to `OPM_REGISTRY`)
via `kernel.WithRegistry`, and a logger bridged to the controller's logging
backend via `kernel.WithLogger`. The Kernel SHALL use the default OCI-backed
schema loader resolving `opmodel.dev/core@v2`. The operator MUST NOT introduce a
new flag or environment variable for Kernel configuration in this change.

#### Scenario: Registry sourced from existing flag

- **WHEN** the operator is started with `--registry` set (or `OPM_REGISTRY` in the environment)
- **THEN** that same value configures the Kernel's registry mapping
- **AND** no additional registry flag or env var is introduced

### Requirement: Core-schema resolution verified at startup

The operator SHALL verify that the Kernel can resolve the OPM core schema during
startup, before the manager begins serving. On success it SHALL log the resolved
core schema version. On failure it SHALL fail startup with a clear error rather
than deferring the failure to the first reconcile.

#### Scenario: Schema resolves successfully

- **WHEN** the Kernel is constructed against a reachable registry holding `opmodel.dev/core@v2`
- **THEN** startup completes
- **AND** the operator logs the resolved core schema version

#### Scenario: Schema unreachable fails fast

- **WHEN** the Kernel cannot resolve the core schema at startup (unreachable or misconfigured registry)
- **THEN** the operator fails startup with an error naming the schema-resolution failure
- **AND** the manager does not begin reconciling

### Requirement: Kernel injected as render-path seam

The operator SHALL pass the constructed Kernel into the reconcilers as a struct
field, establishing the injection point that later enhancement-0001 slices
consume. In this change the field is wired but the legacy render path is
unchanged; reconciliation behavior MUST remain identical to before this change.

#### Scenario: Reconcilers receive the Kernel without behavior change

- **WHEN** the reconcilers are registered with the manager
- **THEN** each render-bearing reconciler holds a reference to the shared Kernel
- **AND** existing reconcile behavior (synthesis, match, render, apply, prune, status) is unchanged because no render path reads the Kernel yet

### Requirement: Embedded kernel line and compile semantics

The operator SHALL embed the library release in which the single-build render is the sole render path (the `library-render-cutover` release or later). Rendering semantics (fail-closed on unresolved demands, matching inside the build, the promoted dependency list) are the kernel's; the operator SHALL NOT reimplement matching or dependency resolution.

#### Scenario: No old-path symbol remains

- **WHEN** the operator builds against the embedded library
- **THEN** no code path references materialization, platform synthesis or the two-phase compile

#### Scenario: Unresolved demand stalls the instance

- **WHEN** a rendered module demands a resource contract the platform's catalogs do not provide
- **THEN** the render is refused and the ModuleInstance stalls with reason `ResolutionFailed`, and no partial render is applied

#### Scenario: Identity mismatch at module acquire

- **WHEN** a published module's declared metadata disagrees with the coordinate it was fetched by
- **THEN** the render fails with the typed identity error naming both values

#### Scenario: Optional trait still degrades to a warning

- **WHEN** an unhandled trait's effective `optional` is true
- **THEN** the render succeeds and the result's warnings carry the trait

### Requirement: Render concurrency is a manager flag bounded by memory

The manager SHALL accept `--max-concurrent-renders` (integer, default 1) and SHALL construct from it one pool of render slots shared by the ModuleInstance and ModulePackage reconcilers, so the flag is the maximum number of renders in flight across the whole process, both kinds together. A reconcile SHALL take a slot before it calls its renderer (platform lease, acquisition, synthesis and render), SHALL hold it while the result is exported for apply (render digest and conversion to unstructured objects), and SHALL release it only after the rendered CUE values are dropped, on success, on error and on a panic that the controller runtime recovers. So no reconcile holds a rendered CUE value without a slot, and the flag bounds the builds resident in the process. Each of the two controllers SHALL also use the flag as its maximum concurrent reconciles, so phases outside the render are not serialised behind the other kind's renders. A reconcile whose wait for a slot ends because its context is cancelled SHALL return the context error without calling the renderer and without patching the object's status, emitting an event or recording reconcile metrics. The Platform controller SHALL stay serial and SHALL take no slot. The flag's help SHALL state the memory sizing rule (per-render cost grows with component count) and that the bound is shared by both kinds, so the value is chosen against the pod's memory limit.

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

#### Scenario: Conversion runs while the slot is held

- **WHEN** a ModuleInstance or ModulePackage reconcile exports its render result for apply, on a pool of one slot
- **THEN** that slot is taken for the whole export, and it is free again once the reconcile has dropped the rendered resources

### Requirement: Rendered values are dropped after conversion

A rendered resource carries the CUE value it was read from, and a held value keeps the whole build reachable. The ModuleInstance and ModulePackage reconcilers SHALL therefore drop the render result's CUE-backed resources as soon as the one export of them returns, before the render digest, the inventory entries and apply are computed from that export, so a reconcile holds a build only from the render to that conversion, and does both while it holds its render slot. Every later phase (shrink judgment, apply, prune, inventory and status) SHALL read the exported data (the unstructured objects and the inventory entries built from them) or the render result's plain data (warnings, required contracts, platform identity), never the dropped resources.

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

### Requirement: A render is bounded by a manager timeout that keeps its slot

The manager SHALL accept `--render-timeout` (a duration, default `10m`) and SHALL refuse a negative value at startup. While a ModuleInstance or ModulePackage reconcile holds its render slot, the renderer call (platform lease, acquisition, synthesis, the render build) and the export of the result for apply SHALL run under a context whose deadline is that timeout. The wait for a slot SHALL NOT count against it, and the reconcile's own context, which the status patch, events, apply and prune use, SHALL carry no such deadline. A value of `0` SHALL disable the deadline, and the render then runs on the reconcile's context as before.

When the deadline passes before the render returns, the reconcile SHALL stop waiting for it and record the attempt (see reconcile-backoff, "A render timeout retries on the backoff"). The render SHALL keep its slot until it really returns (at the next stage boundary when its current I/O honours cancellation, otherwise when that I/O returns), so `--max-concurrent-renders` still bounds the builds held in memory. A render that returns after its deadline SHALL be treated as timed out, and its result SHALL be discarded. A reconcile SHALL NOT read anything the abandoned render writes, and nothing the abandoned render reads SHALL be changed or removed under it: a ModuleInstance render reads a copy of the spec inputs, and a ModulePackage's extracted artifact directory is removed by the render once it returns, or by the reconcile when no render was started.

While a timed-out render of an object is still running, a reconcile of that object SHALL NOT start another render and SHALL NOT take a slot: it SHALL record the attempt as a render timeout whose message says the previous render is still running. One object whose render hangs therefore holds at most one slot, whatever `--max-concurrent-renders` is.

A panic in the render before its deadline SHALL be logged at error level with its value and the stack of the panicking frame, and SHALL then reach the reconcile with its original value, after the slot is free, so it is recorded as `ReconcilePanic`. A panic after the reconcile stopped waiting SHALL be logged at error level with its value and stack, SHALL free the slot, and SHALL NOT stop the process. The operator SHALL log a timed-out render with its timeout, and SHALL log when an abandoned render returns, with how long it ran. The flag's help SHALL state that the slot stays held until the render returns and that `0` disables the deadline.

#### Scenario: Default timeout

- **WHEN** the manager starts without `--render-timeout`
- **THEN** every render runs under a 10-minute deadline counted from when it holds its slot

#### Scenario: Negative timeout is refused

- **WHEN** the manager starts with `--render-timeout=-1s`
- **THEN** it logs the invalid value and exits before starting any controller

#### Scenario: Zero disables the deadline

- **WHEN** the manager starts with `--render-timeout=0`
- **THEN** a render runs on the reconcile's context with no deadline, as it did before the flag existed

#### Scenario: A timed-out render keeps its slot until it returns

- **GIVEN** a pool of one slot and a ModuleInstance whose render blocks past its deadline and keeps running after it
- **WHEN** the deadline passes
- **THEN** the ModuleInstance reports `RenderTimedOut` while the slot is still taken, and a ModulePackage render enqueued next starts only after the blocked render returns

#### Scenario: Queued renders are not timed out

- **GIVEN** a pool of one slot held by a long render
- **WHEN** another reconcile waits for the slot longer than `--render-timeout`
- **THEN** that reconcile is not reported as timed out while it waits, and its own deadline starts when it takes the slot

#### Scenario: A render that finishes in time is unaffected

- **WHEN** a render returns before its deadline
- **THEN** the reconcile reads its result and continues to apply as before

#### Scenario: The status patch outlives the render deadline

- **WHEN** a render times out
- **THEN** the object's status is patched with the failure, because the patch uses the reconcile's context and not the expired render context

#### Scenario: A ModulePackage render past its deadline keeps its files

- **GIVEN** a ModulePackage whose render is still running after its deadline
- **WHEN** the reconcile has returned
- **THEN** the extracted artifact directory still exists, and it is removed once the render returns

#### Scenario: A hung object holds at most one slot

- **GIVEN** a pool of two slots and a ModuleInstance whose render blocks past its deadline and keeps running
- **WHEN** the instance is reconciled again while that render still runs
- **THEN** the reconcile reports `RenderTimedOut` saying the previous render is still running, does not call the renderer, and only one slot is held

#### Scenario: A panic before the deadline keeps its stack

- **GIVEN** a render timeout above zero
- **WHEN** the render panics before its deadline
- **THEN** the operator logs `Render panicked` with the stack of the panicking frame, the slot is free, and the object records `ReconcilePanic`

#### Scenario: A panic after the deadline does not stop the operator

- **WHEN** an abandoned render panics after its reconcile has returned
- **THEN** the operator logs the panic with its stack, the slot is free again, and the process keeps running
