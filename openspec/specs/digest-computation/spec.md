## Purpose

Defines the deterministic SHA-256 digests the reconciler records in status to detect a no-op reconcile: the source and config digests and `IsNoOp`, which the `internal/status` package computes, and the render digest, which is the library's (`opm/k8s/inventory`).

## Requirements

### Requirement: Config digest computation
The `internal/status` package MUST provide a function that computes a deterministic SHA-256 digest from `v1alpha1.RawValues`.

#### Scenario: Deterministic output
- **WHEN** the same values are provided in different `RawValues` instances
- **THEN** the computed digest is identical

#### Scenario: Nil values
- **WHEN** values are nil
- **THEN** the function returns an empty string

#### Scenario: Content sensitivity
- **WHEN** a value field changes
- **THEN** the computed digest differs

### Requirement: DigestSet type
The `internal/status` package MUST define a `DigestSet` struct with fields `Source`, `Config`, `Render`, and `Inventory`.

#### Scenario: All fields populated
- **WHEN** a reconcile attempt progresses through rendering
- **THEN** all four digest fields in the `DigestSet` are populated

### Requirement: No-op detection
The `internal/status` package MUST provide an `IsNoOp` function that compares two `DigestSet` values and returns true only when all four digests match.

#### Scenario: All digests match
- **WHEN** the current digest set matches the last applied digest set
- **THEN** `IsNoOp` returns true

#### Scenario: One digest differs
- **WHEN** any one of the four digests differs
- **THEN** `IsNoOp` returns false

#### Scenario: Empty last applied
- **WHEN** the last applied digest set has empty strings (first reconcile)
- **THEN** `IsNoOp` returns false

### Requirement: Source digest formula is a frozen cross-repo contract

`ModuleSourceDigest` SHALL remain exactly `sha256(modulePath + "@" + moduleVersion)` rendered as `sha256:%x` over the two `spec.module` strings, byte-identical to the CLI's `sourceDigest` — the two actors' no-op detection depends on the equality and neither side may change the formula unilaterally. The formula SHALL be pinned by a golden test naming the peer implementation.

#### Scenario: Golden pin

- **WHEN** the digest test runs
- **THEN** `ModuleSourceDigest` over a fixed coordinate equals the recorded literal digest
- **AND** the test's comment names the CLI peer that must move in lockstep

### Requirement: The render digest is the library's
The render digest the reconcilers record (`lastAttemptedRenderDigest`, `lastAppliedRenderDigest` and history entries) MUST be `RenderDigest` from the library's `opm/k8s/inventory` over the render's one `object.Export`. It MUST be computed from the exported JSON after the rendered CUE values are dropped. The `internal/status` package MUST NOT declare a render digest of its own. A failure to compute it MUST be reported as a render failure (`RenderFailed`) with the step "computing render digest". Source: 0012:D6:R2/R3.

#### Scenario: Order-independent
- **WHEN** the same resources are rendered in a different order
- **THEN** the recorded render digest is identical

#### Scenario: Content sensitivity
- **WHEN** a resource's name, spec or any label other than the managed-by label changes
- **THEN** the recorded render digest differs

#### Scenario: The runtime name does not count
- **WHEN** two renders differ only in the value of the `app.kubernetes.io/managed-by` label (`opm-controller` and `opm-cli`)
- **THEN** their render digests are equal

#### Scenario: The stored render digest changes once on upgrade
- **WHEN** an instance whose `lastAppliedRenderDigest` was written by an earlier operator renders the same objects under this operator
- **THEN** the new render digest differs from the stored value, and the reconcile is not a no-op
