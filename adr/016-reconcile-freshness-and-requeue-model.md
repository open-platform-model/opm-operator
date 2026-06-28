# ADR-016: Reconcile Freshness and Requeue Model

## Status

Accepted

## Context

Every reconcile phase — source resolution, artifact fetch, CUE evaluation, render, SSA apply, prune, status commit — operates on an in-memory copy of the primary object that was read once at the top of the loop. Some of these phases are slow: CUE evaluation and OCI registry pulls can take seconds, occasionally longer. While a loop runs, the underlying object can change: a user edits the spec, a `Platform` materializes, or another writer touches the resource.

A controller must decide how a long-running loop stays correct when the object it started from goes stale. Two broad strategies exist:

1. **Re-read mid-loop.** Re-`Get` the latest object at chosen checkpoints before mutating or committing, so each phase works against current state.

2. **Read once, requeue, rely on idempotency.** Read the object once, let the loop finish against that snapshot, and depend on idempotent application plus a guaranteed re-trigger to converge on the next pass.

The reconcile loop also has to handle the inverse of staleness — *missed triggers*. Controller-runtime delivers watch events at-most-once to a running process; an edge that fires while the controller is down is not redelivered as an event. And not every relevant change produces an edge at all: a manually edited child resource generates no watch event for the owning object today (see the child-reactivity gap tracked separately). The trigger model and the freshness model are therefore two halves of the same question: how does the controller converge when it neither sees fresh state nor receives the edge that changed it?

## Decision

The controller reads the primary object **exactly once per reconcile** and does not re-`Get` it mid-loop. Convergence under staleness is guaranteed by idempotency and re-triggering, not by re-reading. This is strategy 2.

Three mechanisms make read-once safe:

**Server-side apply for all child mutation.** Child resources are applied with SSA under a fixed field manager (ADR-004). Applying a slightly-stale desired state is harmless: SSA is last-writer-wins within owned fields and idempotent, so the next pass against fresh state self-corrects without a read-modify-write race.

**Optimistic-locked status commit.** Status is patched through the Flux `SerialPatcher`, which snapshots the object at loop start and patches against that `resourceVersion`. A concurrent spec edit makes the final patch conflict; the reconcile returns the error and is requeued, and the next pass reads fresh state. The stale loop never silently overwrites a newer object.

**Generation-bump re-enqueue.** The primary watch carries `GenerationChangedPredicate`. A spec change made during a loop bumps `.metadata.generation` and enqueues a follow-up reconcile, guaranteeing the loop re-runs against the new spec. State-based recovery (re-`Get` + recompute digests) — not the event payload — is what converges the object, so a missed *edge* is not a missed *change*: on controller restart the informer LISTs and emits synthetic create events for every object (the create predicate passes), reconciling all objects once against current state.

Level-triggering — periodic re-reconciliation to catch drift and missed edges absent any spec change — is delivered through `RequeueAfter`, not through a cache resync. The manager sets no `SyncPeriod`, and the default informer resync re-emits unchanged-generation updates that `GenerationChangedPredicate` filters out; periodic work must therefore be scheduled explicitly by returning `RequeueAfter`. `ModulePackage` does this today via `spec.interval`. Extending a uniform `spec.interval` to `ModuleInstance` and `Platform` is the planned mechanism for periodic level-triggering and is tracked as a separate enhancement.

Failure paths follow the same requeue discipline: transient failures requeue on bounded exponential backoff, and stalled failures requeue on a long safety recheck (`StalledRecheckInterval`) rather than waiting indefinitely (ADR-005).

A per-reconcile context deadline bounding any single loop is a complementary guardrail and is tracked separately; the read-once model assumes a wedged loop eventually yields and requeues rather than pinning a worker.

## Consequences

**Positive:** Phases need no defensive re-`Get` calls, and there is no read-modify-write race to reason about. Correctness rests on two well-understood primitives — SSA idempotency and optimistic-locked patches — rather than on careful checkpoint placement.

**Positive:** The model degrades gracefully under controller downtime. Because convergence is state-based, a change missed as an edge is recovered on the next reconcile, and every object is reconciled once on process start.

**Negative:** A loop applies child resources from a possibly-stale spec exactly once before the generation-bump re-enqueue corrects it. SSA makes this harmless for owned fields, but it does mean the cluster can briefly hold output rendered from a superseded spec.

**Negative:** Read-once depends on the loop terminating. Without a per-reconcile deadline, a wedged external dependency (slow registry, hung fetch) holds a worker rather than yielding to a requeue. This guardrail is not yet in place.

**Trade-off:** Choosing requeue-plus-idempotency over mid-loop re-reads keeps the loop simple and the convergence argument uniform, at the cost of accepting brief application of stale-but-idempotent desired state and a dependency on explicit `RequeueAfter` scheduling for all level-triggered behavior.

Related: [005-failure-classification-and-retry-model.md](005-failure-classification-and-retry-model.md), [009-reconcile-loop-phases.md](009-reconcile-loop-phases.md), [012-drift-detection-only.md](012-drift-detection-only.md), [004-server-side-apply-as-mutation-primitive.md](004-server-side-apply-as-mutation-primitive.md)
