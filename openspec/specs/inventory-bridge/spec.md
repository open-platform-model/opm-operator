## ADDED Requirements

### Requirement: Inventory identity comparison
The `internal/inventory` package MUST provide `IdentityEqual` and `K8sIdentityEqual` functions operating on `v1alpha1.InventoryEntry`. `IdentityEqual` compares Group, Kind, Namespace, Name, and Component (excluding Version). `K8sIdentityEqual` compares Group, Kind, Namespace, and Name only.

#### Scenario: Version excluded from identity
- **WHEN** two entries differ only in `Version`
- **THEN** `IdentityEqual` returns true

#### Scenario: Component excluded from K8s identity
- **WHEN** two entries differ only in `Component`
- **THEN** `K8sIdentityEqual` returns true but `IdentityEqual` returns false

### Requirement: Stale set computation
The `internal/inventory` package MUST expose a `ComputeStaleSet` function that accepts previous and current `[]v1alpha1.InventoryEntry` slices and returns entries present in previous but absent from current, using `K8sIdentityEqual` for comparison. `K8sIdentityEqual` compares Group, Kind, Namespace, and Name only — Component is excluded so that moving a resource between component labels (a routine CUE refactor) does not produce a stale entry for the live object that SSA apply patches in place.

#### Scenario: Stale entries detected
- **WHEN** the previous inventory contains entries A, B, C and the current contains A, C
- **THEN** the stale set contains only entry B

#### Scenario: No stale entries
- **WHEN** previous and current inventories contain the same entries
- **THEN** the stale set is empty

#### Scenario: Version changes do not create stale entries
- **WHEN** an entry exists in both previous and current but with different `Version` values
- **THEN** the entry is NOT included in the stale set

#### Scenario: Component renames do not create stale entries
- **WHEN** an entry exists in both previous and current with identical Group, Kind, Namespace, and Name but different `Component` values (a CUE refactor moved the resource between components)
- **THEN** the entry is NOT included in the stale set
- **AND** the live object is preserved (SSA apply patches it in place with the new component label, prune does not delete it)

### Requirement: Inventory digest computation
The `internal/inventory` package MUST expose a `ComputeDigest` function that accepts `[]v1alpha1.InventoryEntry` and returns a deterministic SHA-256 digest string in the format `sha256:<hex>`.

#### Scenario: Deterministic digest
- **WHEN** the same entries are provided in different order
- **THEN** the computed digest is identical

#### Scenario: Digest changes on content change
- **WHEN** an entry's `Name` field is changed
- **THEN** the computed digest differs from the original

### Requirement: Entry construction from unstructured resource
The `internal/inventory` package MUST expose a `NewEntryFromResource` function that creates a `v1alpha1.InventoryEntry` from an `*unstructured.Unstructured`, extracting GVK fields, namespace, name, and the component label.

#### Scenario: Entry constructed from resource
- **WHEN** an unstructured resource with GVK `apps/v1/Deployment`, namespace `default`, name `nginx`, and label `component.opmodel.dev/name=web` is provided
- **THEN** the returned entry has Group=`apps`, Kind=`Deployment`, Version=`v1`, Namespace=`default`, Name=`nginx`, Component=`web`

### Requirement: Component label constant
The controller MUST define a `LabelComponentName` constant with value `component.opmodel.dev/name`, used by `NewEntryFromResource` to extract the component name from resource labels.

### Requirement: CLI packages copied to `pkg/`
The controller MUST contain locally copied CLI packages under `pkg/` with all internal import paths rewritten from `github.com/opmodel/cli/pkg/` to `github.com/open-platform-model/opm-operator/pkg/`. The following packages MUST be present: `core`, `errors`, `validate`, `provider`, `module`, `loader`, `render`, `resourceorder`. (`bundle` was excluded — not yet implemented in OPM.)

#### Scenario: Copied packages compile under the renamed module
- **WHEN** `go build ./pkg/...` is run from the module root
- **THEN** all packages compile without errors

#### Scenario: No stale reference to the old module path
- **WHEN** `go.mod` is inspected and all Go files under `pkg/` are searched
- **THEN** there is no `require` entry for `github.com/opmodel/cli` and no import path beginning with `github.com/open-platform-model/poc-controller/`

### Requirement: Process file remains in pkg/render (revised)
The `process_modulerelease.go` file MUST remain in `pkg/render/` with its original function name (`ProcessModuleRelease`). Relocation to `pkg/module` was infeasible due to import cycles (`render → module` already exists; adding `module → render` creates a cycle). Bundle processing was excluded — not yet implemented in OPM.

#### Scenario: ProcessModuleRelease available in render package
- **WHEN** `pkg/render/` is inspected
- **THEN** it contains `ProcessModuleRelease` in `process_modulerelease.go`

## CHANGED Requirements

### Requirement: Inventory type alias preserved
The existing `type Current = releasesv1alpha1.Inventory` alias in `internal/inventory` MUST be preserved as a semantic marker used by other internal packages.
