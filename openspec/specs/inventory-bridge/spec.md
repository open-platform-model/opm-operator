## Purpose

The boundary between the API's inventory entry (`v1alpha1.InventoryEntry`, the shape `status.inventory` stores) and the library's `opm/k8s/inventory`, which owns the entry, the stale set and the inventory digest. The operator converts between the two entry types and builds its entries from its one export of a render.

## Requirements

### Requirement: Inventory type alias preserved
The existing `type Current = releasesv1alpha1.Inventory` alias in `internal/inventory` MUST be preserved as a semantic marker used by other internal packages.

#### Scenario: Alias names the API inventory type
- **WHEN** `internal/inventory` is inspected
- **THEN** it declares `Current` as an alias of the API's `Inventory` type, not a distinct type

### Requirement: Inventory entries convert losslessly to and from the library's Entry
The `internal/inventory` package MUST provide the conversions between `v1alpha1.InventoryEntry` and the library's `inventory.Entry` (`opm/k8s/inventory`), one entry and a slice of entries each way. Group, Kind, Namespace, Name, Version and Component MUST map one to one, so a round trip in either direction returns an equal value. The package MUST declare no identity relation, stale-set function or digest of its own. Those are the library's (0012:D7).

#### Scenario: Round trip from the API entry
- **WHEN** a `v1alpha1.InventoryEntry` with every field set is converted to `inventory.Entry` and back
- **THEN** the result equals the original

#### Scenario: Round trip keeps empty fields empty
- **WHEN** a core-group, cluster-scoped entry with empty Group, Namespace, Version and Component is converted both ways
- **THEN** every empty field stays empty

#### Scenario: No local stale set or digest
- **WHEN** `internal/inventory` is inspected
- **THEN** it declares no stale-set, identity-comparison or digest function

#### Scenario: The exported surface is closed
- **WHEN** the package's tests run
- **THEN** a test parses its non-test files and requires its exported names to be exactly `Current`, `FromEntries`, `FromEntry`, `ToEntries` and `ToEntry`, so any further export fails the tests

### Requirement: The stale set is the library's component-blind stale set
The ModuleInstance and ModulePackage reconcilers MUST compute the stale set with `inventory.StaleSet` from `opm/k8s/inventory`, over the previous inventory's entries and the entries of the current render. An entry is stale only when no current entry has the same Group, Kind, Namespace and Name. Version and Component never count, so a component rename or an API version change leaves nothing stale. Stale entries keep the previous inventory's order. Source: 0012:D7:R1.

#### Scenario: Stale entries detected
- **WHEN** the previous inventory contains entries A, B, C and the current contains A, C
- **THEN** the stale set contains only entry B

#### Scenario: No stale entries
- **WHEN** previous and current inventories contain the same entries
- **THEN** the stale set is empty

#### Scenario: A version change leaves nothing stale
- **WHEN** an entry exists in both previous and current with different `Version` values
- **THEN** the entry is not in the stale set

#### Scenario: A component rename leaves nothing stale
- **WHEN** an entry exists in both previous and current with identical Group, Kind, Namespace and Name but different `Component` values
- **THEN** the entry is not in the stale set
- **AND** the live object is preserved: server-side apply patches it in place with the new component label, and prune does not delete it

### Requirement: The inventory digest is the library's canonical digest
`status.inventory.digest` MUST be `inventory.Digest` from `opm/k8s/inventory` over the inventory's entries. That digest hashes a versioned canonical encoding of each entry's field values, so it depends neither on the order of the entries nor on the JSON tags of the CRD entry type. Source: 0012:D7:R2/R3.

#### Scenario: Order does not matter
- **WHEN** the same entries are digested in a different order
- **THEN** the digest is identical

#### Scenario: Content does
- **WHEN** an entry's `Name` changes
- **THEN** the digest differs

#### Scenario: The stored digest changes once on upgrade
- **WHEN** an instance whose `status.inventory.digest` was written by an earlier operator is applied by this operator with the same entries
- **THEN** the new `status.inventory.digest` equals `inventory.Digest` of those entries and differs from the stored value

### Requirement: Inventory entries come from the one export
The reconciler MUST build the inventory entries of a render from the objects that the render's one `object.Export` decoded. It MUST build one entry per object with the library's `inventory.NewEntry`, which reads group, version, kind, namespace, name and the `component.opmodel.dev/name` label, over the full rendered set, before any resource is withheld from the apply list. The render result MUST NOT carry inventory entries, and no rendered resource MUST be exported a second time for them.

#### Scenario: Entry built from an exported object
- **WHEN** a render exports an object with GVK `apps/v1/Deployment`, namespace `default`, name `nginx` and label `component.opmodel.dev/name=web`
- **THEN** its inventory entry has Group `apps`, Kind `Deployment`, Version `v1`, Namespace `default`, Name `nginx` and Component `web`

#### Scenario: A withheld resource keeps its entry
- **WHEN** a render of three resources is converted and one of them is withheld from the apply list
- **THEN** the converted entries name all three resources, and the apply list holds the other two
