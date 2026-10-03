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
// ModuleInstance render consumes differs: the Ready condition's status and
// reason, the pin set (status.packageIdentity, status.registry), the skew
// policy, or the generation. A message-only Ready rewrite, an operatorVersion
// stamp or a ContractsFulfilled update changes nothing an instance renders
// against, so it is dropped here instead of costing a render per instance.
func platformConsumedFieldsChanged() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			old, okOld := e.ObjectOld.(*releasesv1alpha1.Platform)
			cur, okNew := e.ObjectNew.(*releasesv1alpha1.Platform)
			if !okOld || !okNew {
				return true
			}
			return old.Generation != cur.Generation ||
				old.Status.ObservedGeneration != cur.Status.ObservedGeneration ||
				old.Status.PackageIdentity != cur.Status.PackageIdentity ||
				!equality.Semantic.DeepEqual(old.Status.Registry, cur.Status.Registry) ||
				!ptr.Equal(old.Spec.SkewPolicy, cur.Spec.SkewPolicy) ||
				readyKey(old) != readyKey(cur)
		},
	}
}

// readyKey is the Ready condition's status and reason; absent reads as "".
```

`predicate.Funcs` passes create, delete and generic events when their funcs are nil. That keeps today's behavior for those events:

- A create at operator start, when the informer replays the singleton, enqueues the instances.
- A delete, which clears the store, also enqueues them, so each instance reports `PlatformNotReady` instead of keeping a stale `Ready`.

A type assertion that fails passes the event. Failing open costs one render per instance; failing closed could leave an instance blocked.

**Ready status and reason, not message.** `failReconcile` writes a new message for each distinct build error. A refusal keeps the last good package in the store, so a message change has no effect on what an instance renders. Status and reason do change on the edges that matter: absent or `False` to `True/Generated` (recovery), and `True` to `False` (a refusal, which the instance re-reads cheaply).

**Generation and observedGeneration.** `spec.skewPolicy` and `spec.registry` edits bump `metadata.generation`. The new package then arrives as a later status update that moves `observedGeneration` and `packageIdentity`. Both values are compared, so the instance renders on the edge where the new package actually lands. The early generation edge can render once against the previous package; that render is correct, just early. It is kept because the owner's decision names generation as a trigger, and that edge is rare (one per spec edit).

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

- **Predicate:** a table-driven unit test in `internal/controller` (beside the `claimContributionPredicate` specs). One update per consumed field passes. A message-only `Ready` change, an `operatorVersion` change, a `ContractsFulfilled` change, a `Ready` `lastTransitionTime` change and an identical object are each dropped. Create, delete and generic events pass.
- **Index and mapper:** `moduleInstancePlatform` returns the singleton for an operator-owned instance, an empty-owner instance and an explicit `operator` instance, and returns nothing for CLI-owned and suspended instances. The mapper runs over a `fake.NewClientBuilder().WithIndex(...)` client holding one instance of each kind and enqueues only the operator-managed, unsuspended ones. `k8sClient` in the envtest suite is a direct client and cannot serve a custom field index, so the mapper's unit test uses the fake client.
- **Manager-driven envtest** in `test/integration/reconcile` (the `concurrent_render_test.go` pattern, with no registry). A real manager runs only the ModuleInstance controller with a counting stub renderer. The stub returns `render.ErrPlatformNotReady` until it is told the platform is ready.
  1. Create an operator-managed instance and a CLI-owned instance, and wait for the managed one to report `PlatformNotReady`.
  2. Create the `cluster` Platform with `Ready=False`. Then mark the stub ready and write `Ready=True/Generated` with a `packageIdentity`. The managed instance must leave `PlatformNotReady` within a timeout shorter than `BackoffBaseDelay` (5s), so the Platform watch is the trigger and the transient backoff cannot be. The CLI-owned instance's `Ready` condition keeps its `ManagedExternally` reason and transition time.
  3. Once the managed instance is `Ready`, the success path does not requeue it. Change only the Platform's `Ready` message and `operatorVersion`, and use `Consistently` to check that the render count does not move. Then change `packageIdentity` and use `Eventually` to check that the count goes up by one.

  The order matters. While an instance is in `PlatformNotReady`, its own transient backoff re-renders it every few seconds, which would make a `Consistently` check flaky. The dropped-write check therefore runs only after recovery.

  No Platform reconciler runs, so the test owns every status write and needs no registry.

## Research & Decisions

### Which fields count as consumed

**Context**: The owner's decision lists the `Ready` condition, the pin set and skew data, and the generation. The fields had to be mapped to concrete Platform fields.
**Explored**: `api/v1alpha1/platform_types.go` and the Platform reconciler's status writes (`patchStatus` call sites, `failReconcile`). Background research for this task is in `claude-stuff/kernel-plan-beta1/research.json`, task `j1`.
**Decision**: `Ready` status and reason; `status.packageIdentity` and `status.registry`; `spec.skewPolicy`; `metadata.generation` and `status.observedGeneration`.
**Rationale**: Each of these either moves what the store holds or moves the policy the renderer applies. Every other status field is a report about the platform, not an input to a render.

### Which field names the pin set for the pre-render skip

**Context**: The planned pre-render skip keys a render on its inputs, including the platform's pin set. That key and this predicate have to agree, or the skip would drop a re-render that this filter deliberately lets through.
**Explored**: `internal/platform/identity.go`, and `internal/render/kernel_module_renderer.go` (`RenderResult.PlatformIdentity`).
**Decision**: `status.packageIdentity`. On the render side the same value is `PackageIdentity.String()`, which `RenderResult.PlatformIdentity` already carries.
**Rationale**: It is already the documented authoritative answer to "what a render builds against", it changes whenever the pin set changes, and it needs no new field.

## Risks / Trade-offs

- **A future consumed field missing from the predicate** would leave instances rendering against a stale input until something else enqueues them. Mitigation: the predicate's doc comment lists the fields, and the stalled recheck still requeues every blocked instance.
- **An index function that disagrees with the reconcile's skip conditions** would either enqueue an instance that does not render (harmless) or skip one that does. The index mirrors the two early returns exactly, and its unit test pins them.
