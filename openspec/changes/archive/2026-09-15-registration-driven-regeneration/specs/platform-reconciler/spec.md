## MODIFIED Requirements

### Requirement: Materialize the singleton Platform on reconcile

The operator SHALL reconcile the `Platform` named `cluster` by generating its platform CUE module on the operator's own disk, building it through the kernel's shape-gated platform loader, and recording the generated module (its package identity, directory, built platform) together with the resolved skew policy (`spec.skewPolicy`, `Warn` when unset) in the process-local store. The reconciler SHALL reconcile only the object named `cluster`; any other name SHALL be ignored without error. No materialized twin exists.

The package the reconcile generates SHALL be a function of exactly two inputs — the CR's spec and the set of accepted-and-active `TransformerRegistration` claims — and the reconciler SHALL be woken by a change to either (enhancement 0015 D13). The waking event's content SHALL NOT be an input: the reconcile computes from the current state of both, so a stale, duplicated or reordered event yields the package the current state implies. A reconcile whose inputs resolve to the identity the store already holds, for a module directory that still exists, SHALL skip regeneration and still report the outcome on status; this is what bounds a burst of claim activations to one build rather than one per claim.

#### Scenario: Valid platform materializes

- **WHEN** a `Platform` named `cluster` with resolvable pins is applied
- **THEN** the reconciler generates and builds its module
- **AND** the store's record carries the package identity, the module directory and the skew policy

#### Scenario: Non-cluster object ignored

- **WHEN** the reconciler is triggered for an object whose name is not `cluster`
- **THEN** it performs no generation and returns without error

#### Scenario: A claim change wakes the reconciler

- **WHEN** a `TransformerRegistration` becomes active with no edit to the `Platform` CR
- **THEN** the reconciler is woken and records a package built from the new active-claim set

#### Scenario: An unchanged tuple regenerates nothing

- **WHEN** the reconciler is woken repeatedly while the CR spec and the active-claim set are unchanged
- **THEN** the held package is not rewritten and the CR still reports `Ready=True` with reason `Generated`

## REMOVED Requirements

### Requirement: Single-slot generation-keyed store for concurrent read

**Reason**: The CR's `metadata.generation` is no longer sufficient to identify a generated package. Enhancement 0015 D17 makes the package a function of the CR spec AND the active-claim set, so a claim activating produces a different package while leaving the generation untouched. Keyed on the generation, the store would keep serving a platform that does not contain the provider the operator just accepted, and a superseded package would share a directory with the one replacing it. Every clause of this requirement that named a generation is now false.

**Migration**: See "Single-slot identity-keyed store for concurrent read" in this same capability, which restates the slot, the lease and the retention rule in terms of the package identity. Lease semantics are otherwise unchanged: a reader still leases the record for its render and releases it on return, and a superseded package is still reclaimed only once nothing holds it.

## ADDED Requirements

### Requirement: Single-slot identity-keyed store for concurrent read

The store SHALL hold at most one current generated-module record, keyed on the package identity it was built for, safe for a single writer and many readers. Readers SHALL obtain the record through a lease that they release when their render completes; the store SHALL report which identities are leased. Superseded module directories SHALL be removed only when no lease holds their identity.

The identity SHALL be derived from the `Platform` CR's generation and the sorted set of active claims' catalog coordinates, so two packages built from the same inputs carry the same identity and any change to either input yields a different one (enhancement 0015 D13, D17).

#### Scenario: A spec change replaces the slot

- **WHEN** the Platform spec changes (new generation M) and builds successfully
- **THEN** the store returns the record for the identity carrying generation M and the prior record is no longer current

#### Scenario: A claim change replaces the slot without a spec change

- **WHEN** a claim becomes active while the CR's generation is unchanged
- **THEN** the store returns a record under a different identity than the one it held

#### Scenario: A leased package survives the swap

- **WHEN** a render holds a lease on one identity while a package under another identity is recorded
- **THEN** the leased package's directory is not removed until the lease is released, and is removed by a later reconcile once unleased

#### Scenario: Concurrent reads are safe

- **WHEN** multiple goroutines lease and release the record while the reconciler records a new package
- **THEN** reads return a consistent record or absence without data races, and lease counts stay exact
