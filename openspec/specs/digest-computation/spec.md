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
The `internal/status` package MUST provide a function that computes a deterministic SHA-256 digest from a slice of rendered resources.

#### Scenario: Order-independent
- **WHEN** the same resources are provided in different order
- **THEN** the computed digest is identical (resources are sorted before hashing)

#### Scenario: Content sensitivity
- **WHEN** a resource's name or spec changes
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
