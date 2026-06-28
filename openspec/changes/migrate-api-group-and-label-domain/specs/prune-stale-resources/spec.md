## MODIFIED Requirements

### Requirement: Live-state UUID-based ownership guard
The `Prune` function MUST verify ownership of each candidate resource against the live cluster state before deletion, using the `module-instance.opmodel.dev/uuid` label as the primary identity signal. The guard is defense-in-depth — inventory remains the primary mechanism for deciding what to prune (Constitution Principle III) — but a final live-state check prevents stale-set computation defects from causing destruction and protects against cross-ModuleInstance ownership collisions.

`Prune` MUST accept the reconciling ModuleInstance's instance UUID as a parameter (its signature changes from `Prune(ctx, c, stale)` to `Prune(ctx, c, ownerUUID, stale)`). Callers supply the UUID from the freshly-rendered resources (apply path) or from `ModuleInstanceStatus.InstanceUUID` (deletion path).

For each entry in the stale set that passes safety exclusions (Namespace, CRD), the function MUST:

1. `Get` the live object by GVK, Namespace, Name.
2. If `Get` returns NotFound, treat as success (already-deleted) and continue. (Existing behavior, preserved.)
3. If `Get` returns any other error, append to the error collection and continue with the next entry. (Existing fail-slow behavior, preserved.)
4. If the live object's `app.kubernetes.io/managed-by` label value is not recognized by `core.IsOPMManagedBy` (i.e., the live object is not OPM-managed), skip the deletion, increment `PruneResult.Skipped`, log a structured warning, and continue.
5. If the live object carries a non-empty `module-instance.opmodel.dev/uuid` label whose value differs from the supplied `ownerUUID`, skip the deletion, increment `PruneResult.Skipped`, log a structured warning, and continue. (An empty live UUID label is tolerated for backward compatibility with resources applied before the UUID label was stamped.)
6. Otherwise, proceed with `Delete`.

#### Scenario: Skip resource missing OPM managed-by label
- **GIVEN** a stale entry for ConfigMap `team-a/example` and a live ConfigMap with no `app.kubernetes.io/managed-by` label (or a value not recognized by `core.IsOPMManagedBy`)
- **WHEN** the controller runs Prune with any `ownerUUID`
- **THEN** the ConfigMap is NOT deleted
- **AND** `PruneResult.Skipped` is incremented
- **AND** a warning is logged with kind, namespace, name, and reason `not OPM-managed`

#### Scenario: Skip resource whose instance UUID disagrees with reconciling MI
- **GIVEN** a stale entry for ConfigMap `team-a/example` and a live ConfigMap with `app.kubernetes.io/managed-by=opm-controller` and `module-instance.opmodel.dev/uuid=<UUID-A>`
- **WHEN** the controller runs Prune with `ownerUUID=<UUID-B>` (different ModuleInstance)
- **THEN** the ConfigMap is NOT deleted
- **AND** `PruneResult.Skipped` is incremented
- **AND** a warning is logged with kind, namespace, name, expected `ownerUUID`, and observed `module-instance.opmodel.dev/uuid`

#### Scenario: Delete resource whose instance UUID matches reconciling MI
- **GIVEN** a stale entry for ConfigMap `team-a/example` and a live ConfigMap with `app.kubernetes.io/managed-by=opm-controller` and `module-instance.opmodel.dev/uuid=<UUID-A>`
- **WHEN** the controller runs Prune with `ownerUUID=<UUID-A>` (same ModuleInstance)
- **THEN** the ConfigMap is deleted
- **AND** `PruneResult.Deleted` is incremented

#### Scenario: Tolerate legacy resource with empty UUID label
- **GIVEN** a stale entry for ConfigMap `team-a/legacy` and a live ConfigMap with `app.kubernetes.io/managed-by=open-platform-model` (legacy value) and no `module-instance.opmodel.dev/uuid` label (resource was applied before UUID labels were introduced)
- **WHEN** the controller runs Prune with any `ownerUUID`
- **THEN** the ConfigMap is deleted (legacy resources predate the UUID label and are trusted as OPM-owned via the managed-by label)
- **AND** `PruneResult.Deleted` is incremented

### Requirement: Prune not attempted while stalled on DeletionSAMissing
The deletion-cleanup prune pass MUST NOT execute while the instance is stalled with reason `DeletionSAMissing`. In that state, the impersonated client cannot be built, and prune with any fallback identity is explicitly disallowed.

This requirement complements the existing prune-stale-resources contract: prune executes only when (a) `spec.prune=true`, (b) the inventory has entries to remove, and (c) a valid apply/prune client has been obtained. Condition (c) now explicitly excludes the controller's own client as a fallback on the deletion path.

#### Scenario: Stalled instance does not prune
- **GIVEN** a ModuleInstance stalled with reason `DeletionSAMissing`
- **WHEN** reconcile loops fire during the stall window
- **THEN** no delete API calls are made against any resource in `status.inventory`
- **AND** the inventory remains unchanged across reconciles until recovery (SA restore or orphan-exit)

### Requirement: Orphan-exit clears inventory in final status
When the orphan-exit path runs, the reconcile that removes the finalizer MUST also clear `status.inventory` so the last-observed state of the instance does not claim ownership of resources the controller has abandoned.

#### Scenario: Inventory cleared on orphan-exit
- **GIVEN** a ModuleInstance whose orphan-exit path removes the finalizer
- **WHEN** the final status is committed
- **THEN** `status.inventory` is cleared so it no longer claims ownership of abandoned resources

## ADDED Requirements

### Requirement: Instance UUID persisted on ModuleInstanceStatus
The controller MUST persist the rendered ModuleInstance's instance UUID on `ModuleInstanceStatus.InstanceUUID` after the first successful render. The value is read from any rendered resource's `module-instance.opmodel.dev/uuid` label (all rendered resources carry the same UUID). The Status field is consumed by the deletion path to supply `ownerUUID` to `apply.Prune`; the apply/prune happy path may read directly from the freshly-rendered resources.

#### Scenario: Status.InstanceUUID populated after first successful reconcile
- **GIVEN** a freshly-created ModuleInstance that successfully renders and applies
- **WHEN** the deferred status patcher commits Status
- **THEN** `mi.Status.InstanceUUID` is set to the rendered instance UUID (a non-empty string in UUID format)

#### Scenario: Deletion path reads UUID from Status
- **GIVEN** a ModuleInstance being deleted, with `mi.Status.InstanceUUID` populated by a prior successful reconcile and `mi.Status.Inventory.Entries` non-empty
- **WHEN** the controller runs deletion cleanup (which calls `apply.Prune`)
- **THEN** `apply.Prune` is invoked with `ownerUUID = mi.Status.InstanceUUID`
- **AND** the live-state UUID guard correctly distinguishes resources owned by this MI from any others sharing GVK+ns+name

#### Scenario: Deletion of never-successfully-reconciled MI is a no-op
- **GIVEN** a ModuleInstance being deleted, with `mi.Status.InstanceUUID` empty (never successfully reconciled) and `mi.Status.Inventory.Entries` empty
- **WHEN** the controller runs deletion cleanup
- **THEN** `apply.Prune` is called with an empty stale set (nothing to prune)
- **AND** the finalizer is removed

## REMOVED Requirements

### Requirement: Release UUID persisted on ModuleReleaseStatus

**Reason**: Renamed for Release→Instance vocabulary + label-domain migration (enhancement 0002 D4/D11/D12). The status field `ReleaseUUID → InstanceUUID` and label `module-release.opmodel.dev/uuid → module-instance.opmodel.dev/uuid`.
**Migration**: See the ADDED requirement "Instance UUID persisted on ModuleInstanceStatus".
