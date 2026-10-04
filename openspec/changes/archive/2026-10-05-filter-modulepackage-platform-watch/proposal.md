## Why

The ModulePackage controller watches the `cluster` Platform with no predicate. `mapPlatformToModulePackages` lists every ModulePackage in the cluster and enqueues each one, so every Platform write costs one fetch, load and render per package. Most of those writes change nothing a package renders against. The Platform reconciler rewrites the `Ready` message (and can move between `False` reasons) for each distinct build error, and it updates `ContractsFulfilled`. The ModuleInstance controller's Platform watch already drops these writes through `platformConsumedFieldsChanged`, which shipped with the filtered ModuleInstance watch. The ModulePackage watch was left out of that change's scope and is still unfiltered.

A ModulePackage render reads the same platform store record as a ModuleInstance render: `KernelPackageRenderer` returns `ErrPlatformNotReady` when the store holds no generated module, and otherwise renders with the record's package and its skew policy. So the fields the ModuleInstance predicate compares are the fields a package consumes too, and the same predicate applies unchanged.

The review of the filtered ModuleInstance watch also left two nits in that predicate's code:

- The predicate's doc comment lists `spec.skewPolicy` without saying what that edge costs. A skew-policy edit bumps the generation, and the store records the new policy only when the platform is regenerated. So the `spec.skewPolicy` edge can enqueue a render under the old policy before the `status.observedGeneration` write enqueues the one under the new policy (if the regeneration lands before a queued workload reads the store, the first render already uses the new policy and the second is redundant). The edge stays (the supervisor's call: the cost is one extra render per workload on a rare edit, and the owner's decision names the skew data among the consumed fields). The comment should say so.
- A comment in `test/integration/reconcile/platform_watch_filter_test.go` says the settle point is "the last instant a backoff requeue scheduled while blocked could fire". That is false. Under `ComputeBackoff`, a second or third `NotReady` render requeues at +10s or +20s. The spec is stable for a different reason: controller-runtime's priority queue (on by default in the v0.24 line this repo pins) merges an immediate add into a pending delayed item for the same key, so the watch-driven add consumes the backoff requeue that was scheduled while the instance was blocked.

## What Changes

- **The ModulePackage controller's Platform watch takes the shared predicate.** `SetupWithManager` adds `builder.WithPredicates(platformConsumedFieldsChanged())` to the Platform `Watches`. A Platform update re-enqueues ModulePackages only when the `Ready` status, the pin set (`status.packageIdentity`, `status.registry`), `spec.skewPolicy`, `status.observedGeneration` or `status.operatorVersion` changes. Create, delete and generic events still pass. The mapper is not changed: it still enqueues every ModulePackage.
- **The predicate's doc comment covers both consumers.** It says that ModuleInstance and ModulePackage renders consume the same store record, and it states the cost of the `spec.skewPolicy` edge. No field is added or removed.
- **The settle comment credits the priority-queue merge.** The integration spec's comment says why waiting one `BackoffBaseDelay` past the backoff floor settles the queue. The code does not change.
- **Docs.** The ModulePackage controller's `SetupWithManager` and mapper comments, and `docs/RENDERING.md`'s skew-policy paragraph, say which Platform updates re-enqueue ModulePackages.

The `SetGenerated` channel watch (a store signal that would replace the `status.operatorVersion` edge) stays deferred. So do an indexed ModulePackage mapper and a filter on the TransformerRegistration Platform watch. No decision covers them.

## Impact

- **API types:** none. The CRDs, `dist/install.yaml`, the operator module's generated data and the resource reference do not regenerate.
- **Controllers:** `internal/controller/modulepackage_controller.go` (`SetupWithManager`, comments) and `internal/controller/moduleinstance_controller.go` (the predicate's doc comment only).
- **Reconcile phases:** none. The loop is not changed. The change only reduces how often the ModulePackage loop runs because of a Platform event.
- **Tests:** a manager-driven integration spec that proves the ModulePackage Platform watch drops a no-op status write and passes a consumed-field change. The existing predicate unit table already covers each field, because both controllers share the predicate.
- **Downstream consumers:** none. The cli does not read this behaviour.
- **SemVer:** PATCH: fewer reconciles with the same results. Beta ships it as the next `-beta.N`.
- **Release:** `fix`.
- **Complexity (Principle VII):** one builder option on an existing watch. No new code path.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `modulepackage-kernel-rendering`: the "Re-enqueue ModulePackages when the platform becomes ready" requirement no longer re-enqueues on every Platform change. It re-enqueues all ModulePackages only when a field a package render consumes changes. Its recovery scenario is kept, and scenarios are added for a dropped message-only write, a pin-set change and an operator upgrade into an already-Ready Platform.
