## Context

See proposal.md for the motivation. The facts that shape the design:

- The health judgement (`internal/reconcile/health.go`) reads the entries of `status.inventory`. An entry holds group, version, kind, namespace, name and component. It does not hold the object's spec, so the judgement cannot see a TTL.
- Two reconcile paths judge health without an apply. The `NoOp` path rendered, so it has the rendered Job and the drift dry-run's answer. The skip path (`render-input-key`) did not render: it has the inventory only.
- `TestHealthJudgesOnlyThroughTheLibrary` forbids reading a field of a fetched object in `internal/reconcile`. The Job predicate therefore stays in `internal/apply`, where `Restorable` already lives.
- The task forbids a new API field. A status field is an API field (CRD schema, and the operator module's generated data).
- envtest runs no Job controller and no TTL controller. A test removes the Job itself.

## Goals / Non-Goals

**Goals:**

- An instance whose rendered Job with a TTL finished and was removed reads `Healthy=True` when its other objects are healthy, and stays on the instance reconcile interval.
- The expired Job is not created again while the digests are unchanged.
- A missing Job without a TTL is restored.
- The knowledge survives an operator restart and needs no new API field.

**Non-Goals:**

- ModulePackage health. Its inventory and `NoOp` are not changed.
- A record of whether a Job completed.
- The restore of a deleted Namespace, the interval, the flag, RBAC, drift correction.

## Research & Decisions

### Which evidence says "finished"

**Context**: The operator must tell an expired Job from a deleted one.
**Explored**: (a) Record that a health judgement saw the Job complete. This needs a place to store it (an API field) and misses a Job that completes and expires between two judgements: the health requeue settles at 2 minutes and the catalog TTL is 100 seconds. (b) Use the rendered spec and the cluster state only.
**Decision**: (b). A Job is expired when the rendered Job sets `spec.ttlSecondsAfterFinished`, the drift dry-run reports that it does not exist, and every digest is unchanged (the reconcile is a `NoOp` or a restore).
**Rationale**: It needs no stored state and never runs a finished Job twice. What it cannot know: whether the Job completed, failed, or was deleted by hand before it ran. All three read as finished. For a failed Job this means `Healthy` turns from `False` to `True` when the cluster removes the Job (the TTL applies to a failed Job too), and nothing on the instance then says that it failed. Before this change such an instance stayed `Healthy=False`, for the wrong reason (`Missing`). Keeping it `False` needs a record of the Job's last state, which is an API field; the owner decides whether that is wanted. A Job with a TTL that someone deletes before it ran is therefore not created again until a digest changes. That is the safe direction: the other error runs a migration twice.

### Where the knowledge is kept

**Context**: The skip path has the inventory only, and it judges health on most reconciles.
**Explored**:
1. A status field or a marker on the inventory entry. Ruled out: a new API field.
2. Memory in the operator process. Lost at a restart, and a second source of truth beside status.
3. No memory: the skip path renders whenever an inventory Job is absent. Correct, but every instance with an expired Job then renders on every periodic reconcile (every 10 minutes, not every 30), for ever.
4. Remove the expired Job from `status.inventory.entries`.
**Decision**: 4. The inventory is the operator's record of the objects it manages (Constitution III). An expired Job does not exist and will not exist again under these digests, so it is not managed. After the removal no health judgement reads it, on either path.
**Rationale**: One write, in the ledger that the judgement already reads, and nothing to forget at a restart. Prune and deletion act on inventory entries that exist on the cluster; an absent Job gives them nothing to do. A later apply (changed digests) creates the Job again and writes the full rendered inventory, so the entry returns with the object.

`status.inventory.digest` MUST stay the digest of the rendered set. The no-op check compares it with the digest of the next render; a digest of the shortened list would make the next reconcile apply every object and run the Job again. `revision` moves only on an apply, as before. `count` is the number of entries.

### How the skip path learns that a Job expired

**Context**: The Job expires between two renders. Until the next render (up to one drift render interval plus one reconcile interval) the skip path would read it as `Missing`.
**Decision**: A ModuleInstance reconcile that may skip its render judges health first. When the judgement read an entry of group `batch`, kind `Job` as absent, and `status.failureCounters.drift` is zero, the reconcile does not skip: it renders. The render then removes the entry (TTL) or restores the Job (no TTL).
**Rationale**: The group and kind are read from the inventory entry, not from an object. After the render the entry is gone or the Job exists, so the reconcile renders once per expired Job, not once per interval. The drift counter is the bound: a dry-run that failed leaves the missing set unknown and the entry in place, and without the bound the instance would render on every 2-minute health requeue. With it, the instance reports `Missing` and waits for the drift render interval, as before this change.

```go
// skip path
v := judgeInstanceHealth(...)
if v.absentJob && driftFailures(mi) == 0 {
    // fall through to the render
} else {
    patch Healthy; return requeue
}

// after the drift dry-run, digests unchanged
expired := apply.Expired(missing)          // Jobs with a TTL
applyList, isNoOp, restoring := planRestore(...)  // Restorable(missing), as before
kept := withoutExpired(converted.entries, expired)
// NoOp:    status.inventory.entries = kept (digest, revision unchanged); judge kept
// restore: the new inventory holds kept, with the rendered digest
```

### Reconcile phase impact

- Source, Render: none. One more render after a Job expired.
- Drift: none. The dry-run already returns the missing set.
- Apply: none. `Restorable` already leaves the Job out.
- Prune: none.
- Status: a `NoOp` may shorten `status.inventory.entries`; a restore writes the shortened list. `Healthy` is judged over the list that is committed.

## Risks / Trade-offs

- [A reader of `status.inventory` expects every rendered object] → Documented in `docs/RENDERING.md` and the conditions page. The digest still names the rendered set.
- [`inventory.digest` is no longer the digest of `inventory.entries` after a removal] → Stated in the spec. The only readers of the digest compare it with a render (the no-op check, the registration re-judge), which is what it still is.
- [A Job with a TTL deleted by hand before it ran is not run] → Stated in docs. A spec or values change, or deleting and creating the instance, runs it.
- [A failed status patch on the `NoOp` leaves the entry] → The next reconcile sees the absent Job and renders again. Bounded by the health requeue (2 minutes).
- [An instance that renders only Jobs with a TTL has an empty inventory after they expired and reads `Healthy=Unknown`, "the inventory is empty"] → Stated in docs and pinned by a test. It asks for no health requeue. Reading it as rolled out needs the judgement to tell this empty inventory from one that never held anything.
- [A ModulePackage that applies Jobs still reads `Missing`] → Not in scope; noted in docs as before.
