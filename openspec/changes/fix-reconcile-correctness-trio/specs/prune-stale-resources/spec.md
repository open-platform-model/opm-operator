## ADDED Requirements

### Requirement: Live-state UUID-based ownership guard
The `Prune` function MUST verify ownership of each candidate resource against the live cluster state before deletion, using the `module-release.opmodel.dev/uuid` label as the primary identity signal. The guard is defense-in-depth — inventory remains the primary mechanism for deciding what to prune (Constitution Principle III) — but a final live-state check prevents stale-set computation defects from causing destruction and protects against cross-ModuleRelease ownership collisions.

`Prune` MUST accept the reconciling ModuleRelease's release UUID as a parameter (its signature changes from `Prune(ctx, c, stale)` to `Prune(ctx, c, ownerUUID, stale)`). Callers supply the UUID from the freshly-rendered resources (apply path) or from `ModuleReleaseStatus.ReleaseUUID` (deletion path).

For each entry in the stale set that passes safety exclusions (Namespace, CRD), the function MUST:

1. `Get` the live object by GVK, Namespace, Name.
2. If `Get` returns NotFound, treat as success (already-deleted) and continue. (Existing behavior, preserved.)
3. If `Get` returns any other error, append to the error collection and continue with the next entry. (Existing fail-slow behavior, preserved.)
4. If the live object's `app.kubernetes.io/managed-by` label value is not recognized by `core.IsOPMManagedBy` (i.e., the live object is not OPM-managed), skip the deletion, increment `PruneResult.Skipped`, log a structured warning, and continue.
5. If the live object carries a non-empty `module-release.opmodel.dev/uuid` label whose value differs from the supplied `ownerUUID`, skip the deletion, increment `PruneResult.Skipped`, log a structured warning, and continue. (An empty live UUID label is tolerated for backward compatibility with resources applied before the UUID label was stamped.)
6. Otherwise, proceed with `Delete`.

#### Scenario: Skip resource missing OPM managed-by label
- **GIVEN** a stale entry for ConfigMap `team-a/example` and a live ConfigMap with no `app.kubernetes.io/managed-by` label (or a value not recognized by `core.IsOPMManagedBy`)
- **WHEN** the controller runs Prune with any `ownerUUID`
- **THEN** the ConfigMap is NOT deleted
- **AND** `PruneResult.Skipped` is incremented
- **AND** a warning is logged with kind, namespace, name, and reason `not OPM-managed`

#### Scenario: Skip resource whose release UUID disagrees with reconciling MR
- **GIVEN** a stale entry for ConfigMap `team-a/example` and a live ConfigMap with `app.kubernetes.io/managed-by=opm-controller` and `module-release.opmodel.dev/uuid=<UUID-A>`
- **WHEN** the controller runs Prune with `ownerUUID=<UUID-B>` (different ModuleRelease)
- **THEN** the ConfigMap is NOT deleted
- **AND** `PruneResult.Skipped` is incremented
- **AND** a warning is logged with kind, namespace, name, expected `ownerUUID`, and observed `module-release.opmodel.dev/uuid`

#### Scenario: Delete resource whose release UUID matches reconciling MR
- **GIVEN** a stale entry for ConfigMap `team-a/example` and a live ConfigMap with `app.kubernetes.io/managed-by=opm-controller` and `module-release.opmodel.dev/uuid=<UUID-A>`
- **WHEN** the controller runs Prune with `ownerUUID=<UUID-A>` (same ModuleRelease)
- **THEN** the ConfigMap is deleted
- **AND** `PruneResult.Deleted` is incremented

#### Scenario: Tolerate legacy resource with empty UUID label
- **GIVEN** a stale entry for ConfigMap `team-a/legacy` and a live ConfigMap with `app.kubernetes.io/managed-by=open-platform-model` (legacy value) and no `module-release.opmodel.dev/uuid` label (resource was applied before UUID labels were introduced)
- **WHEN** the controller runs Prune with any `ownerUUID`
- **THEN** the ConfigMap is deleted (legacy resources predate the UUID label and are trusted as OPM-owned via the managed-by label)
- **AND** `PruneResult.Deleted` is incremented

### Requirement: Release UUID persisted on ModuleReleaseStatus
The controller MUST persist the rendered ModuleRelease's release UUID on `ModuleReleaseStatus.ReleaseUUID` after the first successful render. The value is read from any rendered resource's `module-release.opmodel.dev/uuid` label (all rendered resources carry the same UUID). The Status field is consumed by the deletion path to supply `ownerUUID` to `apply.Prune`; the apply/prune happy path may read directly from the freshly-rendered resources.

#### Scenario: Status.ReleaseUUID populated after first successful reconcile
- **GIVEN** a freshly-created ModuleRelease that successfully renders and applies
- **WHEN** the deferred status patcher commits Status
- **THEN** `mr.Status.ReleaseUUID` is set to the rendered release UUID (a non-empty string in UUID format)

#### Scenario: Deletion path reads UUID from Status
- **GIVEN** a ModuleRelease being deleted, with `mr.Status.ReleaseUUID` populated by a prior successful reconcile and `mr.Status.Inventory.Entries` non-empty
- **WHEN** the controller runs deletion cleanup (which calls `apply.Prune`)
- **THEN** `apply.Prune` is invoked with `ownerUUID = mr.Status.ReleaseUUID`
- **AND** the live-state UUID guard correctly distinguishes resources owned by this MR from any others sharing GVK+ns+name

#### Scenario: Deletion of never-successfully-reconciled MR is a no-op
- **GIVEN** a ModuleRelease being deleted, with `mr.Status.ReleaseUUID` empty (never successfully reconciled) and `mr.Status.Inventory.Entries` empty
- **WHEN** the controller runs deletion cleanup
- **THEN** `apply.Prune` is called with an empty stale set (nothing to prune)
- **AND** the finalizer is removed
