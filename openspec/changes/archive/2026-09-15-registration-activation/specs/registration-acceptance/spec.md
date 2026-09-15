## ADDED Requirements

### Requirement: A contract has one provider

Acceptance SHALL refuse a claim whose `provides` names a contract that an enabled `Platform.spec.registry` subscription or another **active** claim already provides, naming the holder and the contract (enhancement 0015 D2, keeping 0010 D37's exactly-one-provider rule). This refusal is distinct from the duplicate-claim refusal: that one is about two claims naming the same catalog, this one is about two providers of the same contract, which can arrive from different catalogs entirely.

An **inactive** accepted claim SHALL NOT hold a contract against a competitor, because a claim that has never served has no dependents to protect.

#### Scenario: A claim is refused against an enabled subscription

- **WHEN** a claim names a contract an enabled subscription's catalog already provides
- **THEN** the claim is refused, naming the contract and the subscribed catalog

#### Scenario: A claim is refused against an active claim

- **WHEN** a claim names a contract another active claim already provides
- **THEN** the claim is refused, naming the contract and the holding claim

#### Scenario: An inactive accepted claim does not block a competitor

- **WHEN** a claim names a contract an accepted but inactive claim also provides
- **THEN** this check does not refuse it on that basis
