# Design: filter-platform-watch

## Context

See `proposal.md` § Why. Current state, read 2026-10-03 (opm-operator main at `9835474`):

- `internal/controller/moduleinstance_controller.go` `SetupWithManager` puts `GenerationChangedPredicate` on `For(&ModuleInstance{})` only, and registers `Watches(&Platform{}, EnqueueRequestsFromMapFunc(r.mapPlatformToModuleInstances))` with no predicate. Its doc comment says this is deliberate: the Platform reconciler signals through a status update, and a status update does not bump generation.
- `mapPlatformToModuleInstances` calls `r.List` over every ModuleInstance and enqueues each one.
- `internal/reconcile/moduleinstance.go` returns before rendering for `spec.owner == cli` (`handleCLIOwned`) and for `spec.suspend` (`handleSuspend`). Every other path renders before it reaches the no-op decision.
- `internal/controller/platform_controller.go` calls `Store.SetGenerated` before it patches the status that carries the new `Ready` condition and `packageIdentity` (`SetGenerated` at :291, `patchStatus` at :311). So by the time an instance sees the status event, the store already holds the package the event announces.
- `internal/platform/identity.go` `PackageIdentity` is a function of the CR generation and the sorted active-claim coordinates. Its string form is `status.packageIdentity`, and the renderer copies the same string into `RenderResult.PlatformIdentity`.
- `cmd/main.go` builds the reconciler with `Client: mgr.GetClient()`, which is the cached client, so a field index registered on the manager answers `r.List`.
- `internal/controller/platform_controller.go` `claimContributionPredicate` (with its tests in `platform_claims_test.go`) is the house pattern for a `predicate.Funcs` that compares old and new objects.

## Goals / Non-Goals

**Goals**

- A Platform event re-renders instances only when something those instances consume has changed.
- Only instances that would render are enqueued.
- `PlatformNotReady` recovery, re-render under a new pin set and re-render under a new skew policy keep working.
- One field name, `status.packageIdentity`, identifies the pin set for this change and for the planned pre-render skip.

**Non-Goals**

- The ModulePackage and TransformerRegistration Platform watches. They have the same shape, but they are outside the decision this change implements.
- A pre-render skip on the reconcile path, which is a separate change. This change only names the field that skip will share.
- Any reconcile-loop or status change.

## Decisions

### The predicate compares the consumed fields

```go
// platformConsumedFieldsChanged passes a Platform update only when a field a
// ModuleInstance render consumes differs: the Ready condition's status, the
// pin set (status.packageIdentity, status.registry), the skew policy, or
// status.observedGeneration. It also passes an operatorVersion change: that write is the
// only status change an operator upgrade makes, and it is the event that
// recovers instances that rendered into PlatformNotReady before the new
// process regenerated the platform. A Ready message or reason rewrite and a
// ContractsFulfilled update change nothing an instance renders against, so
// they are dropped here instead of costing a render per instance.
func platformConsumedFieldsChanged() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			old, okOld := e.ObjectOld.(*releasesv1alpha1.Platform)
			cur, okNew := e.ObjectNew.(*releasesv1alpha1.Platform)
			if !okOld || !okNew {
				return true
			}
			return old.Status.ObservedGeneration != cur.Status.ObservedGeneration ||
				old.Status.PackageIdentity != cur.Status.PackageIdentity ||
				!equality.Semantic.DeepEqual(old.Status.Registry, cur.Status.Registry) ||
				skewPolicyOf(old) != skewPolicyOf(cur) ||
				readyStatus(old) != readyStatus(cur) ||
				old.Status.OperatorVersion != cur.Status.OperatorVersion
		},
	}
}

// readyStatus is the Ready condition's status; absent reads as "".
```

`predicate.Funcs` passes create, delete and generic events when their funcs are nil. That keeps today's behavior for those events:

- A create at operator start, when the informer replays the singleton, enqueues the instances.
- A delete also enqueues them. This is best-effort and is today's behavior: `Store.Clear` runs in the Platform reconciler's goroutine, so an instance can render against the old store before the clear lands.

A type assertion that fails passes the event. Failing open costs one render per instance; failing closed could leave an instance blocked.

**Ready status only, not reason or message.** `failReconcile` writes a new message for each distinct build error, and a failure can move between `False` reasons (`BuildFailed`, `GenerateFailed`, `ContractCollisions`). A refusal keeps the last good package in the store, so neither change has any effect on what an instance renders. The only `True` reason is `Generated`, so the status alone covers the edges that matter: absent or `False` to `True` (recovery), and `True` to `False`. The `True` to `False` edge is passed although it is wasted: every enqueue is a full acquire, synthesis and render (the reconcile renders before any no-op check), and a refusal keeps the last good package, so that render reproduces the last one. It is kept because it is rare and because the owner's decision names the `Ready` condition as a trigger.

**`operatorVersion`, to keep `PlatformNotReady` recovery after an upgrade.** The platform store is in memory and starts empty. After an operator restart every instance renders before the Platform reconciler repopulates the store, and reports `PlatformNotReady`. The status patch that follows the regeneration carries the same `packageIdentity`, the same `Ready=True/Generated` with the same message, the same `observedGeneration` and the same registry. On an upgrade the one field that changes is `operatorVersion`, and today that write is the event that recovers the fleet promptly. Dropping it would leave recovery after every upgrade to the transient backoff (5s doubling, capped at 5 minutes), with instances reporting `Ready=False` for longer. Passing it costs one fleet render per operator upgrade, which is the recovery itself. A restart under the same version writes an identical status, so no update event fires; that gap is today's behavior, and the transient backoff covers it. A `Store.SetGenerated` signal delivered through a `source.Channel` watch would close it, and is left as a follow-up.

**`observedGeneration`, not `metadata.generation`.** `spec.skewPolicy` and `spec.registry` edits bump `metadata.generation`. The new package then arrives as a later status update that moves `observedGeneration` and `packageIdentity`: `Store.SetGenerated` runs before that status patch, so the event finds the new package in the store. A render on the `metadata.generation` edge would run against the previous package and be repeated by the status write, so only `observedGeneration` is compared. The supervisor's triage of the owner's word "generation" reads it this way (status.observedGeneration, plus `packageIdentity`). `spec.skewPolicy` is compared because the owner named skew; a skew edit is a spec edit too, so its edge also arrives before the regenerated package and costs one early render, and the `observedGeneration` write that follows renders under the new policy.

**`status.registry` beside `packageIdentity`.** `packageIdentity` changes whenever the resolved registry changes, so comparing the registry as well is redundant today. It is kept so that a regression in how the identity is computed cannot silently stop instances from re-rendering under a new pin.

### The index keys instances by the Platform they resolve against

```go
const moduleInstancePlatformIndex = ".platform"

// moduleInstancePlatform is the Platform an instance renders against: the
// cluster singleton for an operator-managed, unsuspended instance; none for
// a CLI-owned or suspended one, which return before rendering.
func moduleInstancePlatform(obj client.Object) []string {
	mi, ok := obj.(*releasesv1alpha1.ModuleInstance)
	if !ok || mi.Spec.Owner == releasesv1alpha1.OwnerCLI || mi.Spec.Suspend {
		return nil
	}
	return []string{platformSingletonName}
}
```

`SetupWithManager` registers the index with `mgr.GetFieldIndexer().IndexField(ctx, &ModuleInstance{}, moduleInstancePlatformIndex, moduleInstancePlatform)` before it builds the controller. The mapper then lists `client.MatchingFields{moduleInstancePlatformIndex: obj.GetName()}`.

Today every Platform is named `cluster`, so the index value is constant. Keying it by the Platform's name anyway keeps the mapper honest: it enqueues exactly the instances that resolve against the object that changed. A non-singleton Platform, which the CRD refuses anyway, therefore maps to nothing.

An instance that is being deleted stays indexed. Its deletion path does not render, but it is rare, and filtering it would add a third exclusion that nothing asks for.

### Tests sit at the lightest tier that proves each claim

- **Predicate:** a table-driven unit test in `internal/controller` (beside the `claimContributionPredicate` specs). One update per consumed field passes, and so does an `operatorVersion` change. A message-only `Ready` change, a `False` reason change, a `ContractsFulfilled` change, a `Ready` `lastTransitionTime` change and an identical object are each dropped. Create, delete and generic events pass.
- **Index and mapper:** `moduleInstancePlatform` returns the singleton for an operator-owned instance, an empty-owner instance and an explicit `operator` instance, and returns nothing for CLI-owned and suspended instances. The mapper runs over a `fake.NewClientBuilder().WithIndex(...)` client holding one instance of each kind and enqueues only the operator-managed, unsuspended ones. `k8sClient` in the envtest suite is a direct client and cannot serve a custom field index, so the mapper's unit test uses the fake client.
- **Manager-driven envtest** in `test/integration/reconcile` (the `concurrent_render_test.go` pattern, with no registry). A real manager runs only the ModuleInstance controller with a counting stub renderer. The stub returns `render.ErrPlatformNotReady` until it is told the platform is ready.
  1. Create an operator-managed instance and a CLI-owned instance, and wait for the managed one to report `PlatformNotReady` and for the CLI-owned one to report `ManagedExternally`.
  2. Create the `cluster` Platform with `Ready=False`, wait for the render the create and status events cause, and wait until the render count stays still, so no earlier event is still queued. Then mark the stub ready and write `Ready=True/Generated` with a `packageIdentity`. The managed instance must render successfully exactly once, before the stub's first `NotReady` call plus `BackoffBaseDelay` (5s). Every backoff requeue is scheduled by a `NotReady` render at least `BackoffBaseDelay` after it, and every `NotReady` render happens at or after the first, so a render before that instant can only come from the Platform watch. The last `NotReady` call would not do: a requeue scheduled from the first call fires before the last call plus 5s.
  3. Once the managed instance is `Ready`, the success path does not requeue it. Wait (by polling the clock, with no sleep) past the instant a requeue scheduled while blocked could fire, then until the render count stays still. Change only the Platform's `Ready` message and the `ContractsFulfilled` condition, and use `Consistently` to check that the render count does not move. Then change `packageIdentity`, use `Eventually` to check that the count reaches at least one more, and then `Consistently` to check that it stays there.
  4. The upgrade edge: mark the stub not ready and change `packageIdentity`, wait for the `NotReady` render, then mark the stub ready and write a status whose only change is `operatorVersion`. The instance must render successfully before that `NotReady` render plus `BackoffBaseDelay`.

  The reconciler's `Client` is a counting `client.Client` that embeds `mgr.GetClient()`, so the field index still answers its `List`, and that counts `Get` calls per key. The CLI-owned instance's `Get` count must not move across steps 2 to 4. Cleanup runs through `DeferCleanup`. Its `ManagedExternally` condition cannot show an enqueue, because re-acknowledging a CLI-owned instance is specified to change nothing.

  The order matters. While an instance is in `PlatformNotReady`, its own transient backoff re-renders it every few seconds, which would make a `Consistently` check flaky. The dropped-write check therefore runs only after recovery.

  No Platform reconciler runs, so the test owns every status write and needs no registry.

- **The existing direct-client mapper spec goes.** `internal/controller/moduleinstance_platform_gate_test.go` calls the mapper with the envtest suite's direct `k8sClient` and expects every instance back. The API server rejects a custom field selector on a CRD, so with the index that spec would get nothing. The fake-client mapper spec replaces it.

## Research & Decisions

### Which fields count as consumed

**Context**: The owner's decision lists the `Ready` condition, the pin set and skew data, and the generation. The fields had to be mapped to concrete Platform fields.
**Explored**: `api/v1alpha1/platform_types.go` and the Platform reconciler's status writes (`patchStatus` call sites, `failReconcile`). Background research for this task is in `claude-stuff/kernel-plan-beta1/research.json`, task `j1`.
**Decision**: `Ready` status; `status.packageIdentity` and `status.registry`; `spec.skewPolicy`; `status.observedGeneration` (not `metadata.generation`, whose edge renders against the previous package); and `status.operatorVersion`, which is not consumed but carries the upgrade recovery.
**Rationale**: Each of these either moves what the store holds or moves the policy the renderer applies. `operatorVersion` is not an input to a render, but it is the only status change an operator upgrade makes, and it is what recovers instances that rendered into `PlatformNotReady` before the new process regenerated the platform (see § The predicate compares the consumed fields). The `Ready` reason and message, and `ContractsFulfilled`, are reports about the platform, not inputs to a render.

### Which field names the pin set for the pre-render skip

**Context**: The planned pre-render skip keys a render on its inputs, including the platform's pin set. That key and this predicate have to agree, or the skip would drop a re-render that this filter deliberately lets through.
**Explored**: `internal/platform/identity.go`, and `internal/render/kernel_module_renderer.go` (`RenderResult.PlatformIdentity`).
**Decision**: `status.packageIdentity`. On the render side the same value is `PackageIdentity.String()`, which `RenderResult.PlatformIdentity` already carries.
**Rationale**: It is already the documented authoritative answer to "what a render builds against", it changes whenever the pin set changes, and it needs no new field.

## Risks / Trade-offs

- **A future consumed field missing from the predicate** would leave instances rendering against a stale input until something else enqueues them. Mitigation: the predicate's doc comment lists the fields, and an instance blocked on `PlatformNotReady` is still retried by the transient backoff (`BackoffBaseDelay` doubling to `BackoffMaxDelay`, 5 minutes).
- **A textual conflict with `bound-render-memory`.** That sibling change also rewrites the `SetupWithManager` doc comment and adds a field to `ModuleInstanceReconciler`. Whichever of the two merges second rebases the doc comment and keeps both paragraphs. The new envtest leaves the render slots nil, which that change treats as unbounded.
- **An index function that disagrees with the reconcile's skip conditions** would either enqueue an instance that does not render (harmless) or skip one that does. The index mirrors the two early returns exactly, and its unit test pins them.
