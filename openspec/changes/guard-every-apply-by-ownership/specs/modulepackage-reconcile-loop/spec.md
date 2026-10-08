## MODIFIED Requirements

### Requirement: No-op detection
The `ReleaseReconciler` MUST detect no-op reconciliations when source artifact revision, config, render, and inventory digests all match the last applied values. A reconcile whose digests match is not a no-op when a rendered object that the apply verdict allows exists outside `status.inventory`, or when an inventoried object is adopted by another instance: it then applies the objects the verdict allows and records the inventory without the adopted object.

#### Scenario: All digests match
- **WHEN** source artifact digest, config digest, render digest, and inventory digest all match the last applied values
- **AND** every rendered object that exists is in `status.inventory` and none is adopted by another instance
- **THEN** the controller skips apply and prune, keeps `Ready=True`, and requeues with interval

#### Scenario: All digests match and an object was adopted by another instance
- **GIVEN** a Ready ModulePackage and an inventoried ConfigMap that a user annotates `opmodel.dev/adopt` with another instance's UUID
- **WHEN** a reconcile renders with matching digests
- **THEN** the ConfigMap is not written, `status.inventory` no longer lists it and `Ready` stays `True`

## ADDED Requirements

### Requirement: A ModulePackage guards every apply by ownership
A ModulePackage reconcile that renders MUST judge every rendered object with the library's apply verdict before its first write, with the package's recorded instance identities, exactly as a ModuleInstance reconcile does (`ssa-apply`, "An apply is judged by the ownership verdict before its first write"; `reconcile-loop-assembly`, "A reconcile the apply verdict refuses is retried and not stalled"). The read MUST be made by the client that applies. A reconcile that skips its render MUST NOT read the objects. Source: 0012:D8:R4.

#### Scenario: A package is refused on another instance's object
- **GIVEN** a ModulePackage whose render names a ConfigMap that a ModuleInstance holds and that is not in the package's inventory
- **WHEN** the controller reconciles the package
- **THEN** nothing is written, and the package reports `Ready=False` with reason `ApplyRefused` and no `Stalled` condition

#### Scenario: A package adopts an annotated object
- **GIVEN** the ConfigMap of the scenario above annotated `opmodel.dev/adopt` with the package's `status.instanceUUID`
- **WHEN** the retry runs
- **THEN** the render is applied and the ConfigMap is listed in the package's `status.inventory`

#### Scenario: A skipped render reads nothing
- **GIVEN** a ModulePackage whose render inputs are unchanged inside the drift render interval
- **WHEN** the controller reconciles
- **THEN** no rendered object is read for the verdict
