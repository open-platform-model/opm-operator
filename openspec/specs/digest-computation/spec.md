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

### Requirement: Render digest computation
The `internal/status` package MUST provide a function that computes a deterministic SHA-256 digest of the rendered resource set from the library's single export of it (`object.Export` in `opm/k8s/object`), so the digest and the objects applied come from one CUE export per resource. It MUST sort the exported objects by group, kind, namespace and name and hash each object's exported JSON in that order. Its bytes MUST equal those of the digest the operator computed before it read the library's export, pinned by a golden test, so an upgrade alone never makes an applied instance look changed.

#### Scenario: Order-independent
- **WHEN** the same resources are provided in different order
- **THEN** the computed digest is identical (resources are sorted before hashing)

#### Scenario: Content sensitivity
- **WHEN** a resource's name or spec changes
- **THEN** the computed digest differs

#### Scenario: Golden bytes
- **WHEN** the digest test runs over its fixed resource set
- **THEN** the digest equals the literal recorded before the move to the library's export

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
