## Context

The process has one library Kernel, shared with no gate: every kernel verb builds in a CUE context
of its own (library ADR-007), so correctness never needs serialisation. Memory does. A render's
working set grows with the module (61 MB plus 7.75 MB per component, `docs/RENDERING.md`), and a
module that vendors CRDs is far above the fitted line: a cert-manager-sized render peaks at about
1.75 GB of heap and about 3.5 GB RSS. The shipped limit is 4Gi.

Today the bound is per controller:

```go
// cmd/main.go:310 and :330, both reconcilers
MaxConcurrentRenders: maxConcurrentRenders,
// moduleinstance_controller.go:129, modulepackage_controller.go:176
WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxConcurrentRenders, ...})
```

so the process admits 2N renders. A rendered `pkg/core.Resource` carries its `cue.Value`
(`pkg/core/resource.go:29`, copied from `kernel.Compiled` in `compiled_adapter.go:20`), and a held
value keeps the whole build reachable. Both reconciles keep the `*render.RenderResult` alive until
they return: `moduleinstance.go` converts at `:294` and still reads `renderResult` at `:452`;
`modulepackage.go` converts at `:438` inside `applyAndPruneModulePackage` and reads the result again
at `:494`, and the caller holds it until return.

Reconcile phase impact: Render (a slot is taken around the renderer call) and Apply (the
CUE-backed resources are dropped right after conversion, before apply). Source, Prune and Status
are unchanged; no status condition, reason or event is added.

## Goals / Non-Goals

**Goals:**

- `--max-concurrent-renders` bounds renders in flight across the whole process.
- A reconcile stops pinning a build as soon as it holds the unstructured objects.
- The Go runtime collects harder as the process approaches the pod limit instead of being killed
  at it.

**Non-Goals:**

- Measuring heap and RSS before and after (a separate measurement task in the kernel plan).
- Changing what the renderers return (`RenderResult` keeps `[]*core.Resource`), caching render
  output, or switching `Compiled.Value` to bytes; those were decided separately.
- Metrics or events for slot waits, a new flag, or a change to the Platform reconciler (it builds
  one generation at a time already and takes no slot).
- Raising or lowering the 4Gi limit.

## Decisions

### 1. The slot pool: `render.Slots`

A counted semaphore over a buffered channel, in `internal/render` beside the renderer seam both
reconcile paths already import:

```go
// Slots bounds how many renders are in flight across the process.
type Slots struct{ ch chan struct{} }

func NewSlots(n int) *Slots // n >= 1; main.go already refuses n < 1

// Acquire blocks until a slot is free or ctx is done. The returned release is
// idempotent. A nil *Slots never blocks and returns a no-op release, so tests
// and callers that build params without a pool keep today's behaviour.
func (s *Slots) Acquire(ctx context.Context) (release func(), err error)
```

`cmd/main.go` builds one `render.NewSlots(maxConcurrentRenders)` and hands the same pointer to both
reconcilers (`RenderSlots` field), which pass it into `ModuleInstanceParams` and
`ModulePackageParams`. No `golang.org/x/sync` dependency: a channel is enough for a fixed count.

### 2. The slot spans the renderer call, taken before the platform lease

The reconcile takes the slot immediately before `params.Renderer.RenderModule`
(`moduleinstance.go:254`) and `params.Renderer.Render` (`renderModulePackage`, `modulepackage.go:369`)
and releases it when the call returns, success or error:

```go
release, err := params.RenderSlots.Acquire(ctx)
if err != nil {
    return ctrl.Result{}, fmt.Errorf("waiting for a render slot: %w", err)
}
renderResult, err := params.Renderer.RenderModule(ctx, ...)
release()
```

That covers exactly the section the owner named: the lease, acquisition (module fetch or package
load), synthesis and the render build, plus the adapter's conversion of `Compiled` into resources.
Taking the slot in the reconcile layer rather than inside `KernelModuleRenderer` and
`KernelPackageRenderer` has two effects: the stub renderers the reconcile tests inject are bounded
too, so the cross-kind bound is testable without a registry; and the slot is taken before the
renderer takes its platform lease, so a render waiting for a slot pins no platform generation and
renders against the newest one when it starts. A ModulePackage fetches and extracts its artifact
before it waits, so at most N extracted artifacts sit on the `/tmp` emptyDir per controller.

A wait cut short by a cancelled context (manager shutdown) returns the context error to
controller-runtime. It is not classified as a render failure: the object is not marked stalled and
no event is emitted. The deferred status patch still runs as it does for any early return.

### 3. Reconcile concurrency stays per controller

Each controller keeps `MaxConcurrentReconciles` at the flag's value. The slot pool is the render
bound; reconcile concurrency only decides how many reconciles may be waiting on it or doing
non-render work. Keeping it at N means a ModulePackage deletion, prune, suspend or CLI-owned
reconcile never queues behind N ModuleInstance renders, and the cost of a waiting reconcile is one
worker goroutine (plus, for a ModulePackage, its extracted artifact). Decoupling it into a second
flag was rejected (Principle VII: no new knob without a need), and halving it per controller cannot
express an odd total. The `MaxConcurrentRenders` field comments say the controller uses it as its
reconcile concurrency, and that renders across both kinds are bounded by the shared slots.

### 4. Drop the CUE-backed resources right after conversion

```go
resources, err := toUnstructuredSlice(renderResult.Resources)
// error path unchanged
renderResult.Resources = nil // the unstructured copies are all later phases need
```

In `moduleinstance.go` after `:294`; in `applyAndPruneModulePackage` after `:438`, through the
pointer the caller also holds, so the caller's later reads (`renderResult.InventoryEntries` at
`:494` via the return value) are unaffected. `status.RenderDigest(renderResult.Resources)` runs
before conversion in both paths (`:278`, `:410`) and needs no change. On the ModulePackage no-op
path the conversion never runs, and the result goes out of scope when the reconcile returns, which
follows the no-op check directly.

The retained build therefore lives from slot release to conversion: digests, diagnostics events
and, for a ModulePackage, the no-op check. That stretch does no I/O and no evaluation, so a render
that takes the freed slot reaches its own peak only after registry acquisition, long after the
previous build was dropped. Holding the slot until conversion would close it entirely but would
thread a release function into `applyAndPruneModulePackage`; not worth it for an I/O-free stretch.

### 5. `GOMEMLIMIT` as a literal next to the limit

The Go runtime parses `GOMEMLIMIT` as a byte count with an optional `B`, `KiB`, `MiB`, `GiB` or
`TiB` suffix, so a `resourceFieldRef` on `limits.memory` (plain bytes) would parse. It cannot scale
the value, though: a `divisor` only changes the unit, so the downward API can say 100% of the limit
and never 80%. The owner asked for about 80%, so the manifest carries a literal:

```yaml
env:
- name: GOMEMLIMIT
  value: 3276MiB # about 80% of limits.memory (4Gi); change both together
```

80% of 4096 MiB is 3276.8 MiB. The headroom covers what the soft limit does not count (goroutine
stacks beyond the heap accounting, cgo, page cache charged to the cgroup) and the transient
overshoot while a render's peak is being collected. The comment above `resources` keeps its account
of why the limit is 4Gi and adds the soft limit and the shared render bound.

## Risks / Trade-offs

- [Throughput at the default] → a cluster with both kinds now renders them serially. That is the
  documented meaning of the default; the flag raises it.
- [Something else retains the build] → `RenderResult` is the only value the reconcile keeps across
  phases, and its other fields are plain data. If a heap profile later shows the build still
  reachable after conversion, the measurement task finds it; this change does not claim a number.
- [A literal drifts from the limit] → a kustomize patch that raises the memory limit without
  touching `GOMEMLIMIT` leaves the soft limit low, which costs extra GC but never an OOMKill. The
  manifest comment and `docs/RENDERING.md` say to move both.
- [Slot starvation] → a channel semaphore is not FIFO, so under sustained load a reconcile can wait
  longer than its arrival order suggests. Each render is bounded and every slot is released on every
  return path, so no reconcile waits forever.
