## MODIFIED Requirements

### Requirement: The inventory digest is the library's canonical digest
`status.inventory.digest` MUST be `inventory.Digest` from `opm/k8s/inventory` over the entries of the rendered set that the last apply recorded. It equals the digest of `status.inventory.entries`, with one exception: after a ModuleInstance reconcile removed the entry of an expired Job (`drift-detection`, "A missing object is restored"), the entries are fewer and the digest still names the rendered set. A reader MUST compare the stored digest with the digest of a render, and MUST NOT recompute it from the stored entries to decide whether to apply. That digest hashes a versioned canonical encoding of each entry's field values, so it depends neither on the order of the entries nor on the JSON tags of the CRD entry type. Source: 0012:D7:R2/R3.

#### Scenario: Order does not matter
- **WHEN** the same entries are digested in a different order
- **THEN** the digest is identical

#### Scenario: Content does
- **WHEN** an entry's `Name` changes
- **THEN** the digest differs

#### Scenario: The stored digest changes once on upgrade
- **WHEN** an instance whose `status.inventory.digest` was written by an earlier operator is applied by this operator with the same entries
- **THEN** the new `status.inventory.digest` equals `inventory.Digest` of those entries and differs from the stored value

#### Scenario: The digest after an expired Job left the entries
- **WHEN** a ModuleInstance reconcile with unchanged digests removes an expired Job from `status.inventory.entries`
- **THEN** `status.inventory.digest` keeps its value, which is `inventory.Digest` of the rendered entries, the Job included
