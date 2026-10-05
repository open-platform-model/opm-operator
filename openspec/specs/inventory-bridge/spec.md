## Purpose

The boundary between the API's inventory entry (`v1alpha1.InventoryEntry`, the shape `status.inventory` stores) and the library's `opm/k8s/inventory`, which owns the entry, the stale set and the inventory digest. The operator converts between the two entry types and builds its entries from its one export of a render.

## Requirements

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

### Requirement: Inventory type alias preserved
The existing `type Current = releasesv1alpha1.Inventory` alias in `internal/inventory` MUST be preserved as a semantic marker used by other internal packages.

#### Scenario: Alias names the API inventory type
- **WHEN** `internal/inventory` is inspected
- **THEN** it declares `Current` as an alias of the API's `Inventory` type, not a distinct type
