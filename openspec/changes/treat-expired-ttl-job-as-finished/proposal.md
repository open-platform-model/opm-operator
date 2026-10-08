## Why

A `batch/v1` Job that sets `ttlSecondsAfterFinished` is removed by the cluster after it finished. The opm catalog's Job transformer sets a TTL of 100 seconds by default. The health judgement reads the removed Job as `Missing`, so an instance that renders such a Job turns `Healthy=False` at the first periodic reconcile after the Job expired and stays so, judged every 2 minutes. The periodic reconcile of the change `requeue-healthy-instances` (same branch) makes every such instance reach that state about 10 minutes after rollout. The owner decided that the verdict is fixed on the same branch before it is pushed.

## What Changes

- A ModuleInstance reconcile that renders with unchanged digests and learns from the drift dry-run that a rendered Job with a TTL does not exist treats that Job as finished: it is not restored (as before), and it is removed from the entries of `status.inventory`, so no later health judgement reads it. `Healthy` is judged over the remaining entries.
- A reconcile that would skip its render and reads an inventory Job as absent renders instead, because only the render says whether the Job has a TTL. So an expired Job is classified at the first periodic reconcile after it expired, and a deleted Job without a TTL is created again at that reconcile, not up to one drift render interval later.
- A Job without a TTL that is missing is restored, as before.
- A Job with a TTL that was removed before it ran, and one that failed before the cluster removed it, cannot be told from one that completed: the operator records no outcome of a Job. Both are treated as finished and are not created again until the instance's digests change. A failed Job therefore stops being reported once it expired.
- An instance whose only inventory entries were expired Jobs reads `Healthy=Unknown` (`HealthUnknown`, the inventory is empty), as any empty inventory does.
- `status.inventory.digest` stays the digest of the rendered set, so the removal does not make the next reconcile apply.
- Docs and ADR-019 state the rule; the sentences that say an expired Job reads `Missing` are corrected.

No change to `api/v1alpha1`, no new flag, no RBAC change. ModulePackage is not changed.

SemVer class: PATCH after GA (a wrong condition is corrected); in beta it ships in the next `-beta.N`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `drift-detection`: the requirement "A missing object is restored" states that an expired Job with a TTL is finished and leaves the inventory, and that an absent inventory Job is classified by a render.
- `instance-health`: a new requirement that an expired Job with a TTL does not make the instance unhealthy.
- `render-input-key`: the skip conditions gain one: no inventory Job of a ModuleInstance is absent, unless the last drift detection failed.
- `inventory-bridge`: the stored inventory digest is the digest of the rendered set, which is not the digest of the listed entries after an expired Job left them.
- `reconcile-loop-assembly`: "Status always patched" and "Inventory updated only on full success" allow the removal of expired Job entries on a `NoOp` and on a restore.

## Impact

- Code: `internal/apply/drift.go` (the expired set), `internal/reconcile/moduleinstance.go` (inventory entries, skip path), `internal/reconcile/health.go` (the verdict reports an absent Job).
- Tests: `internal/controller/restore_missing_test.go`, `internal/apply/restore_test.go`, `internal/reconcile` unit tests.
- Docs: `docs/RENDERING.md`, `docs/site/diagnostics/operator-conditions.md`, `adr/019-fixed-requeue-interval-for-module-instances.md`.
- Load: one extra render per instance and per expired Job after each apply that creates the Job. No lasting cost.
- Readers of `status.inventory`: an expired Job is no longer listed. Prune and deletion act on objects that exist, so they lose nothing.
