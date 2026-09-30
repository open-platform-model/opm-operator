## MODIFIED Requirements

### Requirement: A contract has one provider

Acceptance SHALL refuse a claim whose `provides` names a contract that an enabled entry of the built platform's registry or another **active** claim already provides, naming the holder and the contract (enhancement 0015 D2, keeping 0010 D37's exactly-one-provider rule). This refusal is distinct from the duplicate-claim refusal: that one is about two claims naming the same catalog, this one is about two providers of the same contract, which can arrive from different catalogs entirely.

Which enabled entries provide a contract SHALL be read from the built platform's own provider count (the contract inventory's providers, which core computes once and the render build refuses on), never recounted by the operator. A provider SHALL be identified by its registry entry: the catalog module path with its major, the same string a claim's `spec.catalog` and a `spec.registry` key carry. Two majors of one catalog are two providers. When the built platform's provider count cannot be read, acceptance SHALL defer the verdict (not refuse), as it does when no platform has been built.

An **inactive** accepted claim SHALL NOT hold a contract against a competitor, because a claim that has never served has no dependents to protect.

A claim whose deletion is **blocked** SHALL keep holding its contracts. Its catalog is still supplying them to the generated platform, so admitting a second provider would not replace it: it would over-subscribe the contract and refuse platform generation for every instance in the cluster. Refusing the newcomer names both ends of the problem instead.

A claim's **own** catalog SHALL NOT hold a contract against it. Once a claim is active its catalog is an enabled entry of the very platform the next reconcile judges it against (enhancement 0015 D13), so counting the claim's own entry would refuse the claim for providing what it exists to provide, deactivate it, drop its catalog from the next generated package and accept it again, an oscillation rather than a verdict. The claim's own entry is the registry entry equal to its `spec.catalog`; another major of the same catalog is not its own entry. Only ANOTHER provider of the contract is a conflict, and the claim's own catalog being excused SHALL NOT excuse a different catalog providing the same contract.

#### Scenario: A claim is refused against an enabled subscription

- **WHEN** a claim provides a contract an enabled registry subscription's catalog already provides
- **THEN** it is refused, naming the contract and the subscribed catalog by its registry entry (path with major)

#### Scenario: A claim is refused against an active claim

- **WHEN** a claim provides a contract another active claim already provides
- **THEN** it is refused, naming the contract and the holding claim

#### Scenario: An inactive accepted claim does not block a competitor

- **WHEN** the holder of a contract is accepted but has never activated
- **THEN** a competing claim for that contract is not refused against it

#### Scenario: An active claim is not refused against its own catalog

- **WHEN** an active claim is re-judged against a platform whose registry now carries its own catalog, with transformers stamped the way core stamps them (a major-free module path)
- **THEN** it is not refused for providing its own contracts

#### Scenario: Another provider still refuses a claim whose catalog is in the registry

- **WHEN** a different catalog provides the same contract as an active claim whose own catalog is in the registry
- **THEN** the competing claim is still refused

#### Scenario: A blocked claim still holds its contracts against another catalog

- **WHEN** a claim from a different catalog providing the same contract is judged while the holder's deletion is blocked
- **THEN** it is refused, naming the blocked claim

#### Scenario: Another major of the claim's own catalog is another provider

- **WHEN** a claim for one major of a catalog provides a contract that an enabled registry entry for a different major of the same catalog already provides
- **THEN** it is refused with reason `ContractSubscribed`, naming the contract and the other major's registry entry

#### Scenario: An unreadable provider count defers the verdict

- **WHEN** the built platform's contract inventory cannot be read
- **THEN** the claim is not refused: it reports `Ready=Unknown` with reason `PlatformNotReady` and is requeued
