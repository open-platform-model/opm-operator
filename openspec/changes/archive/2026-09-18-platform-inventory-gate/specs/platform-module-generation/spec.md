## MODIFIED Requirements

### Requirement: The generated module is validated by building it

After writing the module, the reconciler SHALL build it through the kernel's shape-gated platform loader against the operator's configured registry, and after a successful build SHALL read the built platform's contract inventory and record the package only when the inventory is routable and discriminated (the platform-inventory-gate capability). The Ready condition SHALL reflect the outcome: Ready=True with reason `Generated` when the build succeeds and the gate passes; Ready=False with reason `BuildFailed`, with the error naming the failing dependency, entry or inventory field, when a pinned build does not exist (closure derivation or build), an entry's key disagrees with its imported catalog's declared module path, the build fails otherwise, or the inventory cannot be read; Ready=False with reason `GenerateFailed` when the module could not be written to disk; Ready=False with reason `OverSubscribedContracts` or `ComparablePredicates` when the module built and the gate refused it. The materialize-era reasons (`Materialized`, `MaterializeFailed`) are retired. A failed or refused reconcile SHALL leave the previously recorded module (if any) in place.

#### Scenario: Clean build sets Ready

- **WHEN** the generated module's pins name published builds, the build succeeds and the inventory is routable and discriminated
- **THEN** the Platform CR reports Ready=True and records the reconciled generation

#### Scenario: Nonexistent pin surfaces on the CR

- **WHEN** `spec.registry` names a catalog version that is not published
- **THEN** the Platform CR reports Ready=False with reason `BuildFailed` and a message naming the catalog path and version

#### Scenario: A failed build keeps the last good module

- **WHEN** a Platform generation N built successfully and generation N+1 fails to build
- **THEN** the process-local record still names generation N's module directory, and the CR reports Ready=False for generation N+1

#### Scenario: A built but refused module keeps the last good module

- **WHEN** a Platform generation N built and passed the gate and generation N+1 builds but its inventory is not routable or not discriminated
- **THEN** the process-local record still names generation N's module, and the CR reports Ready=False for generation N+1 with the refusal's reason
