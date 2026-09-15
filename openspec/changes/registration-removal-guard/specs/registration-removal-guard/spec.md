## Purpose

Define what stops a provider abandoning its dependents: when a `TransformerRegistration` may not be deleted, when an update to its `provides` may not be applied, and what each refusal tells the operator. Enhancement 0015 D3 and D16 treat deletion and shrinking as the same act through two doors, so both are governed here.

The mechanism by which dependents are counted is deliberately not specified: D16 leaves the refusal site and mechanics implementation-grade, and this capability states the guarantee rather than the machinery.

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

### Requirement: Shrinking provides with dependents is refused before it takes effect

The operator SHALL refuse an update that removes a contract from `spec.provides` while instances still demand that contract, and the refusal SHALL take effect while the previously accepted claim is still effective. A refusal that arrives after the new spec has replaced the accepted claim SHALL NOT be considered to satisfy this requirement: by then the old claim is gone and dependents are broken regardless of the reason reported.

The refusal SHALL name the dropped contracts and the dependent count, matching the blocked delete's shape, since both are the same act through different doors.

#### Scenario: A shrinking upgrade is refused

- **WHEN** a provider upgrade re-renders its claim with a contract dropped from `provides`, and instances still demand that contract
- **THEN** the change does not take effect, and the refusal names the dropped contracts and the dependent count

#### Scenario: The previously accepted claim survives the refusal

- **WHEN** such an update is refused
- **THEN** the claim that was accepted before the update is still the effective one, and dependents continue to render

#### Scenario: A non-shrinking upgrade passes untouched

- **WHEN** a provider upgrade re-renders its claim with the same contract set at a new catalog version
- **THEN** this requirement does not refuse it

#### Scenario: Dropping a contract nobody demands is allowed

- **WHEN** an update removes a contract from `provides` that no instance demands
- **THEN** it is accepted

### Requirement: The dependent count is derived, never authored

The dependent count SHALL be derived from what instances actually demand, and SHALL NOT be read from any field a module author can write. A refusal SHALL be reproducible: the same cluster state SHALL yield the same count and the same verdict.

#### Scenario: The count reflects actual demand

- **WHEN** an instance stops demanding a contract and the claim is re-evaluated
- **THEN** the count falls accordingly, without an operator editing anything on the claim

#### Scenario: The verdict is stable

- **WHEN** a blocked deletion is re-evaluated with no change to instances
- **THEN** the same count and the same refusal are reported
