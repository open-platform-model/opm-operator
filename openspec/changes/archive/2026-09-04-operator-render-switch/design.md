# Design: operator-render-switch

## Context

See `proposal.md` § Why. Current state on `main` (after PR 118): the Platform reconciler generates and builds the platform module and records `Store.Generated{Generation, Dir, Platform}`; the renderers still call `Store.Get()` (the `*MaterializedPlatform` slot, never written) and `Kernel.Compile`; every kernel use is serialised behind `Store.AcquireKernel`; no controller sets `MaxConcurrentReconciles`; `RenderResult.Warnings` is dropped by both reconcilers.

Upstream contract (`library-render-cutover`, design.md § Public surface changes): `Kernel.Render(RenderInput{Instance, Platform, RuntimeName, Skew})` is the sole render verb; `Instance` and `Platform` must carry `Source`; the build stages the platform from its on-disk root without copying (`renderstage.serveDir`); `RenderError` carries `RenderDiagnostics` and unwraps to `oerrors.{UnresolvedDemandsError, UnmatchedComponentsError, TransformError, OverSubscribedContractError}`; `SkewRefuse` fails before evaluation with `*oerrors.SkewError`; ADR-005 states one Kernel per goroutine for the context-owning methods and shares-nothing for `Render`.

## Goals / Non-Goals

**Goals**

- One render path in the operator, consuming the generated module; e2e green.
- Retire the stopgap: no held materialized platform, no process-wide render serialisation.
- Skew policy, warnings and concurrency each get exactly one surface.

**Non-Goals**

- Changing the generated module's content, path or retention rule beyond honouring leases.
- A status ledger for warnings or resolved versions on the workload CRs.
- Per-tenant or per-instance skew policy (the platform is the admin's object; D7 names the operator surface, and a per-instance override would be a second answer).

## Decisions

### The kernel gate narrows to the context-owning calls; `Render` runs outside it

**Context**: ADR-005 (cutover PR 3) keeps "one Kernel per goroutine" for the methods that evaluate in the Kernel's own `cue.Context` (`AcquireModuleFromRegistry`, `SynthesizeInstance`, `AcquireInstanceFromDir`, `LoadPlatformPackage`) and makes `Render` shares-nothing.
**Options**: (1) keep `AcquireKernel` but hold it only across the context-owning steps of a render, releasing before `Render`; (2) one Kernel per reconcile, no gate, at the cost of a schema-cache fetch per Kernel unless the cache is injected; (3) keep the gate across the whole render as today.
**Decision**: option 1. Acquisition and synthesis stay serial (they are I/O-bound and short), the CUE build overlaps.
**Rationale**: the measured cost is in the build, not the acquisition (0019 experiment 08); option 2 is the cleaner end state but needs a library seam for sharing the schema cache and is a later, smaller step once the gate's remaining scope is visible in profiles.

### Renders lease the generated record; prune skips leased generations

**Context**: `renderstage.serveDir` serves an on-disk platform from its own directory. With concurrent renders, `Layout.Prune` after a new generation could delete `gen-N-1` while a render reads it.
**Options**: copy the module into the render's staging directory (a library change, and a copy per render of a tree the build reads once); never prune while any render runs (a global condition the platform reconciler cannot observe without a counter anyway); a per-generation lease.
**Decision**: `Store.Lease() (Generated, release func(), ok bool)` increments a counter on the record's generation; the reconciler builds its keep set from the current generation plus every leased generation and passes it to `Layout.Prune`. A lease outlives nothing but the render.
**Rationale**: the counter is the minimal observable fact; the retention rule ("current plus what is being read") replaces the "current plus previous" approximation and is exact.

### Skew policy is `Platform.spec.skewPolicy` (owner decision, D-C)

**Context**: D7 leaves the surface to each frontend; D18 fixes the default to warn-and-render.
**Decision**: `SkewPolicy *string` with `+kubebuilder:validation:Enum=Warn;Refuse`, optional, nil meaning `Warn`. The Platform reconciler copies the resolved policy into `Store.Generated.Skew`; both renderers pass it as `RenderInput.Skew`. Changing the field bumps the Platform generation, so it regenerates (byte-identical module, new directory) and re-enqueues the workloads through the existing Platform watch.
**Rationale**: admission-time strictness is the cluster admin's decision and the Platform is their object (D7's multi-tenant argument); the value is visible in `kubectl get platform -o yaml` next to the pins it governs. A manager flag would require a redeploy to change and would be a second place to look.

### Warnings become events on transition; resolved versions are logged

**Context**: `Render` returns human-readable warnings (skew under `Warn`, unhandled optional traits) and `ResolvedVersions` rows; today the operator drops `Warnings`.
**Options**: events only; a bounded `status.warnings` list on both workload CRs; both.
**Decision**: a Warning event (reason `RenderWarning`, action `Render`) per distinct warning message, emitted when the set of warnings for the object changes (compared against the previous reconcile's set kept on the reconciler's in-memory map keyed by object, the same transition pattern the Platform reconciler uses for failures); `ResolvedVersions` at `V(1)` in the reconcile log.
**Rationale**: Principle VII: events are the existing warning channel (`events-emission`) and need no API change; a status ledger is a separate change if operators ask for it.

### Skew refusal is its own reason

**Decision**: `SkewRefused` (Ready=False, stalled recheck interval) with a message naming the path, the module's required version and the platform's version; not `RenderFailed`.
**Rationale**: the fix is a platform pin bump or a module downgrade, both admin actions; a distinct reason makes `kubectl get moduleinstance` show it without reading messages.

### Concurrency knob (owner decision, D-E)

**Decision**: `--max-concurrent-renders` (int, default 1) sets `controller.Options.MaxConcurrentReconciles` on the ModuleInstance and ModulePackage controllers. The Platform controller stays at 1. Help text states the sizing rule from 0019 `06-operational.md` (about 61 MB plus 7.75 MB per component per concurrent render, plus 0.3 GB base) and that the default preserves today's serial behaviour.
**Rationale**: with shares-nothing renders the only shared state is the narrowed gate, so the flag is safe by construction; defaulting to 1 keeps the switch behaviour-neutral for existing deployments.

### Error classification against `RenderError`

**Decision**: `resolution.go` classifies by unwrapping: `UnresolvedDemandsError` or `UnmatchedComponentsError` → `ResolutionFailed` (unchanged semantics, new package for the second type); `SkewError` → `SkewRefused`; `TransformError`, `OverSubscribedContractError` and any other `RenderError` → `RenderFailed`. Pre-evaluation refusals that are the operator's defect (missing `Source`, an uncovered OPM path) are `RenderFailed` with the kernel's message verbatim; they indicate a bug, not a user action.

### Integration specs seed the store from a generated module

**Decision**: the materialize-based integration specs (`kernel_*_renderer`, `example_modules`, `platform_gate`) and `materializeSchemaModule` are deleted; a shared helper generates the platform module with `platformmodule.Generate` + `Closure` into a temp `Layout`, builds it through `AcquirePlatformFromDir`, and records it with `SetGenerated`. The specs then exercise the same path the reconciler does.

## Risks / Trade-offs

- [The cutover release slips] → this change cannot start; nothing here is worth landing against alpha.24.
- [`Render` staging cost per reconcile on large modules] → measured and bounded (0019 experiments 04, 07); the default concurrency of 1 keeps memory at today's envelope; the flag is opt-in.
- [Warning-event dedup map grows] → keyed by namespaced name, entries dropped on object deletion (the finalizer path) and bounded by the number of live objects.
- [Lease leak on a panic mid-render] → `release` is deferred immediately after `Lease`; a leaked lease only delays pruning of one directory until restart, never blocks a render.
- [Enum field on an existing CR] → optional with a default-equivalent nil; stored objects without the field reconcile unchanged; CRD validation ratcheting keeps status patches working (as measured for `version`).

## Migration Plan

Lands on the 0019 release train after `library-render-cutover` PR 2 is released; `go.mod` bump as `fix(deps)` in the same PR as the switch (the operator cannot build against the cutover without it). Rollback pre-release is a revert of the PR, which also reverts the pin. Existing Platform CRs need no edit; existing ModuleInstances re-render on the next reconcile and produce the same objects (parity proven by the library's cutover proof).

## Open Questions

None that change the specs or tasks. Deferrable: whether the resolved-versions rows deserve a status field once operators have seen them in logs.
