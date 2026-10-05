## Context

Line numbers are at `c303a46` (opm-operator#255). Re-check them before editing.

**The library package.** `opm/k8s/health` in library `v1.0.0-beta.6` (already pinned) exports:

- `Evaluate(*unstructured.Unstructured) Status` judges one object. Deployment, StatefulSet and DaemonSet are judged by rollout state, including the observed generation. A Job is judged by its `Complete` condition, a PVC by its phase, and a custom resource by its `Ready` condition, falling back to `Applied`. Passive kinds report `Applied`.
- `IsHealthy(Status)` is true for `Ready`, `Applied`, `Complete` and `Bound`.
- `Aggregate(statuses, unhealthy int) (Status, ready, total)`: `unhealthy` counts tracked objects with no status (missing or unreadable). The result is `Unknown` for a total of zero, `Ready` when all are healthy, and `NotReady` otherwise.
- `ProgressDeadlineExceeded(obj) bool` is true only for a Deployment whose controller has observed the current generation and reports `Progressing` with reason `ProgressDeadlineExceeded`.

The package is pure. The caller fetches each object uncached and after the apply it should reflect (package doc).

**ModuleInstance flow** (`internal/reconcile/moduleinstance.go`):

- Skip check at `:260`. It returns `ctrl.Result{}` before the deferred commit is armed and before `MarkReconciling`.
- Deferred commit `:308-379`. The NoOp branch calls `commitNoOpStatus` (`:113-143`). Every status patch lists its owned conditions: `:128` (NoOp), `:186` (panic), `:366` (deferred commit), `:841`, `:901`, `:931` (CLI-owned, own instance, suspend) and `:1118` (deletion).
- NoOp return at `:530-535`. The apply client is built at `:545` (`buildApplyClient`, `:1237-1255`): an impersonated `client.New` (uncached) or `params.Client` (the manager's client).
- Success at `:636-640` returns `ctrl.Result{}`.

**ModulePackage flow** (`internal/reconcile/modulepackage.go`):

- `conditionsAtStart` is cloned and `MarkReconciling` is applied at `:277-278`, before the skip at `:307`. The skip sets `renderSkipped` and returns `RequeueAfter: interval`, and the deferred commit (`:214`) returns without a patch.
- The NoOp branch of the commit is at `:225-233`. The NoOp return is at `:340-345`. The apply client is built inside `applyAndPruneModulePackage` (`:656-720`). Success at `:357-363` returns `RequeueAfter: interval`. `patchModulePackageStatus` (`:826`) and the deletion patch (`:979`) list the owned conditions.

**Triggers.** Both controllers filter `For()` by `GenerationChangedPredicate`, and neither watches the applied objects. A Deployment's status change never enqueues its instance, so health can only be followed by requeueing. A ModuleInstance has no periodic requeue after a success. A ModulePackage requeues on `spec.interval` (default 5 minutes).

**Phases touched.** Source, Render, Apply and Prune: none. Status: a new condition written after the outcome is known. Requeue: a success, a NoOp or a skip may now requeue.

## Goals / Non-Goals

**Goals:**

- A `Healthy` condition judged by `opm/k8s/health` from uncached reads taken after the apply, through the identity that applied the objects.
- Requeue until rolled out, with a floor and a ceiling. A stalled Deployment stops the fast requeue and says why.
- `Ready`, `dependsOn`, TransformerRegistration activation, digests, history and inventory unchanged.

**Non-Goals:**

- Making `Ready` depend on `Healthy`. The owner decides later.
- Continuous health monitoring. `Healthy` is judged when a reconcile runs. Once `RolledOut`, nothing requeues the instance for health.
- Watching applied objects, health events, metrics, caching impersonated clients.
- Any change to the library's evaluator. Its known limits are listed under Risks.

## Research & Decisions

### D1. A separate condition, written only after a successful outcome

**Context**: The owner decided that `Ready` keeps meaning "applied". Every reader of `Ready` stays as it is: `renderSkip.maySkip`, `checkDependsOn`, TransformerRegistration activation, and Flux kstatus.
**Explored**: (a) a status field with counts; (b) a condition type `Healthy`; (c) a `Ready` reason. (c) changes `Ready`, which the owner ruled out. (a) adds a field that a condition message already carries.
**Decision**: (b). Status `True`, `False` or `Unknown`, with the reasons `RolledOut`, `NotRolledOut`, `ProgressDeadlineExceeded` and `HealthUnknown`. It is written only by a reconcile that leaves `Ready=True` with reason `ReconciliationSucceeded`: after a successful apply and prune, on a `NoOp`, and on a skipped render. Every other path leaves it as it was, so it keeps describing the last applied render.
**Rationale**: A failed render or apply changes nothing on the cluster. Re-judging there would need a client that the failure path may not have, for example after an impersonation failure. A suspended object is deliberately left alone.

### D2. Read through the applying identity, uncached, after the apply

**Context**: The library requires uncached reads taken after the apply. The tenant's ServiceAccount, not the manager, owns the objects of an impersonated instance.
**Decision**: The reader is the impersonated client when an effective ServiceAccount is set (`client.New`, uncached). Otherwise it is `params.APIReader` (the manager's uncached reader), never the manager's cached `params.Client`. On the success path the reader is the apply client the reconcile already built, or `APIReader`. On a `NoOp` or a skip it is built the same way (`buildApplyClient` / `buildModulePackageApplyClient`). If it cannot be built, `Healthy` is `Unknown` with reason `HealthUnknown` and the error in the message.
**Rationale**: Server-side apply already needs `get` on every applied object, through that identity (the Flux resource manager reads the live object). So no new RBAC is needed, and the operator never reads, as itself, objects a tenant applied.

```go
// healthReader returns the reader the health judgement uses.
func healthReader(applyClient client.Client, impersonated bool, apiReader client.Reader) client.Reader {
	if impersonated {
		return applyClient // client.New: uncached
	}
	return apiReader // the manager's client caches typed reads
}
```

### D3. Bounded parallel reads under one deadline

**Context**: The plan asks for batched reads for large inventories. A List per kind and namespace would read fewer times. But a tenant ServiceAccount may hold `get` without `list`, and a namespace List returns objects the instance does not own.
**Explored**: (a) a List per (GVK, namespace) with a label selector, falling back to Get when forbidden; (b) one Get per entry, serially; (c) one Get per entry, at most 8 in flight, all under one 30-second deadline.
**Decision**: (c). Each entry becomes an `unstructured.Unstructured` with `apiVersion` from `Group` and `Version` and the entry's `Kind`, `Namespace` and `Name`. `NotFound` counts as missing. A no-match error (`meta.IsNoMatchError`: the kind's CustomResourceDefinition is gone, so the object cannot exist) also counts as missing; counting it unreadable would hold `HealthUnknown` for as long as the kind stays unserved. Any other error, including the deadline, counts as unreadable. Results are kept in inventory order, so the message is stable and a re-judgement that finds the same state patches nothing.
**Rationale**: (a) needs a new RBAC verb, or a fallback that runs both paths, for a saving that matters only for inventories of hundreds of objects. (c) bounds both the API server load and the worker time.

```go
func judgeHealth(ctx context.Context, r client.Reader, entries []releasesv1alpha1.InventoryEntry) healthVerdict {
	ctx, cancel := context.WithTimeout(ctx, healthReadTimeout) // 30s
	defer cancel()
	objs, missing, unreadable := readEntries(ctx, r, entries, healthReadParallelism) // 8, inventory order
	var statuses []health.Status
	var stalled, notReady []string // "Kind ns/name (Status)", first 5 kept
	for _, o := range objs {
		s := health.Evaluate(o)
		statuses = append(statuses, s)
		if health.ProgressDeadlineExceeded(o) { stalled = append(stalled, name(o)) }
		if !health.IsHealthy(s) { notReady = append(notReady, describe(o, s)) }
	}
	agg, ready, total := health.Aggregate(statuses, len(missing)+len(unreadable))
	switch {
	case len(stalled) > 0:                  // False, ProgressDeadlineExceeded
	case len(notReady)+len(missing) > 0:    // False, NotRolledOut
	case len(unreadable) > 0:               // Unknown, HealthUnknown (requeue)
	case agg == health.Unknown:             // Unknown, HealthUnknown: no objects (no requeue)
	default:                                // True, RolledOut
	}
}
```

### D4. Reasons and their order

**Decision**: The first matching row wins.

| Order | Finding | Status | Reason | Requeue |
| --- | --- | --- | --- | --- |
| 1 | a Deployment reports `ProgressDeadlineExceeded` | False | `ProgressDeadlineExceeded` | `StalledRecheckInterval` (30m) |
| 2 | an object is not healthy, or is missing | False | `NotRolledOut` | health backoff |
| 3 | an object could not be read (or the reader could not be built) | Unknown | `HealthUnknown` | health backoff |
| 4 | the inventory is empty | Unknown | `HealthUnknown` | none |
| 5 | every object read and healthy | True | `RolledOut` | none |

The message starts with "`<ready>/<total>` objects ready". For rows 1 and 2 it names up to five objects with their status (for example `Deployment apps/web (NotReady)`, `ConfigMap apps/cfg (Missing)`) and then "and N more". For row 1 the stalled Deployments come first (in inventory order), then the other not-healthy objects (in inventory order), so the reason's cause is always named. For row 2 every named object is in inventory order. Row 3 names the error of the first unreadable object in inventory order, never by completion order of the parallel reads.
**Rationale**: A known stall outranks progress, and a known not-ready object outranks an unread one, because each tells the reader more. `True` needs every object read.

### D5. Requeue: elapsed-time backoff with a floor and a ceiling

**Context**: The owner asked for a requeue until the instance rolls out. No counter of health checks exists, and adding a status field for one would be the only new field in this change.
**Decision**: `healthRequeue(v, lastAppliedAt, now)` returns `clamp((now - lastAppliedAt)/2, 5s, 2m)` for rows 2 and 3, `StalledRecheckInterval` for row 1, and 0 otherwise. A nil or future `lastAppliedAt` gives the floor. A ModuleInstance returns `RequeueAfter` with it. A ModulePackage uses `min(it, interval)` when it is non-zero, and `interval` otherwise. `nextRetryAt` is not set: nothing failed, and the field is documented as the next retry after a failure.
**Rationale**: Elapsed-time halving is stateless and survives a restart. Just after an apply it checks at 5s, 5s, 7.5s, and so on, and it settles at the 2-minute ceiling after about four minutes. A rollout that never finishes (a StatefulSet has no progress deadline) costs N reads every 2 minutes, which is cheap next to a render.

### D6. The skip patches `Healthy` only, and only when it changed

**Context**: `render-input-key` says a skipped reconcile patches nothing. For a ModuleInstance, a health requeue within the drift render interval is a skip. Without a health read on the skip, a requeue could never observe the rollout.
**Decision**: On a skip, the reconciler builds the reader (D2), judges health over `status.inventory.entries`, sets `Healthy`, and patches with `Healthy` as the only owned condition. The patch helper sends nothing when the condition is unchanged. For a ModulePackage, `pkg.Status.Conditions` is first restored from `conditionsAtStart`, so the transient `Reconciling` this attempt set is never written. A skip still takes no render slot, writes no `observedGeneration`, emits no event, and leaves `lastAttempted*`, history and `renderedAt` alone. A skipped ModuleInstance returns `RequeueAfter` from D5. A skipped ModulePackage requeues after the shorter of D5 and `spec.interval`.
**Rationale**: This is the smallest exception that lets a requeue observe a rollout. "The skip is not an attempt" still holds: the condition records an observation of the cluster, not an outcome of the reconcile.

### D7. Conditions the operator stops maintaining are removed

**Decision**: `MarkManagedExternally` and `MarkSelfManagementRefused` delete `Healthy`. Neither path judges health, and a `Healthy=True` left over from an operator-owned era would describe objects the operator no longer reconciles. `MarkSuspended` keeps it: a suspended object keeps all its last conditions, which is Flux's convention.

### D8. Owned conditions

**Decision**: `HealthyCondition` joins every `patch.WithOwnedConditions` list of both reconcilers: the seven ModuleInstance lists named in Context (D7 removes the condition through the CLI-owned and own-instance patches), `patchModulePackageStatus` and the ModulePackage deletion patch. Patches from the operator then always win on `Healthy`, the same way they win on `Ready`.

### D9. Printer column

**Decision**: `+kubebuilder:printcolumn:name="Healthy",type=string,JSONPath=".status.conditions[?(@.type=='Healthy')].status"` on both kinds, right after `Ready`. It shows at the default priority, because rollout state is what a reader of `kubectl get mi` looks for next.

## Risks / Trade-offs

- **A new custom resource with no `Ready` condition yet reads as `Applied`.** In beta.6 `evaluateCustomHealth` falls back to `Applied` (healthy) for a custom resource with no `Ready` condition. On the success path the read comes right after the apply, so a freshly created custom resource whose controller has not acted yet counts as healthy, and `Healthy` can be `True` before that controller judges it. `RolledOut` does not requeue, so nothing re-judges it until the next reconcile. The operator adds no rule of its own (`kubernetes-tier-adoption` forbids it); this is a library follow-up, filed together with the next item as one: custom-resource readiness without generation or condition evidence.
- **The custom resource `Ready` condition is not generation-checked by the library.** A custom resource whose spec changed in this apply may still carry `Ready=True` from its previous generation, so `Evaluate` reports it `Ready` at once. This is a limit of `opm/k8s/health`, not of the read order. It is listed as a library follow-up.
- **A refused TransformerRegistration claim affects its provider's `Healthy` depending on timing.** The claim is a custom resource whose `Ready` is the acceptance verdict. If the TransformerRegistration controller has judged the claim before the provider's health read, a refused claim is `NotReady`, and the provider stays `NotRolledOut` and requeues at the 2-minute ceiling for as long as the refusal holds. On a first apply the read usually comes first: the claim has no `Ready` yet, reads as `Applied` (previous item), the provider goes `RolledOut` and is not requeued for health, so it stays `RolledOut` until its next reconcile. Both outcomes are accepted for now: activation keys on the provider's `Ready`, so nothing deadlocks, and the fix belongs to the library follow-up above.
- **A failed Job is `NotRolledOut` forever**, requeued at the ceiling: the library has no predicate that tells a failed Job from a running one. This is listed as a library follow-up, like `ProgressDeadlineExceeded`.
- **Every health requeue renders when the skip cannot apply.** With `--drift-render-interval=0` the skip is disabled, and when `status.lastAppliedInputs` is nil because the key is incomplete (for example a Platform without `status.packageIdentity`, `moduleinstance.go:1348`) there is nothing to match. In both cases a ModuleInstance that has not rolled out renders on every health requeue until it does. A `NoOp` does not move `lastAppliedAt`, so that is from every 5 seconds just after the apply, settling at every 2 minutes: about seven renders, each holding a render slot, in the first minute after an apply. Before this change such an instance did not requeue after a success at all. The default (30m) with a complete key keeps the skip on. Both cases are documented in `docs/RENDERING.md`.
- **The skip now reads the cluster.** It reads N objects per skipped reconcile, and through an impersonated identity it also does one ServiceAccount read and builds a client. While a ModuleInstance has not rolled out, health requeues make the skip its common path (D6). For a ModulePackage skips come once per `spec.interval`, or sooner on a health requeue.
- **A Missing object persists across `NoOp`s.** A render whose digests are unchanged is a `NoOp`, and the `NoOp` path runs drift detection but never re-applies (`IsNoOp`, `internal/status/digests.go:76`, compares digests, not live state). An object deleted out of band therefore keeps the instance `Healthy=False` with reason `NotRolledOut` until an input changes or a non-`NoOp` apply happens. A ModuleInstance in that state requeues every 2 minutes for as long as it lasts, and renders once per drift render interval. This is accepted: the condition reports the truth, and re-applying on drift is out of scope here.

## Sections

1. Vocabulary and API: condition, reasons, helpers, printer columns, regenerated CRDs, dist and module data, and the removal in D7. Nothing judges health yet.
2. The judgement (D2 to D5) and the ModuleInstance wiring (D1, D6, D8), with unit and envtest coverage.
3. The ModulePackage wiring.
4. Docs, the e2e assertion and verification.
