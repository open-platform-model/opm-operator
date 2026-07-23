# Delta: prune-stale-resources (envtest-coverage-batch)

No behavior change. The ownership-guard requirement gains the CLI-manager-identity tolerance scenario (enhancement 0006 D40's post-handoff window), test-pinning what `core.IsOPMManagedBy` already accepts. The requirement text also corrects post-0002 naming drift: the label is `module-instance.opmodel.dev/uuid` (was `module-release.opmodel.dev/uuid`) and the identity comes from `ModuleInstanceStatus.InstanceUUID` — naming only, the code already used these names.

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

#### Scenario: Skip resource whose instance UUID disagrees with reconciling instance
- **GIVEN** a stale entry for ConfigMap `team-a/example` and a live ConfigMap with `app.kubernetes.io/managed-by=opm-controller` and `module-instance.opmodel.dev/uuid=<UUID-A>`
- **WHEN** the controller runs Prune with `ownerUUID=<UUID-B>` (different ModuleInstance)
- **THEN** the ConfigMap is NOT deleted
- **AND** `PruneResult.Skipped` is incremented
- **AND** a warning is logged with kind, namespace, name, expected `ownerUUID`, and observed `module-instance.opmodel.dev/uuid`

#### Scenario: Delete resource whose instance UUID matches reconciling instance
- **GIVEN** a stale entry for ConfigMap `team-a/example` and a live ConfigMap with `app.kubernetes.io/managed-by=opm-controller` and `module-instance.opmodel.dev/uuid=<UUID-A>`
- **WHEN** the controller runs Prune with `ownerUUID=<UUID-A>` (same ModuleInstance)
- **THEN** the ConfigMap is deleted
- **AND** `PruneResult.Deleted` is incremented

#### Scenario: Tolerate legacy resource with empty UUID label
- **GIVEN** a stale entry for ConfigMap `team-a/legacy` and a live ConfigMap with `app.kubernetes.io/managed-by=open-platform-model` (legacy value) and no `module-instance.opmodel.dev/uuid` label (resource was applied before UUID labels were introduced)
- **WHEN** the controller runs Prune with any `ownerUUID`
- **THEN** the ConfigMap is deleted (legacy resources predate the UUID label and are trusted as OPM-owned via the managed-by label)
- **AND** `PruneResult.Deleted` is incremented

#### Scenario: Delete resource still carrying the CLI manager identity

- **GIVEN** a stale entry for ConfigMap `team-a/example` and a live ConfigMap with `app.kubernetes.io/managed-by=opm-cli` and a UUID label matching the reconciling instance (the post-handoff window: applied by the CLI, removed from the module before any relabeling reconcile ran — enhancement 0006 D40)
- **WHEN** the controller runs Prune with the matching `ownerUUID`
- **THEN** the ConfigMap is deleted (all OPM manager identities are accepted by `core.IsOPMManagedBy`)
- **AND** `PruneResult.Deleted` is incremented
