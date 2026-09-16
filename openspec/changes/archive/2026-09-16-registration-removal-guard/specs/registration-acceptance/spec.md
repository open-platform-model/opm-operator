## MODIFIED Requirements

### Requirement: A second claim for one provider is refused naming the claimant

When more than one claim names the same provider catalog, acceptance SHALL accept at most one and SHALL refuse the others naming the claimant that holds it (enhancement 0015 D12). The refusal SHALL identify the competing claim by name, so an operator can see which object to remove. The decision SHALL be stable: which claim holds acceptance SHALL NOT change from one reconcile to the next while both exist.

A claim being deleted SHALL NOT compete for the provider — **unless its deletion is blocked**, in which case it SHALL keep holding it. A blocked claim has not left: it is still accepted, still active, and its catalog is still an entry of the generated platform, so a second claim for that catalog is the duplicate this requirement refuses.

#### Scenario: The second claim is refused naming the first

- **WHEN** two instances of one provider module each render a claim for the same catalog
- **THEN** one is accepted and the other is refused, naming the accepted claim

#### Scenario: The holder does not change under repeated reconciles

- **WHEN** both claims are reconciled repeatedly with no spec change
- **THEN** the same claim keeps acceptance

#### Scenario: A claim whose deletion is blocked keeps holding its provider

- **WHEN** a claim for the same catalog is judged while the holder's deletion is blocked by its dependents
- **THEN** the new claim is refused, naming the blocked claim

### Requirement: A contract has one provider

Acceptance SHALL refuse a claim whose `provides` names a contract that an enabled entry of the built platform's registry or another **active** claim already provides, naming the holder and the contract (enhancement 0015 D2, keeping 0010 D37's exactly-one-provider rule). This refusal is distinct from the duplicate-claim refusal: that one is about two claims naming the same catalog, this one is about two providers of the same contract, which can arrive from different catalogs entirely.

An **inactive** accepted claim SHALL NOT hold a contract against a competitor, because a claim that has never served has no dependents to protect.

A claim whose deletion is **blocked** SHALL keep holding its contracts. Its catalog is still supplying them to the generated platform, so admitting a second provider would not replace it — it would over-subscribe the contract and refuse platform generation for every instance in the cluster. Refusing the newcomer names both ends of the problem instead.

A claim's **own** catalog SHALL NOT hold a contract against it. Once a claim is active its catalog is an enabled entry of the very platform the next reconcile judges it against (enhancement 0015 D13), so counting the claim's own entry would refuse the claim for providing what it exists to provide, deactivate it, drop its catalog from the next generated package and accept it again — an oscillation, not a verdict. Only ANOTHER provider of the contract is a conflict, and the claim's own catalog being excused SHALL NOT excuse a different catalog providing the same contract.

#### Scenario: A claim is refused against an enabled subscription

- **WHEN** a claim provides a contract an enabled registry subscription's catalog already provides
- **THEN** it is refused, naming the contract and the subscribed catalog

#### Scenario: A claim is refused against an active claim

- **WHEN** a claim provides a contract another active claim already provides
- **THEN** it is refused, naming the contract and the holding claim

#### Scenario: An inactive accepted claim does not block a competitor

- **WHEN** the holder of a contract is accepted but has never activated
- **THEN** a competing claim for that contract is not refused against it

#### Scenario: An active claim is not refused against its own catalog

- **WHEN** an active claim is re-judged against a platform whose registry now carries its own catalog
- **THEN** it is not refused for providing its own contracts

#### Scenario: Another provider still refuses a claim whose catalog is in the registry

- **WHEN** a different catalog provides the same contract as an active claim whose own catalog is in the registry
- **THEN** the competing claim is still refused

#### Scenario: A blocked claim still holds its contracts against another catalog

- **WHEN** a claim from a different catalog providing the same contract is judged while the holder's deletion is blocked
- **THEN** it is refused, naming the blocked claim
