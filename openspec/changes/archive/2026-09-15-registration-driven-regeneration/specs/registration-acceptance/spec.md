## MODIFIED Requirements

### Requirement: A contract has one provider

Acceptance SHALL refuse a claim whose `provides` names a contract that an enabled entry of the built platform's registry or another **active** claim already provides, naming the holder and the contract (enhancement 0015 D2, keeping 0010 D37's exactly-one-provider rule). This refusal is distinct from the duplicate-claim refusal: that one is about two claims naming the same catalog, this one is about two providers of the same contract, which can arrive from different catalogs entirely.

An **inactive** accepted claim SHALL NOT hold a contract against a competitor, because a claim that has never served has no dependents to protect.

A claim's **own** catalog SHALL NOT hold a contract against it. Once a claim is active its catalog is an enabled entry of the very platform the next reconcile judges it against (enhancement 0015 D13), so counting the claim's own entry would refuse the claim for providing what it exists to provide, deactivate it, drop its catalog from the next generated package and accept it again — an oscillation, not a verdict. Only ANOTHER provider of the contract is a conflict, and the claim's own catalog being excused SHALL NOT excuse a different catalog providing the same contract.

#### Scenario: A claim is refused against an enabled subscription

- **WHEN** a claim names a contract an enabled subscription's catalog already provides
- **THEN** the claim is refused, naming the contract and the subscribed catalog

#### Scenario: A claim is refused against an active claim

- **WHEN** a claim names a contract another active claim already provides
- **THEN** the claim is refused, naming the contract and the holding claim

#### Scenario: An inactive accepted claim does not block a competitor

- **WHEN** a claim names a contract an accepted but inactive claim also provides
- **THEN** this check does not refuse it on that basis

#### Scenario: An active claim is not refused against its own catalog

- **WHEN** an active claim is re-judged against a platform whose registry carries that claim's own catalog
- **THEN** this check does not refuse it, and the claim stays accepted and active

#### Scenario: Another provider still refuses a claim whose catalog is in the registry

- **WHEN** the built platform's registry carries both the claim's own catalog and a different catalog providing the same contract
- **THEN** the claim is refused, naming the other catalog and never the claim's own
