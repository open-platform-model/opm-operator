## MODIFIED Requirements

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
