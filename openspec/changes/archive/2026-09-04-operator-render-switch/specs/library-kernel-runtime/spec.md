## MODIFIED Requirements

### Requirement: Single long-lived library Kernel

The manager SHALL construct one library Kernel for the process lifetime and share it across controllers. Calls that evaluate in the Kernel's own context (module acquisition, instance synthesis, on-disk instance acquisition, the platform build) SHALL be serialised behind the store's kernel gate; the single-build render SHALL run outside the gate, sharing nothing between renders. The gate SHALL never be held across a render.

#### Scenario: Two renders overlap

- **WHEN** two ModuleInstances reconcile concurrently with `--max-concurrent-renders` above 1
- **THEN** their acquisition steps serialise and their builds overlap, and both render correctly

#### Scenario: Kernel constructed once at startup
- **WHEN** the manager process starts
- **THEN** exactly one Kernel is constructed before any controller is registered and shared by every reconciler that receives it

#### Scenario: Kernel survives across reconciles
- **WHEN** multiple reconcile loops execute over the process lifetime
- **THEN** no reconcile path constructs a new Kernel, and the core schema is fetched at most once

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

## ADDED Requirements

### Requirement: Render concurrency is a manager flag bounded by memory

The manager SHALL accept `--max-concurrent-renders` (integer, default 1) and apply it as the maximum concurrent reconciles of the ModuleInstance and ModulePackage controllers. The Platform controller SHALL stay serial. The flag's help SHALL state the memory sizing rule (per-render cost grows with component count) so the value is chosen against the pod's memory limit.

#### Scenario: Default keeps reconciles serial

- **WHEN** the manager starts without the flag
- **THEN** the ModuleInstance and ModulePackage controllers reconcile one object at a time, as before

#### Scenario: Raising the bound allows overlap

- **WHEN** the manager starts with `--max-concurrent-renders=4`
- **THEN** up to four ModuleInstances (and four ModulePackages) reconcile at once
