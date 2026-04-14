## Context

Controller-runtime provides `record.EventRecorder` via `mgr.GetEventRecorderFor("opm-controller")`. Events appear in `kubectl describe <resource>` output and can be watched cluster-wide. Flux controllers emit events for apply, prune, and error conditions. OPM should follow the same pattern.

## Goals / Non-Goals

**Goals:**
- Wire `EventRecorder` into the reconciler struct.
- Emit events at reconcile milestones and on failures.
- Use consistent event reasons matching condition reason constants.
- Keep event messages concise (single line, actionable).

**Non-Goals:**
- Custom event aggregation or deduplication (Kubernetes handles this natively).
- Emitting events for every individual resource in an apply set (too noisy).
- Event-based alerting configuration (operator tooling concern).

## Decisions

### 1. Event emission points

| Phase | Event Type | Reason | When |
|-------|-----------|--------|------|
| 0 | Normal | `Suspended` | Entering suspend |
| 0 | Normal | `Resumed` | Exiting suspend (first reconcile after unsuspend) |
| 1 | Warning | `SourceNotReady` | Source not ready (soft-blocked) |
| 2 | Warning | `ArtifactFetchFailed` | Fetch or validation failure |
| 3 | Warning | `RenderFailed` | CUE evaluation failure |
| 4 | Normal | `NoOp` | No changes detected |
| 5 | Normal | `Applied` | Successful apply with count |
| 5 | Warning | `ApplyFailed` | Apply failure |
| 6 | Normal | `Pruned` | Successful prune with count |
| 6 | Warning | `PruneFailed` | Prune failure |
| 7 | Normal | `ReconciliationSucceeded` | Full reconcile success |

### 2. Event recorder name: "opm-controller"

The recorder is created with `mgr.GetEventRecorderFor("opm-controller")`. This appears as the `reportingController` in Event objects.

### 3. Event messages include counts where applicable

Examples:
- "Applied 12 resources (3 created, 9 unchanged)"
- "Pruned 2 stale resources"
- "Source team-a/my-source is not ready"
- "CUE evaluation failed: invalid config value"

### 4. Use Eventf for format strings

Use `recorder.Eventf(obj, corev1.EventTypeNormal, reason, format, args...)` for consistency.

## Risks / Trade-offs

- **[Trade-off] Event volume** — One event per reconcile cycle. For resources reconciling frequently (transient failures), events accumulate. Kubernetes default TTL (1 hour) and aggregation handle this.
- **[Risk] Event RBAC** — Controller needs `create` and `patch` on `events` resource. Controller-runtime's default scaffold includes this.
