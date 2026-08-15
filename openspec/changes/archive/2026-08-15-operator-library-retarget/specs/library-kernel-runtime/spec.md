# library-kernel-runtime — Delta

## MODIFIED Requirements

### Requirement: Embedded kernel line and compile semantics

The operator SHALL embed the library at the core-v2 line (v1.0.0-alpha.13 or later): the default schema loader resolves `opmodel.dev/core@v2`, module acquisition verifies declared identity against the fetched coordinate (a mismatch is a typed identity error), and compile SHALL fail — not warn — on unresolved demands: undemandable resources and unhandled traits whose effective `optional` is false. Render warnings SHALL carry only effectively-optional unhandled traits.

#### Scenario: Unresolved demand stalls the instance

- **WHEN** a rendered module demands a resource contract the materialized platform does not provide
- **THEN** compile fails and the ModuleInstance stalls with reason RenderFailed
- **AND** no partial render is applied

#### Scenario: Identity mismatch at module acquire

- **WHEN** a published module's declared metadata disagrees with the coordinate it was fetched by
- **THEN** the render fails with the typed identity error naming both values

#### Scenario: Optional trait still degrades to a warning

- **WHEN** an unhandled trait's effective `optional` is true
- **THEN** the render succeeds and the warning channel carries the trait
