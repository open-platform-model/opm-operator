## MODIFIED Requirements

### Requirement: Surface materialize outcome on status

The reconciler SHALL record the outcome on the `Platform` status: `Ready=True` with reason `Generated` on success, `Ready=False` with reason `BuildFailed` (a dependency did not resolve, the module did not build, or its contract inventory could not be read; the message names the dependency, the registry entry or the field), `GenerateFailed` (the module could not be written), `ContractCollisions` (the built platform's inventory reports contract keys that more than one enabled registry entry defines; the message names each colliding key and the registry entries defining it), `OverSubscribedContracts` (the built platform's inventory is not routable; the message names each over-subscribed contract, its defining catalog when one is enabled and the registry entries providing it) or `ComparablePredicates` (the built platform's inventory is not discriminated; the message names each comparable pair and the contracts it shares). When several refusals hold, the reason SHALL be the first of `ContractCollisions`, `OverSubscribedContracts`, `ComparablePredicates` that applies, and the message SHALL carry every finding. On success the reconciler SHALL also write the non-gating `ContractsFulfilled` condition from the effective package's inventory. `status.observedGeneration` SHALL be set on every outcome. A failure or a refusal SHALL NOT overwrite a previously recorded good module.

#### Scenario: Success sets Ready and observedGeneration

- **WHEN** generation and build succeed for generation N and the built inventory is routable and discriminated
- **THEN** `status.conditions` carries `Ready=True` (reason `Generated`), a `ContractsFulfilled` condition, and `status.observedGeneration == N`

#### Scenario: Materialize failure surfaces structured error

- **WHEN** a pinned build does not exist or an entry's key disagrees with its imported catalog
- **THEN** `status.conditions` carries `Ready=False` (reason `BuildFailed`) with a message naming the path and version or the registry entry

#### Scenario: A refusal surfaces a structured verdict

- **WHEN** the module builds and its inventory reports a colliding contract key, or is not routable, or is not discriminated
- **THEN** `status.conditions` carries `Ready=False` with reason `ContractCollisions`, `OverSubscribedContracts` or `ComparablePredicates` and a message naming the contracts involved and the defining registry entries, the providing registry entries or the comparable transformers

#### Scenario: Failure preserves last-good materialized platform

- **WHEN** a previously recorded good module exists and a subsequent reconcile fails or is refused
- **THEN** the store still returns the last-good record and the failure or refusal is reflected only on the Ready condition
