## Why

The ModuleInstance controller watches the `cluster` Platform with no predicate, and its map function lists every ModuleInstance in the cluster and enqueues each one. Every Platform write therefore costs one full acquire, synthesis and render per instance: the reconcile renders before it decides anything is a no-op. Most of those writes change nothing an instance consumes. The Platform reconciler rewrites the `Ready` message (and can move between `False` reasons) for each distinct build error, and updates `ContractsFulfilled`; neither alters what a ModuleInstance renders against. CLI-owned and suspended instances are enqueued too, although neither renders.

The watch is unfiltered for a reason, and that reason still holds. An instance blocked on `PlatformNotReady` must recover as soon as the platform is generated, and an instance rendered under a superseded pin set or skew policy must re-render. Both triggers arrive as Platform status updates, which do not bump the Platform's generation. So the filter has to look at the fields that matter, not only at the generation.

## What Changes

- **A predicate on the Platform watch.** The ModuleInstance controller passes a Platform update only when a field that instances consume differs between the old and the new object: the `Ready` condition's status (not its reason or message), the pin set (`status.packageIdentity` and `status.registry`), the skew policy (`spec.skewPolicy`), or `status.observedGeneration` (not `metadata.generation`: a spec edit bumps it before the platform is regenerated, so a render on that edge would run against the previous package). It also passes a `status.operatorVersion` change: after an operator upgrade that is the only status change the regenerated Platform writes, and it is the event that recovers instances which rendered into `PlatformNotReady` while the new process's in-memory store was still empty. Create and delete events still pass, as do generic events.
- **The mapper enqueues only instances that render against the Platform.** A field index on ModuleInstance records the Platform an instance resolves against: the `cluster` singleton for an operator-managed, unsuspended instance, and nothing for an instance with `spec.owner: cli` or `spec.suspend: true`. The map function lists ModuleInstances through that index instead of listing them all. A resumed instance or an ownership handoff changes the spec, so the instance's own generation-change event reconciles it; the Platform watch is not needed for that.
- **The pin-set field is `status.packageIdentity`.** This change names it as the field that identifies the pin set an instance renders against. The planned pre-render skip (operator-side render input key) uses the same field, so the two changes key on one value.
- **Docs.** The controller's `SetupWithManager` comment and `docs/RENDERING.md`'s skew-policy paragraph say which Platform changes re-enqueue workloads.

## Impact

- **API types:** none. The predicate reads existing `Platform` fields. The index is an in-memory cache index and is not a CRD field. The CRDs, `dist/install.yaml` and the resource reference do not regenerate.
- **Controllers:** `internal/controller/moduleinstance_controller.go`: `SetupWithManager` registers the field index and adds `builder.WithPredicates` to the Platform `Watches`; `mapPlatformToModuleInstances` lists with `client.MatchingFields`. The ModulePackage and TransformerRegistration Platform watches are not changed.
- **Reconcile phases:** none. The reconcile loop is not changed. The change only reduces how often the loop runs because of a Platform event.
- **Tests:** unit tests for the predicate (one case per consumed field, plus an `operatorVersion` change that passes, and message-only, `False`-reason-only and `ContractsFulfilled`-only updates that are dropped), unit tests for the index function and the mapper (with a fake client that carries the index, replacing the existing direct-client mapper spec, which a field-selector list cannot serve), and one manager-driven envtest that proves `PlatformNotReady` recovery through the watch, the dropped no-op status write and the CLI-owned instance left alone.
- **Downstream consumers:** none. The cli does not read this behavior.
- **SemVer:** PATCH. Fewer reconciles, with the same results. Beta ships it as the next `-beta.N`.
- **Release:** `perf`.
- **Complexity (Principle VII):** one predicate, one index function and a one-line change to the mapper's list call. This is justified by the cost: today each Platform status write renders the whole fleet, and that cost grows with the number of instances.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `platform-gated-rendering`: the "Re-enqueue ModuleInstances when the platform becomes ready" requirement no longer re-enqueues all instances on every Platform change. It re-enqueues only the operator-managed, unsuspended instances, and only when a field they consume changes. Its existing recovery scenario is kept, and scenarios are added for a pin-set change, a dropped message-only write, a skipped CLI-owned instance and an operator upgrade into an already-Ready Platform.
- `module-instance-ownership`: the re-acknowledgement scenario's example ("after a Platform change re-enqueue") is no longer true; it becomes "after a requeue or an informer resync".
