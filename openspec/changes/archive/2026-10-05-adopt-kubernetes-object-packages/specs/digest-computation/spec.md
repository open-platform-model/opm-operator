## MODIFIED Requirements

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
