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
