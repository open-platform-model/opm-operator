## ADDED Requirements

### Requirement: An object adopted by another instance is excluded from drift detection and from the restore
Drift detection MUST compare only the objects the apply verdict allows. An object the verdict refuses as `adopted-elsewhere` MUST NOT be reported as drifted or as missing, and MUST NOT be restored. The read the verdict needs MUST be the one read made per object before the dry-run, so a reconcile that renders sends no more requests per object than before. A reconcile with unchanged digests whose read of an object fails MUST report that as a failed drift check, as before (`Drifted=Unknown` with reason `DriftCheckForbidden` when the read is Forbidden), MUST restore nothing and MUST let no object go in that reconcile. Source: 0012:D8:R8.

#### Scenario: A let-go object is not drift
- **GIVEN** an inventoried ConfigMap whose `opmodel.dev/adopt` annotation names another instance and whose data the other instance changed
- **WHEN** a reconcile renders with unchanged digests
- **THEN** `Drifted` does not name the ConfigMap

#### Scenario: A let-go object is not restored
- **GIVEN** a ConfigMap the instance let go, which the other instance then deleted and created again with its annotation
- **WHEN** the controller reconciles
- **THEN** the controller does not write the ConfigMap

#### Scenario: One read per object
- **GIVEN** a ModuleInstance with ten rendered objects and unchanged digests
- **WHEN** a reconcile renders
- **THEN** the controller sends one GET of its own per object before the dry-run, as before this requirement

#### Scenario: A refused read on unchanged digests
- **GIVEN** an effective ServiceAccount that may not get ConfigMaps, and unchanged digests
- **WHEN** the controller reconciles
- **THEN** `Drifted` is `Unknown` with reason `DriftCheckForbidden`, nothing is restored and `status.inventory` keeps its entries
