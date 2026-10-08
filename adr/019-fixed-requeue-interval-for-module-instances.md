# ADR-019: Fixed Requeue Interval for Module Instances

## Status

Accepted

Replaces one sentence of [ADR-016](016-reconcile-freshness-and-requeue-model.md): the one that names a uniform `spec.interval` as the planned mechanism for periodic level-triggering of `ModuleInstance`. The rest of ADR-016 stands. The decision is cheap to reverse: the interval is an operator flag, and `0` turns it off.

## Context

ADR-016 decided that level-triggering is delivered through `RequeueAfter`, never through a cache resync, and left it unbuilt for `ModuleInstance`: a reconcile that ended well with a rolled-out inventory returned no requeue. The controller watches the `ModuleInstance` (generation changes only) and the `Platform`, and nothing watches the objects an instance applied. So the `Healthy` and `Drifted` conditions of an unchanged instance kept the state of its last reconcile until a spec edit, a Platform change or an operator restart, and an object deleted by hand stayed deleted. `ModulePackage` did not have the gap, because it requeues on `spec.interval`.

A requeue alone does not bring a deleted object back. A reconcile whose digests are unchanged ends as a `NoOp`, and a `NoOp` skips the apply. ADR-012 rules out automatic drift correction, because a controller that rewrites fields a mutating webhook also writes fights that webhook for ever.

Three forces shape the choice. A render is the expensive step, and the default operator renders one object at a time. The CRD is on the path to GA, so a new field is a contract that cannot be withdrawn. And the operator must not add a write per instance per interval.

The options considered:

1. Status quo, with the limit documented. No code and no load, but a failed Deployment or a deleted object goes unreported for days.
2. A fixed operator-wide interval, set by a flag.
3. A `spec.interval` field on `ModuleInstance`, as on `ModulePackage` (the plan ADR-016 named).
4. A watch on the applied objects, so a change to a child enqueues its instance.

## Decision

A `ModuleInstance` reconcile that ends well (an apply, a `NoOp` or a skipped render) and whose health judgement asks for no requeue returns `RequeueAfter` of the operator flag `--instance-reconcile-interval`, default 10 minutes, with up to 10 percent added at random. `0` disables it. Health, failure and stalled requeues keep their own intervals. Suspended instances, CLI-owned instances and the operator's own instance are not requeued. This is option 2.

A reconcile that renders, finds its digests unchanged and learns from the drift dry-run that rendered objects do not exist applies those objects, and only those. An object that exists is never rewritten, so ADR-012 holds: drift is reported, not corrected. A missing object is not drift. A `Job` that sets `ttlSecondsAfterFinished` is not restored, because the cluster removes it after it finished and creating it again would run it again.

The interval sets how stale `Healthy` can be. The drift render interval of the render skip (`--drift-render-interval`, default 30 minutes) still sets how stale `Drifted` can be and how long a deleted object waits: between two renders the periodic reconcile only reads the inventory.

Option 3 was not chosen now because it adds an API field before the need for a per-instance value is shown; the flag does not block it, since a later field can override the operator default. Option 4 was not chosen because it needs an informer per rendered kind, the memory and RBAC that go with them, and a filter for the operator's own writes. Option 1 was not chosen because the stale conditions were the defect.

## Consequences

**Positive:** `Healthy` of an unchanged instance is at most one interval old, `Drifted` at most one drift render interval old, and a deleted object comes back without a spec change. A change missed as a watch edge is now also recovered without a restart, which closes the gap ADR-016 recorded.

**Positive:** No API change and no new dependency. The unchanged periodic reconcile sends no write: a skipped render patches `Healthy` only when the verdict changed.

**Negative:** New load. Each healthy instance costs one uncached read per inventory object every interval, and one render, one dry-run per rendered object and one status write every drift render interval. Before this decision an unchanged instance rendered once. With one render slot the periodic renders of N instances run one after another and fit while N times the render time is below the drift render interval; past that, renders for spec changes queue behind periodic ones.

**Negative:** The interval is one value for the whole operator. A team that wants one instance checked every minute and another every hour cannot say so.

**Negative:** An object deleted on purpose comes back. `spec.suspend` is the way to stop the operator from acting on an instance. With `--drift-render-interval=0`, an object that something else deletes again after every restore is applied on every health requeue.

**Operational:** Three flags tune the load: a longer `--instance-reconcile-interval` or `--drift-render-interval`, or more `--max-concurrent-renders`. `--instance-reconcile-interval=0` returns to the earlier behaviour at once. The operator logs the interval at start.

**Security:** No new trust boundary, identity or permission. The periodic reads and the restore run through the identity that already applies the instance (the impersonated ServiceAccount, or the manager), so a tenant cannot make the operator create anything its ServiceAccount could not create before. The restore re-asserts rendered objects only, so it adds no way to change what is applied. The new load is bounded by the render slots and the interval, not by anything a tenant sets.

**Trade-off:** A fixed interval gives bounded staleness for the cost of polling. It reacts in minutes where a watch would react in seconds, and it spends reads on instances where nothing changed. That is accepted for a design with no new API, no informers and one line of scheduling.

Related: [016-reconcile-freshness-and-requeue-model.md](016-reconcile-freshness-and-requeue-model.md), [012-drift-detection-only.md](012-drift-detection-only.md), [005-failure-classification-and-retry-model.md](005-failure-classification-and-retry-model.md), [../docs/RENDERING.md](../docs/RENDERING.md)
