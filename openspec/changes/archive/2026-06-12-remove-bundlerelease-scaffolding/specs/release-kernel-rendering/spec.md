# release-kernel-rendering — delta

## ADDED Requirements

### Requirement: Non-ModuleRelease packages are rejected

For a fetched package whose `kind` is anything other than `ModuleRelease`, the renderer SHALL return `ErrUnsupportedKind` and the reconciler SHALL surface `Ready=False` with reason `UnsupportedKind` and `Stalled=True`. The rejection SHALL NOT name speculative kinds: the kernel's `#ModuleRelease` load gate (`loaderfile.ErrWrongKind`) is the detection mechanism, and the resulting error is generic.

#### Scenario: Wrong-kind package is rejected

- **WHEN** a `Release` whose fetched package has a `kind` other than `ModuleRelease` is reconciled
- **THEN** rendering returns an unsupported-kind error
- **AND** the status reflects `UnsupportedKind` and nothing is applied

## REMOVED Requirements

### Requirement: BundleRelease remains unsupported

**Reason**: The `BundleRelease` CRD and all bundle scaffolding are removed; `BundleRelease` is no longer a kind the system names anywhere. The protective behavior (reject non-`ModuleRelease` packages with `ErrUnsupportedKind`) is preserved generically by the added requirement above.
**Migration**: None — the observable stall semantics (`Ready=False`, reason `UnsupportedKind`, `Stalled=True`, nothing applied) are unchanged; only the error message no longer references BundleRelease.
