## MODIFIED Requirements

### Requirement: Claim status separates acceptance from activation

`TransformerRegistrationStatus` SHALL carry `conditions`, `accepted` and `active`, so that a stored claim can report the three states enhancement 0015 D3 defines: not yet judged, accepted but inactive, and active. A controller SHALL watch the kind and record a verdict on every claim, setting `conditions`, `accepted` and `observedGeneration`. `active` SHALL remain false: nothing activates an accepted claim yet, so acceptance changes what a claim *reports*, not what it *does*.

#### Scenario: A stored claim receives a verdict

- **WHEN** a valid `TransformerRegistration` is applied and the manager is running
- **THEN** a reconcile is triggered for it and its status reports whether it was accepted
- **AND** `observedGeneration` matches the claim's generation

#### Scenario: A stored claim is inert

- **WHEN** a `TransformerRegistration` is applied and reconciled to any verdict, accepted or refused
- **THEN** `status.active` is false
- **AND** nothing in the cluster renders differently as a result: no workload, no `Platform` field and no generated platform module reflects the claim
