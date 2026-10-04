# Design: filter-modulepackage-platform-watch

## Context

See `proposal.md` § Why. Current state, read 2026-10-04 (opm-operator main at `8dc24b3`):

- `internal/controller/modulepackage_controller.go` `SetupWithManager` puts `GenerationChangedPredicate` on `For(&ModulePackage{})` and registers `Watches(&Platform{}, EnqueueRequestsFromMapFunc(r.mapPlatformToModulePackages))` with no predicate (:146-149). `mapPlatformToModulePackages` (:257-275) lists every ModulePackage and enqueues each one.
- `internal/controller/moduleinstance_controller.go` defines `platformConsumedFieldsChanged()` (:194-210) and registers it on the ModuleInstance controller's Platform watch (:148-152). The function is package-level and unexported, in the same package as the ModulePackage controller.
- `internal/render/kernel_package_renderer.go` reads the same platform store record as `kernel_module_renderer.go`: it returns `ErrPlatformNotReady` when the store holds no generated module (:72) and renders with the record's package and `rec.Skew` (:80).
- `internal/reconcile/modulepackage.go:410-418` maps `ErrPlatformNotReady` to `Ready=False/PlatformNotReady` and requeues after the interval. Its comment names the Platform watch as the prompt recovery path.
- `internal/controller/platform_controller.go` calls `Store.SetGenerated` before it patches the status (`SetGenerated` at :291), so the status event finds the new package already in the store. This holds for both consumers.
- controller-runtime is `v0.24.1`. Its priority queue is on by default (`pkg/controller/controller.go:253`, `ptr.Deref(options.UsePriorityQueue, true)`). In `lockedAddWithOpts`, an add without a delay for a key that is already waiting clears the waiting item's `ReadyAt` and moves it to the ready tree. So a watch-driven add consumes a pending `RequeueAfter` for the same key.
- `test/integration/reconcile/platform_watch_filter_test.go:284-290` holds the settle comment that the proposal corrects. The archived `2026-10-03-filter-platform-watch/design.md` repeats the claim. The archive is a record and is not edited.

## Goals / Non-Goals

**Goals**

- A Platform update re-enqueues ModulePackages only when a field a package render consumes has changed.
- `PlatformNotReady` recovery, re-render under a new pin set and recovery after an operator upgrade keep working for packages.
- The predicate's doc comment and the settle comment say what is true.

**Non-Goals**

- The `SetGenerated` channel watch. It stays deferred: the platform-store reshape planned for the kernel's platform overlay mode would reshape it.
- An index on ModulePackage, or a mapper that skips suspended packages. No decision covers them.
- Changing which fields the predicate compares. `spec.skewPolicy` stays.
- The TransformerRegistration Platform watch.

## Decisions

### One predicate for both controllers

The ModulePackage controller reuses `platformConsumedFieldsChanged()` and does not get a copy:

```go
b := ctrl.NewControllerManagedBy(mgr).
	For(&releasesv1alpha1.ModulePackage{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
	Watches(
		&releasesv1alpha1.Platform{},
		handler.EnqueueRequestsFromMapFunc(r.mapPlatformToModulePackages),
		builder.WithPredicates(platformConsumedFieldsChanged()),
	)
```

Both renderers read one store record: the generated package, its pin set and its skew policy. So the set of consumed fields is the same, and two copies would drift. The function stays in `moduleinstance_controller.go`. Moving it to a file of its own would be tidier, but it would conflict with the later operator change for the pre-render input key, which edits the same function and is gated on this change. Its doc comment changes from "a field a ModuleInstance render consumes" to "a field a ModuleInstance or ModulePackage render consumes".

The `status.operatorVersion` edge matters for packages for the same reason it matters for instances. After an operator upgrade, the in-memory store starts empty, packages render into `PlatformNotReady`, and the regenerated Platform's status write differs only in `operatorVersion`. Without that edge, packages would recover on the interval requeue, not on the watch. Before this change, the unfiltered watch recovered them; the predicate keeps that.

**Reconcile phase impact:** none. Only the Render phase's trigger rate changes: fewer Platform-driven enqueues.

### The skewPolicy edge stays, and its comment states its cost

`spec.skewPolicy` lives in the spec. An edit bumps `metadata.generation` in the same update event, before the platform is regenerated. The store records the policy at `SetGenerated` time (`platform_controller.go:291-296`), so a render on the `spec.skewPolicy` edge runs under the old policy. The `status.observedGeneration` write that follows re-enqueues again, and that render uses the new policy. The edge therefore costs one early render per workload, and it never renders a wrong result: the second render corrects the first.

**Decision:** keep the edge (supervisor's call in the wave-2 plan). The owner's decision lists the skew data among the consumed fields, and the cost is bounded to a rare spec edit. The doc comment gains this sentence:

```go
//   - spec.skewPolicy. An edit bumps metadata.generation before the platform
//     is regenerated, and the store records the policy only when it is, so
//     this edge renders once under the old policy and the observedGeneration
//     write that follows renders under the new one. The extra render on a
//     rare edit is accepted.
```

### The settle comment credits the priority-queue merge

Replacement text for `platform_watch_filter_test.go:284-286`:

```go
// (c) Wait one BackoffBaseDelay past the backoff floor, then for quiet,
// so the success path (which does not requeue) is settled. This is not
// the last instant a backoff requeue scheduled while blocked could fire:
// a second NotReady render requeues at +10s. The spec is stable because
// controller-runtime's priority queue (the default) merges the Platform
// watch's immediate add into the pending delayed item for the same key,
// so the recovering render consumed that requeue.
```

The code is not changed. The comment is the defect.

## Research & Decisions

### Proving the wiring at the lightest tier

**Context**: The predicate's unit table (`moduleinstance_platform_watch_test.go`) already proves each field edge. What is new is that the ModulePackage controller registers the predicate on its Platform watch. A unit test cannot inspect a builder's watches.

**Explored**: (a) a manager-driven spec that renders a real ModulePackage. It needs a Flux source with an artifact and a fetcher, and no integration spec in `test/integration/reconcile` drives a `ModulePackageReconciler` today. (b) a manager-driven spec with no ModulePackage at all, where the reconciler's `Client` counts `List` calls for `ModulePackageList`. The mapper is the only caller of that `List` when no package exists, so each count is one Platform event that passed the predicate.

**Decision**: (b). The spec runs only the `ModulePackageReconciler` in a manager, with a counting client that embeds `mgr.GetClient()`. It creates the `cluster` Platform, waits for the count to stop moving, writes a status that changes only the `Ready` message and `ContractsFulfilled`, and checks with `Consistently` that the count does not move. Then it writes a new `status.packageIdentity` and checks with `Eventually` that the count grows. It deletes the Platform with `DeferCleanup`.

**Rationale**: It proves the registration against a real API server and informer with no registry, source or render, which is what the change adds. The PlatformNotReady recovery itself is unchanged: the reconcile path is untouched, and the Ready-status edge the recovery rides is in the shared unit table. A mutation check (remove the `builder.WithPredicates` line) must turn the spec red.

## Risks / Trade-offs

- **A future field a package consumes but an instance does not.** The shared predicate would miss it for packages. Today there is none: both renderers read the same record. If one appears, the predicate's doc comment is where to split it.
- **The interval requeue stays the safety net.** If a consumed-field edge is ever missed, a blocked package recovers on its `spec.interval` requeue, as before the watch existed.
