## Purpose

Define the claim-to-verdict path: what acceptance fetches, what it compares it against, which claims it refuses, and what each refusal names. A claim carries no author-trusted data (enhancement 0015 D11), so acceptance re-derives every fact it judges rather than reading it off the CR.

This capability decides one transition — whether a claim is accepted. Activation, the finalizer, the shrink refusal and regeneration on the accepted set are separate capabilities.

## ADDED Requirements

### Requirement: Every claim receives a verdict

The operator SHALL reconcile every `TransformerRegistration`, recording the outcome on its status as `conditions`, `accepted` and `observedGeneration`. A refusal SHALL name what failed and the value that failed it, so the claimant can act on it without reading operator logs. A claim SHALL NOT be partially honoured: acceptance is whole or it is a refusal.

#### Scenario: An accepted claim reports acceptance

- **WHEN** a claim passes every check
- **THEN** `status.accepted` is true and a condition records the acceptance

#### Scenario: A refused claim names its reason

- **WHEN** a claim fails any check
- **THEN** `status.accepted` is false and a condition names the failing check and the offending value

### Requirement: The claimed artifact must be a catalog

Acceptance SHALL acquire the artifact named by `spec.catalog` at `spec.version` and SHALL refuse the claim unless that artifact is a `#Catalog` (enhancement 0015 D10). The refusal SHALL be structural — the kind gate the acquisition applies — rather than a rule this repo maintains. An artifact that cannot be resolved at all SHALL be refused distinguishably from one that resolves and is the wrong kind, since the first is a registry or coordinate problem and the second is an authoring one.

#### Scenario: A module artifact is refused

- **WHEN** `spec.catalog` names an artifact whose kind is `Module`
- **THEN** the claim is refused, naming the kind found

#### Scenario: An unresolvable coordinate is refused distinguishably

- **WHEN** `spec.catalog` at `spec.version` resolves to nothing in the registry
- **THEN** the claim is refused with a reason distinct from the wrong-kind refusal, naming the coordinate

### Requirement: The claimed provider set is re-derived and compared for exact equality

Acceptance SHALL derive the provider-fulfilled contract set from the acquired catalog and SHALL compare it to `spec.provides` for exact equality, refusing drift in either direction and naming both lists (enhancement 0015 D11). A claim listing a contract the catalog does not implement, and a claim omitting one it does, SHALL both be refused. Acceptance SHALL NOT accept a subset, because partial registration would leave the remainder reported as unfulfilled with no indication that the provider withheld it.

#### Scenario: A claim listing an unimplemented contract is refused

- **WHEN** `spec.provides` names a contract the catalog's transformers do not require with provider fulfilment
- **THEN** the claim is refused, naming both the claimed and the derived list

#### Scenario: A claim omitting an implemented contract is refused

- **WHEN** the catalog implements a provider-fulfilled contract absent from `spec.provides`
- **THEN** the claim is refused, naming both lists

#### Scenario: An exactly matching claim passes this check

- **WHEN** `spec.provides` equals the derived set
- **THEN** this check does not refuse the claim, whatever the order the two were produced in

### Requirement: The claim must come from the instance it names

Acceptance SHALL verify that the `ModuleInstance` named by `spec.providerRef` exists and that its `status.inventory` owns this claim, refusing the claim otherwise (enhancement 0015 D11's check deferred to this slice). `providerRef` is stamped by the renderer and cannot be authored, so a claim its named instance does not own did not come from that instance — a hand-applied stray, which would otherwise point a later health gate at the wrong package.

The check SHALL be grounded in the inventory rather than in the claim's labels. D11 suggests the owner labels, but measured, a rendered claim carries an instance *name* label and no namespace-bearing or uuid label, while `providerRef` carries a namespace and a name; a label-only check would therefore admit a stray placed by any instance sharing the provider's name in another namespace — the exact substitution the check exists to stop. The inventory is already this repo's ownership record by constitutional principle.

An instance whose inventory has not settled SHALL leave the claim awaiting a verdict rather than refused: a claim can reach the API server before its owner's status does, and a race is not a verdict.

#### Scenario: A claim its named instance does not own is refused

- **WHEN** the `ModuleInstance` named by `spec.providerRef` has a settled inventory that does not hold this claim
- **THEN** the claim is refused, naming both identities

#### Scenario: A claim naming an instance that does not exist is refused

- **WHEN** `spec.providerRef` names a `ModuleInstance` that is absent
- **THEN** the claim is refused as not rendered output, rather than accepted on the strength of its own `providerRef`

#### Scenario: A claim racing its provider's inventory is not refused

- **WHEN** the `ModuleInstance` named by `spec.providerRef` exists but has written no inventory yet
- **THEN** no verdict is recorded and the claim is retried

### Requirement: A second claim for one provider is refused naming the claimant

When more than one claim names the same provider catalog, acceptance SHALL accept at most one and SHALL refuse the others naming the claimant that holds it (enhancement 0015 D12). The refusal SHALL identify the competing claim by name, so an operator can see which object to remove. The decision SHALL be stable: which claim holds acceptance SHALL NOT change from one reconcile to the next while both exist.

#### Scenario: The second claim is refused naming the first

- **WHEN** two instances of one provider module each render a claim for the same catalog
- **THEN** one is accepted and the other is refused, naming the accepted claim

#### Scenario: The holder does not change under repeated reconciles

- **WHEN** both claims are reconciled repeatedly with no spec change
- **THEN** the same claim keeps acceptance

### Requirement: A build-incompatible provider is refused at acceptance

Acceptance SHALL compare the catalog's committed requirements against the platform's resolved versions, per shared OPM-namespace path, and SHALL refuse the claim when the catalog requires a version greater than the platform's within the same major, or any version in a different major (enhancement 0015 D8). The refusal SHALL happen here rather than at render, where the failure would name an unrelated module instance. The refusal message SHALL state that the comparison is conservative — a requirement records what the provider was tidied against, not what it uses — and that lowering the requirement is the author's fix.

#### Scenario: A provider requiring a newer build is refused

- **WHEN** the claimed catalog requires a shared path at a version greater than the platform's resolved version, same major
- **THEN** the claim is refused, naming the path, both versions, and the one-line fix

#### Scenario: A provider on a different major is refused unconditionally

- **WHEN** the claimed catalog requires a shared path in a different major than the platform's
- **THEN** the claim is refused without comparing versions

#### Scenario: A provider requiring an older build is accepted by this check

- **WHEN** every shared requirement is at or below the platform's resolved version within the same major
- **THEN** this check does not refuse the claim
