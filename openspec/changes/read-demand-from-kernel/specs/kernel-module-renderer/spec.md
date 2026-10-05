## MODIFIED Requirements

### Requirement: Render a ModuleRelease through the kernel

`KernelModuleRenderer` SHALL acquire the module from the registry, synthesize the instance (source-carrying) with the supplied values, and render it through the single-build render with the leased platform record and the record's skew policy. Every kernel call SHALL share nothing: acquisition, synthesis and the build each evaluate in a context of their own, so renders of different objects overlap with no gate.

Values SHALL reach synthesis as a stack of values sources, each carrying an origin that identifies where the operator read it from, so a values error names that origin rather than an anonymous filename. No acquisition or synthesis call SHALL pass a per-call registry or load-options argument: the registry mapping is the one the shared Kernel was constructed with.

The renderer SHALL NOT check the values against the module's `#config` itself: the kernel's instance synthesis is the one check. A synthesis failure SHALL be reported with every CUE finding it carries, each followed by its source positions, under the frame `synthesizing release: `, and SHALL keep the library's error in its chain so a typed cause (a registry fetch failure among them) is still found by type.

#### Scenario: Renders resources from a generated platform

- **WHEN** `RenderModule` is called for a resolvable module while a generated platform is recorded
- **THEN** the result carries the rendered resources and inventory entries, and any warnings the render reported

#### Scenario: Values are applied when supplied

- **WHEN** non-nil `RawValues` are passed
- **THEN** they are supplied to instance synthesis as a values source whose origin names the CR field they came from
- **AND** when no values are supplied the module's `#config` defaults apply

#### Scenario: A values error names its origin

- **WHEN** the supplied raw values violate the module's `#config` schema
- **THEN** the render fails with an error naming the origin the operator gave that source, not an anonymous filename
- **AND** the error lists each finding with its line and column in that source, for example `spec.values:1:13`

#### Scenario: Values are checked once, by synthesis

- **WHEN** `RenderModule` is called with values
- **THEN** the renderer calls no `#config` validation of its own before synthesis
- **AND** a values conflict is reported from instance synthesis, framed `synthesizing release: `

#### Scenario: A values error stalls as a render failure

- **WHEN** a ModuleInstance's `spec.values` violate the module's `#config`
- **THEN** `status.conditions` reports `Ready=False` with reason `RenderFailed` and `Stalled=True`, and the condition message is the renderer's error unchanged

#### Scenario: A synthesis failure without CUE findings reads unchanged

- **WHEN** synthesis fails with an error whose chain holds no CUE error, such as a registry fetch failure
- **THEN** the error reads `synthesizing release: ` followed by the library's message unchanged, and its typed cause is still found by type

## ADDED Requirements

### Requirement: The render reports the instance's contract demand from the kernel

Both kernel renderers (`KernelModuleRenderer` for a ModuleInstance, `KernelPackageRenderer` for a ModulePackage) SHALL report on `RenderResult.RequiredContracts` the contract demand the kernel's render reports on its diagnostics: every `#resources` and `#traits` key of every component of the instance, sorted and deduplicated. The operator SHALL NOT compute the demand itself by reading the instance's components. An empty demand SHALL be reported as an empty, non-nil list. Source: 0013:D24.

The demand is read only from a successful render; this requirement does not change when the reconciler writes it (see `reconcile-loop-assembly`, "The instance's contract demand is recorded on its status").

#### Scenario: A rendered module reports its components' contracts

- **WHEN** `RenderModule` renders a fixture module whose components declare resources and traits
- **THEN** the result's `RequiredContracts` is exactly the sorted, deduplicated set of those contract FQNs

#### Scenario: Both renderers fill the demand the same way

- **WHEN** a ModuleInstance and a ModulePackage render instances of the same module
- **THEN** both results carry the demand the kernel reported for their render, read through the same adapter

#### Scenario: An empty demand is an empty list

- **WHEN** the kernel's render reports no contracts, or reports the list as absent
- **THEN** the result's `RequiredContracts` is an empty, non-nil list

#### Scenario: No operator walk of the components

- **WHEN** the operator's render package is searched for a reader of a component's `#resources` or `#traits`
- **THEN** none exists; the demand comes from the kernel's render diagnostics only
