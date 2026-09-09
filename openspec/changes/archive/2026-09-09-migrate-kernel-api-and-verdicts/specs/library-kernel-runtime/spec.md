## MODIFIED Requirements

### Requirement: Single long-lived library Kernel

The manager SHALL construct one library Kernel for the process lifetime and share it across controllers. The Kernel is safe for concurrent use across its methods: every kernel call (module acquisition, instance synthesis, on-disk instance acquisition, the platform build and the single-build render) evaluates in a context the library creates for that call and releases with it, so the operator SHALL NOT serialise any kernel call behind a gate of its own and SHALL hold no lock while calling the Kernel. Concurrency is bounded by the controllers' own limits: `--max-concurrent-renders` on the render paths, and one platform generation at a time by the Platform reconciler's construction.

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

- **WHEN** a developer inspects the platform store and the reconcilers
- **THEN** no mutex or gate serialises kernel calls, and no reconcile holds a lock while calling the Kernel
