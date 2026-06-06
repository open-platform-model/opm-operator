## ADDED Requirements

### Requirement: Single long-lived library Kernel

The operator SHALL construct exactly one `*kernel.Kernel` from the
`open-platform-model/library` module during manager startup and keep it alive for
the entire process lifetime. The Kernel MUST NOT be reconstructed per reconcile,
per object, or per controller — one Kernel (and therefore one schema `*Cache`) is
shared across all reconcilers.

#### Scenario: Kernel constructed once at startup

- **WHEN** the manager process starts
- **THEN** exactly one `*kernel.Kernel` is constructed before any controller is registered
- **AND** the same Kernel instance is shared by every reconciler that receives it

#### Scenario: Kernel survives across reconciles

- **WHEN** multiple reconcile loops execute over the process lifetime
- **THEN** no reconcile path constructs a new Kernel
- **AND** the core schema is fetched at most once (subsequent access is served from the Kernel's in-process cache)

### Requirement: Kernel configured from existing inputs

The operator SHALL configure the Kernel from inputs the process already accepts:
the registry mapping from the `--registry` flag (falling back to `OPM_REGISTRY`)
via `kernel.WithRegistry`, and a logger bridged to the controller's logging
backend via `kernel.WithLogger`. The Kernel SHALL use the default OCI-backed
schema loader resolving `opmodel.dev/core@v0`. The operator MUST NOT introduce a
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

- **WHEN** the Kernel is constructed against a reachable registry holding `opmodel.dev/core@v0`
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
