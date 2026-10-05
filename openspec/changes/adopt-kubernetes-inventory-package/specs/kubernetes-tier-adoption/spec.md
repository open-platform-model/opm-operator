## MODIFIED Requirements

### Requirement: The reconciler converts a render with one export

The reconciler SHALL convert a render's resources with one call to the library's `object.Export`, which exports each resource from CUE once. It SHALL take the render digest, the unstructured objects it applies and the inventory entries from that one result, inside the render slot. It SHALL drop the rendered CUE values as soon as the export returns, so the digest and the entries read exported data only. No other code path SHALL export a rendered resource. A failure of the export SHALL be reported with the reason and message the reconciler reported before: a value that will not export is a render failure named "computing render digest", and exported JSON that will not decode to an object is an apply failure named "converting resources" with the resource.

#### Scenario: Digest and apply objects from one export

- **WHEN** a render of N resources is converted for apply
- **THEN** the render digest equals the library's `inventory.RenderDigest` over `object.Export` of the same resources, and the N apply objects are that export's `Exported.Object` values in input order

#### Scenario: Export failure keeps its reason

- **WHEN** one rendered value fails to export
- **THEN** the conversion fails with reason `RenderFailed` and a message that begins "computing render digest", and the rendered resources are dropped

#### Scenario: Decode failure keeps its reason

- **WHEN** one rendered value exports to JSON that does not decode to an object
- **THEN** the conversion fails with reason `ApplyFailed` and a message that begins "converting resources", and the rendered resources are dropped

#### Scenario: Inventory entries from the same export

- **WHEN** a render of N resources is converted for apply
- **THEN** the conversion carries N inventory entries, one per `Exported.Object` in input order, and the render result carries none

## ADDED Requirements

### Requirement: The operator uses the library's inventory package and keeps no copy

The operator SHALL compute the inventory stale set, the inventory digest and the render digest only with the library's `opm/k8s/inventory` (`StaleSet`, `Digest`, `RenderDigest`). It SHALL build inventory entries with that package's `NewEntry`. It SHALL declare no entry constructor, identity relation, stale-set function, inventory digest or render digest of its own. Its only inventory code is the conversion between the API's `InventoryEntry` and `inventory.Entry`. Source: 0012:D3:R6.

#### Scenario: No local copy

- **WHEN** the operator's Go source is searched for `ComputeStaleSet`, `ComputeDigest`, `NewEntryFromResource`, `IdentityEqual`, `K8sIdentityEqual` or a render digest function in `internal/status`
- **THEN** none is found, and every reader calls `opm/k8s/inventory`
