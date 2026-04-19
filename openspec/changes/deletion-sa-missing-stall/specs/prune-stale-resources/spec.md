## ADDED Requirements

### Requirement: Prune not attempted while stalled on DeletionSAMissing
The deletion-cleanup prune pass MUST NOT execute while the release is stalled with reason `DeletionSAMissing`. In that state, the impersonated client cannot be built, and prune with any fallback identity is explicitly disallowed.

This requirement complements the existing prune-stale-resources contract: prune executes only when (a) `spec.prune=true`, (b) the inventory has entries to remove, and (c) a valid apply/prune client has been obtained. Condition (c) now explicitly excludes the controller's own client as a fallback on the deletion path.

#### Scenario: Stalled release does not prune
- **GIVEN** a ModuleRelease stalled with reason `DeletionSAMissing`
- **WHEN** reconcile loops fire during the stall window
- **THEN** no delete API calls are made against any resource in `status.inventory`
- **AND** the inventory remains unchanged across reconciles until recovery (SA restore or orphan-exit)

### Requirement: Orphan-exit clears inventory in final status
When the orphan-exit path runs, the reconcile that removes the finalizer MUST also clear `status.inventory` so the last-observed state of the release does not claim ownership of resources the controller has abandoned.

#### Scenario: Inventory cleared on orphan-exit
- **GIVEN** a ModuleRelease stalled with `DeletionSAMissing` and `status.inventory.entries` containing 3 items
- **WHEN** the orphan annotation is set and the next reconcile processes the orphan-exit
- **THEN** the status patch applied in that reconcile sets `status.inventory.entries` to an empty slice (or removes the Inventory struct, whichever matches existing nil semantics)
- **AND** the event message's orphaned-count reflects the pre-clear size (3)
