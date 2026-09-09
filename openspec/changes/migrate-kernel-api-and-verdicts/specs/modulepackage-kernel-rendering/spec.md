## MODIFIED Requirements

### Requirement: Non-ModuleRelease packages are rejected

For a fetched package whose `kind` is anything other than `ModuleInstance`, the renderer SHALL return `ErrUnsupportedKind` and the reconciler SHALL surface `Ready=False` with reason `UnsupportedKind` and `Stalled=True`. The rejection SHALL NOT name speculative kinds: the kernel's `#ModuleInstance` shape gate (`oerrors.ErrWrongKind`, the sentinel the library's `opm/errors` package declares) is the detection mechanism, and the resulting error is generic.

#### Scenario: Wrong-kind package is rejected

- **WHEN** a `ModulePackage` whose fetched package has a `kind` other than `ModuleInstance` is reconciled
- **THEN** rendering returns an unsupported-kind error
- **AND** the status reflects `UnsupportedKind` and nothing is applied
