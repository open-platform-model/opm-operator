## MODIFIED Requirements

### Requirement: PlatformStatus carries conditions and observedGeneration

`PlatformStatus` SHALL carry `conditions` (a `metav1.Condition` list keyed by type), `observedGeneration` and `operatorVersion`. The Ready condition SHALL summarise module generation: `Ready=True` reason `Generated`; `Ready=False` reason `BuildFailed`, `GenerateFailed`, `OverSubscribedContracts` or `ComparablePredicates`. A `ContractsFulfilled` condition SHALL report the effective package's unfulfilled provider-fulfilled contracts without affecting Ready (reasons `UnfulfilledContracts`, `ContractsFulfilled`, `NoContractsDefined`). The field's documentation SHALL name every reason.

#### Scenario: Status reflects the generate-and-build outcome

- **WHEN** the Platform reconciles
- **THEN** `status.conditions` carries a Ready condition with one of the reasons `Generated`, `BuildFailed`, `GenerateFailed`, `OverSubscribedContracts` or `ComparablePredicates`

#### Scenario: Status carries the contract report

- **WHEN** the Platform reconciles to `Ready=True`
- **THEN** `status.conditions` carries a `ContractsFulfilled` condition with one of the reasons `UnfulfilledContracts`, `ContractsFulfilled` or `NoContractsDefined`

#### Scenario: Status subresource present

- **WHEN** the CRD is installed
- **THEN** `Platform` exposes a `/status` subresource, and `status.conditions`, `status.observedGeneration` and `status.operatorVersion` are part of the schema

#### Scenario: Operator printcolumn

- **WHEN** `kubectl get platform` runs against a reconciled Platform
- **THEN** the output includes an `Operator` column showing `.status.operatorVersion`
