## Purpose

The Platform reconciler's read of the built platform's contract inventory: which of core's reports refuse platform-package generation, which only report, and how each surfaces on `Platform.status` (enhancement 0015 D5, D18).

## Requirements

### Requirement: An over-subscribed platform is refused at generation

After a generated platform module builds, the reconciler SHALL read the built platform's contract inventory before recording the package. When any provider-fulfilled contract is required by transformers from more than one enabled catalog (the inventory is not routable; 0010 D37, enhancement 0015 D2 and D18), the reconciler SHALL refuse the package: `Ready=False` with reason `OverSubscribedContracts` and a message naming each over-subscribed contract, the catalog that defines it and every transformer that requires it, in a deterministic order.

#### Scenario: Two catalogs providing one contract are refused

- **WHEN** the built platform's inventory lists a provider-fulfilled contract required by a transformer from each of two enabled catalogs
- **THEN** the Platform reports `Ready=False` with reason `OverSubscribedContracts`, the message names the contract, its defining catalog and both transformer FQNs, and no package is recorded for that tuple

### Requirement: An undiscriminated platform is refused at generation

When the built platform's inventory reports a pair of enabled transformers whose match predicates are comparable over a shared catalog-fulfilled contract (the inventory is not discriminated; enhancement 0015 D5), the reconciler SHALL refuse the package: `Ready=False` with reason `ComparablePredicates` and a message naming, for each reported pair, the broader transformer, the narrower transformer and the contracts they share, in a deterministic order. No arbitration, ordering or most-specific-wins SHALL be applied. When a platform is both over-subscribed and undiscriminated, the reason SHALL be `OverSubscribedContracts` and the message SHALL carry both findings.

#### Scenario: A comparable pair is refused naming both transformers

- **WHEN** the built platform's inventory reports one comparable pair over one catalog-fulfilled contract
- **THEN** the Platform reports `Ready=False` with reason `ComparablePredicates`, the message names the broader FQN, the narrower FQN and the shared contract, and no package is recorded for that tuple

#### Scenario: Both findings report under the routing reason

- **WHEN** the built platform's inventory is neither routable nor discriminated
- **THEN** the Platform reports `Ready=False` with reason `OverSubscribedContracts` and the message names the over-subscribed contracts and the comparable pairs

#### Scenario: Discriminated transformers coexist

- **WHEN** the built platform's inventory reports no comparable pair, however many transformers share a catalog-fulfilled contract
- **THEN** the gate refuses nothing on discrimination and the package is recorded

### Requirement: A refusal preserves the effective package and its status

A refused generation SHALL behave as a failed build toward everything outside the Ready condition: the process-local store SHALL keep the last good package, `status.packageIdentity` and `status.registry` SHALL keep describing it, `status.observedGeneration` SHALL be set, a warning event SHALL be emitted on transition only, and the reconcile SHALL requeue on the stalled recheck interval. Renders SHALL keep consuming the last good package. A refusal with no last good package SHALL leave the store empty, so instances wait at `PlatformNotReady` while the cause is on the Platform. The refused module directory MAY remain on disk until the next successful generation prunes it.

#### Scenario: The last good package survives a refusal

- **WHEN** a package is held for one tuple and the next tuple is refused
- **THEN** the store still returns the held package and its identity, `status.packageIdentity` and `status.registry` are unchanged, and the Ready condition alone carries the refusal

#### Scenario: A first-boot refusal holds nothing

- **WHEN** no package has been recorded and the first tuple is refused
- **THEN** the store holds no package, renders report `PlatformNotReady`, and the Platform's Ready condition names the refusal

### Requirement: The gate is re-evaluated from current state

A refusal SHALL be level-computed from the current tuple: a Platform edit or a claim change wakes the reconciler through its existing watches, and a tuple whose built inventory is routable and discriminated SHALL be recorded and reported `Ready=True` with reason `Generated`, with nothing to clear by hand.

#### Scenario: Removing the competing catalog recovers

- **WHEN** a refused tuple's competing catalog is disabled in `spec.registry` or its claim is removed
- **THEN** the next reconcile records the package and the Platform reports `Ready=True` with reason `Generated`

### Requirement: Unfulfilled contracts are reported, never refused

The reconciler SHALL write a `ContractsFulfilled` condition from the effective package's inventory on both success paths (a fresh build and the current-package skip) and SHALL declare it among the conditions it owns. `ContractsFulfilled=False` with reason `UnfulfilledContracts` SHALL name each provider-fulfilled contract nothing on the platform implements and the catalog that defines it; `ContractsFulfilled=True` with reason `ContractsFulfilled` SHALL report that every defined provider-fulfilled contract has a provider; `ContractsFulfilled=True` with reason `NoContractsDefined` SHALL report that the enabled catalogs list no contract, so nothing was verified. The condition SHALL NOT move `Ready` and SHALL NOT be rewritten by a refused generation, which describes only the package renders consume (enhancement 0015 D18).

#### Scenario: An unfulfilled contract is a report beside a generated platform

- **WHEN** the effective package's inventory lists a provider-fulfilled contract that no enabled transformer requires
- **THEN** the Platform reports `Ready=True` with reason `Generated` and `ContractsFulfilled=False` with reason `UnfulfilledContracts`, the message naming the contract and its defining catalog

#### Scenario: Every contract fulfilled

- **WHEN** the effective package's inventory defines contracts and lists none unfulfilled
- **THEN** the Platform reports `ContractsFulfilled=True` with reason `ContractsFulfilled`

#### Scenario: No contract defined is visible, not a pass

- **WHEN** the effective package's inventory defines no contract
- **THEN** the Platform reports `ContractsFulfilled=True` with reason `NoContractsDefined` and a message saying nothing was verified

#### Scenario: The skip path keeps the condition current

- **WHEN** a reconcile finds the held package already current for the tuple
- **THEN** `ContractsFulfilled` is written from the held package's inventory

### Requirement: An unreadable inventory is a build failure

When the built platform carries no readable contract inventory, or lacks a report field the library expects, the reconciler SHALL report `Ready=False` with reason `BuildFailed` naming the missing field, and SHALL NOT record the package.

#### Scenario: A platform without the report is not a pass

- **WHEN** the built platform's inventory cannot be decoded
- **THEN** the Platform reports `Ready=False` with reason `BuildFailed` and the message names the field the read failed on
