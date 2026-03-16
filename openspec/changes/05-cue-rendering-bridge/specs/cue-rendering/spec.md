## ADDED Requirements

### Requirement: Module rendering from directory and values
The `internal/render` package MUST provide a `RenderModule` function that takes a CUE module directory path and optional CRD values, and returns rendered Kubernetes resources with inventory entries.

#### Scenario: Successful rendering
- **WHEN** the directory contains a valid CUE module and values are compatible with the module's `#config` schema
- **THEN** `RenderModule` returns a `RenderResult` with non-empty `Resources` and `InventoryEntries`

#### Scenario: Invalid values
- **WHEN** the provided values do not conform to the module's `#config` schema
- **THEN** `RenderModule` returns an error wrapping `pkg/errors.ConfigError`

#### Scenario: No values provided
- **WHEN** `values` is nil (module has no required config or has defaults)
- **THEN** `RenderModule` succeeds using the module's default values

#### Scenario: Module has no components
- **WHEN** the CUE module evaluates to a spec with no components
- **THEN** `RenderModule` returns an error indicating no components found

### Requirement: Runtime labels injection
The render bridge MUST inject `#runtimeLabels` with `app.kubernetes.io/managed-by: opm-controller` before CUE evaluation so that all rendered resources carry controller identity metadata.

#### Scenario: Managed-by label present on resources
- **WHEN** rendering completes successfully
- **THEN** every resource in the result carries `app.kubernetes.io/managed-by: opm-controller` in its labels

### Requirement: RenderResult includes inventory entries
The `RenderResult` MUST include `[]v1alpha1.InventoryEntry` built from the rendered resources, using the inventory bridge from change 1.

#### Scenario: Inventory entries match resources
- **WHEN** rendering produces N resources
- **THEN** the result contains N inventory entries with correct Group, Kind, Namespace, Name, and Component fields

### Requirement: CRD values to CUE conversion
The render bridge MUST convert `v1alpha1.RawValues` (JSON bytes) to a `cue.Value` for the CLI pipeline.

#### Scenario: Valid JSON values
- **WHEN** `RawValues` contains valid JSON
- **THEN** the JSON is compiled into a `cue.Value` and passed to `ParseModuleRelease`

#### Scenario: Invalid JSON values
- **WHEN** `RawValues` contains malformed JSON
- **THEN** `RenderModule` returns an error before attempting CUE evaluation
