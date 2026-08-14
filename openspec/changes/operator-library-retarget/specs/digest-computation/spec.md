# digest-computation — Delta

## MODIFIED Requirements

### Requirement: Source digest formula is a frozen cross-repo contract

`ModuleSourceDigest` SHALL remain exactly `sha256(modulePath + "@" + moduleVersion)` rendered as `sha256:%x` over the two `spec.module` strings, byte-identical to the CLI's `sourceDigest` — the two actors' no-op detection depends on the equality and neither side may change the formula unilaterally. The formula SHALL be pinned by a golden test naming the peer implementation.

#### Scenario: Golden pin

- **WHEN** the digest test runs
- **THEN** `ModuleSourceDigest` over a fixed coordinate equals the recorded literal digest
- **AND** the test's comment names the CLI peer that must move in lockstep
