## MODIFIED Requirements

### Requirement: An over-subscribed platform is refused at generation

After a generated platform module builds, the reconciler SHALL read the built platform's contract inventory before recording the package. When any provider-fulfilled contract is provided by more than one enabled registry entry (the inventory is not routable; 0010 D37, enhancement 0015 D2 and D18), the reconciler SHALL refuse the package: `Ready=False` with reason `OverSubscribedContracts` and a message naming, in a deterministic order, each over-subscribed contract, the catalog that defines it when an enabled catalog does, and every registry entry that provides it.

The count SHALL be the platform's own, as core computes it and the render build refuses on: every transformer of every enabled registry entry counts, a provider is identified by its registry entry (the catalog module path with its major), and a contract counts whether or not an enabled catalog defines it. The operator SHALL NOT recount it, so the generation gate and the render refusal fire on the same platforms.

#### Scenario: Two catalogs providing one contract are refused

- **WHEN** the built platform's inventory lists a provider-fulfilled contract provided by each of two enabled registry entries
- **THEN** the Platform reports `Ready=False` with reason `OverSubscribedContracts`, the message names the contract, its defining catalog and both providing registry entries, and no package is recorded for that tuple

#### Scenario: Two majors of one provider catalog are refused

- **WHEN** two majors of one provider catalog are enabled and each carries a transformer requiring the same provider-fulfilled contract
- **THEN** the Platform reports `Ready=False` with reason `OverSubscribedContracts`, the message names both registry entries (the same path with two majors), and no package is recorded for that tuple

#### Scenario: Providers of a contract whose defining catalog is not enabled are refused

- **WHEN** the catalog defining a provider-fulfilled contract is disabled or absent and two enabled registry entries provide that contract
- **THEN** the Platform reports `Ready=False` with reason `OverSubscribedContracts`, the message names the contract and both providing registry entries with no defining catalog, and no package is recorded for that tuple
