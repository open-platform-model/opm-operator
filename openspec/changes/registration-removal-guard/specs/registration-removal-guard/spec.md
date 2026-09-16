## Purpose

Define what stops a provider abandoning its dependents: when a `TransformerRegistration` may not be deleted, and what the refusal tells the operator. Enhancement 0015 D3 and D16 treat deletion and shrinking as the same act through two doors; this capability governs the deletion door.

**The shrinking door (D16) is deliberately not here.** It needs a refusal site that takes effect before the new spec replaces the accepted claim, and both candidates — a validating webhook, and D16's hold-last-good fallback — cost more than one section: a webhook changes the install surface this operator ships, and hold-last-good gives a claim two answers to what it provides. It is its own change, and the requirement lands in this capability when it does. Until then a shrinking `provides` is accepted, and this capability does not claim otherwise.

The mechanism by which dependents are counted is deliberately not specified: this capability states the guarantee rather than the machinery.

## ADDED Requirements

### Requirement: A claim with dependents cannot be deleted

The operator SHALL prevent deletion of a `TransformerRegistration` while instances depend on contracts it provides, and SHALL report the refusal on the claim naming how many depend on it. The block SHALL be released once no dependents remain, so a claim never becomes permanently undeletable.

#### Scenario: Deleting a depended-on claim is blocked

- **WHEN** an active claim is deleted while instances demand a contract it provides
- **THEN** the object is not removed, and its status names the dependent count

#### Scenario: The block releases when dependents go away

- **WHEN** the last dependent stops demanding the claim's contracts
- **THEN** the pending deletion completes without further operator action

#### Scenario: A claim nothing depends on deletes immediately

- **WHEN** a claim with no dependents is deleted
- **THEN** it is removed without a block

### Requirement: The dependent count is derived, never authored

The dependent count SHALL be derived from what instances actually demand, and SHALL NOT be read from any field a module author can write. A refusal SHALL be reproducible: the same cluster state SHALL yield the same count and the same verdict.

#### Scenario: The count reflects actual demand

- **WHEN** an instance stops demanding a contract and the claim is re-evaluated
- **THEN** the count falls accordingly, without an operator editing anything on the claim

#### Scenario: The verdict is stable

- **WHEN** a blocked deletion is re-evaluated with no change to instances
- **THEN** the same count and the same refusal are reported
