## Context

See `proposal.md` for the two stuck deletes. The code facts that shape the fix:

- `handleNotReconciled` runs before finalizer registration and before the deletion branch. For `spec.owner == cli` it calls `handleCLIOwned`, which returns nil for a deleting object. `handleOwnInstance`, its sibling, already releases a leftover finalizer without pruning.
- The primary watch is `For(ModuleInstance, GenerationChangedPredicate)`. A metadata-only write (an annotation) does not move `metadata.generation`. The second watch is the Platform. No ServiceAccount is watched; the reconcile reads the ServiceAccount through the uncached `APIReader` when it builds the impersonated client.
- A stalled delete returns `RequeueAfter: StalledRecheckInterval` (30 minutes).
- The manager ClusterRole holds `get` and `impersonate` on `serviceaccounts`.

## Goals / Non-Goals

**Goals:**

- A deleting CLI-owned instance never waits on the operator's finalizer.
- The two documented recovery actions of a `DeletionSAMissing` stall take effect within seconds on a `ModuleInstance`.

**Non-Goals:**

- `ModulePackage`. Its controller has the same generation-only watch and the same stall; it is another controller and another change.
- A trigger for restored RBAC (a Role or binding). That needs watches on four RBAC kinds and a mapping from subject to instance.
- Any change to the stalled recheck, to the periodic reconcile of ADR-019, or to what the deletion path does once it runs.
- Removing the finalizer from a live CLI-owned instance. The main spec states that a CLI-owned own instance keeps it, and a later handover to the operator would add it again.

## Decisions

### Release in the owner-skip gate, on deletion only

`handleCLIOwned` MUST, for a deleting instance that carries `opmodel.dev/cleanup`, remove the finalizer with the existing `removeFinalizer` helper and return. A NotFound from that patch is success. It MUST NOT build a client, prune or patch status.

```go
if !mi.DeletionTimestamp.IsZero() {
    params.Warnings.Forget(keyOf(mi))
    if !controllerutil.ContainsFinalizer(mi, FinalizerName) {
        return nil
    }
    // release, never prune: the CLI owns the objects
    return ignoreNotFound(removeFinalizer(ctx, params.Client, mi))
}
```

Alternative: also strip the finalizer from a live CLI-owned instance, as `handleOwnInstance` does. Rejected: it changes a behaviour the main spec pins ("A CLI-owned own instance is left alone") and it is not needed to unblock the delete.

Alternative: let the deletion fall through to `handleDeletion`. Rejected: that path prunes the inventory, which belongs to the CLI.

The deletion event reaches the controller without a new watch: a graceful delete of an object with a finalizer sets `deletionTimestamp` and moves `metadata.generation`, which is how the operator-owned delete is seen today. An instance already stuck when the new operator starts is reconciled from the initial list.

### An OR predicate on the primary watch for the orphan annotation

The `For()` predicate becomes `predicate.Or(GenerationChangedPredicate{}, orphanAnnotationSet())`. `orphanAnnotationSet` passes an update only when the new object has a `deletionTimestamp`, its `opm.dev/force-delete-orphan` annotation is `"true"`, and the old object's was not.

Alternative: `predicate.AnnotationChangedPredicate`. Rejected: every annotation write (a `kubectl apply`, a Flux reconcile request) would reconcile and possibly render a healthy instance.

Alternative: a shorter requeue for the stall. Rejected: it polls, it still has a wait, and the brief excludes requeue changes.

The annotation set before the delete needs no trigger: the delete event reconciles the instance and the deletion path reads the annotation.

### A metadata-only ServiceAccount watch, create events only

```go
Watches(&corev1.ServiceAccount{},
    handler.EnqueueRequestsFromMapFunc(r.mapServiceAccountToModuleInstances),
    builder.WithPredicates(serviceAccountCreated()),
    builder.OnlyMetadata)
```

- Metadata only: the mapping needs the name and the namespace. The cache then holds `PartialObjectMetadata`, not the whole object, for every ServiceAccount of the cluster.
- Create only: the "return" of a ServiceAccount is a create. Updates and deletes change nothing an instance waits for.
- The map function lists the instances of the ServiceAccount's namespace from the cache and keeps those whose effective ServiceAccount has that name. The precedence (`spec.serviceAccountName`, else the flag default) comes from one function in `internal/reconcile`, exported for this use, so the watch and the reconcile cannot disagree.
- It skips CLI-owned instances and suspended instances that are not being deleted: a reconcile of either only rewrites an acknowledgement and emits an event.
- It does not restrict itself to deleting instances. The apply path stalls on the same missing ServiceAccount with the same 30 minute recheck; filtering those out would be code whose only effect is to keep that wait.

The reconcile that follows reads the ServiceAccount with the uncached reader, so it cannot run ahead of the cache.

The trigger is the ServiceAccount alone. When a user creates the ServiceAccount before the RBAC that lets it delete the inventory, the woken prune is forbidden and the delete stalls again, with `ImpersonationFailed`, until the stalled recheck. The order "RBAC first, ServiceAccount last" recovers at once; the docs name it. A retry after a forbidden prune is requeue logic and is not part of this change.

At start every existing ServiceAccount arrives as a create. Each costs one cached list of its namespace; the instances it enqueues are in the queue already from their own initial list, and the workqueue deduplicates.

### RBAC: `list` and `watch` on `serviceaccounts`

An informer needs `list` and `watch`. The marker on `ModuleInstanceReconciler` changes from `get;impersonate` to `get;impersonate;list;watch`, and `task dev:manifests` and `task operator:installer` regenerate the three shipped forms. No write verb and no new `impersonate` target.

This is a security trade-off for the owner (see Risks). The read is of ServiceAccount metadata across the cluster. The role already lets the operator impersonate any ServiceAccount, which is the stronger right.

## Reconcile phase impact

- Source, Render, Apply, Prune: none.
- Deletion: the owner-skip gate gains the finalizer release. `handleDeletion` is not changed.
- Status: none. The released CLI-owned instance gets no status write.
- Triggers: two new enqueue sources, as above.

## Research & Decisions

### What the periodic reconcile already covers

**Context**: opm-operator#261 (ADR-019) added a periodic reconcile. The brief asks what it covers here.
**Explored**: `instanceRequeue` and its callers, the returns of `handleNotReconciled`, `handleDeletion` and `handleDeletionImpersonationFailure` in `internal/reconcile/moduleinstance.go`.
**Decision**: nothing in this change can be dropped because of it.
**Rationale**: `instanceRequeue` is applied to a reconcile that ended well (apply, NoOp, skipped render). A CLI-owned instance returns `ctrl.Result{}` from the gate, and a reconcile would release nothing anyway. A stalled delete returns `StalledRecheckInterval` (30 minutes), not the 10 minute instance interval. An instance stalled on the apply path is not helped by #261 either: it also returns the stalled recheck, so the ServiceAccount watch is the only prompt trigger there too.

## Risks / Trade-offs

- [An operator of this release running under the old ClusterRole cannot sync the ServiceAccount informer. The `ModuleInstance` controller fails to start after the 2 minute cache sync timeout, `mgr.Start` returns the error and the process exits (`cmd/main.go`). The pod restarts and repeats this, so all four controllers stop, not only this one] → every shipped form of the role carries the rule in the same commit, and the operator module's RBAC is generated from `config/`. An install that pins the image and hand-maintains RBAC must add the rule. Named in the PR body.
- [The operator can now list ServiceAccount metadata cluster-wide] → no write verb, metadata only in the cache; weaker than the `impersonate` it already holds. Owner question in the swarm report.
- [A user who can edit `spec.owner` and delete the instance orphans its objects] → the same user can already set `spec.prune: false` and delete; no new power.
- [Memory for the ServiceAccount cache on a large cluster] → metadata only; a ServiceAccount's metadata is small.
