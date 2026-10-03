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

Reconcile phase impact: Render (a slot is taken around the renderer call and the export of its
result for apply) and Apply (the CUE-backed resources are dropped inside that slot, before apply). Source, Prune and Status
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

// Run takes a slot, calls fn, and gives the slot back with a deferred release,
// so a panic inside fn frees it too. It returns only the wait's error; what fn
// produces comes back through fn's closure.
func (s *Slots) Run(ctx context.Context, fn func()) error
```

`cmd/main.go` binds one `render.NewSlots(maxConcurrentRenders)` to a variable and hands that pointer
to both reconcilers (`RenderSlots` field), which pass it into `ModuleInstanceParams` and
`ModulePackageParams`. No `golang.org/x/sync` dependency: a channel is enough for a fixed count.

### 2. The slot spans the renderer call and the export, taken before the platform lease

The reconcile wraps `params.Renderer.RenderModule` (`moduleinstance.go:254`) and
`params.Renderer.Render` (`renderModulePackage`, `modulepackage.go:369`), together with the export
of the result for apply (section 4), in `Slots.Run`:

```go
var (
    renderResult *render.RenderResult
    converted    *convertedRender
    err          error
)
if waitErr := params.RenderSlots.Run(ctx, func() {
    renderResult, converted, err = renderAndConvertInstance(ctx, params.Renderer, &mi)
}); waitErr != nil {
    skipCommit = true // the wait was cut short: see below
    return ctrl.Result{}, fmt.Errorf("waiting for a render slot: %w", waitErr)
}
```

The release is deferred inside `Run`, never inline after the call. controller-runtime v0.24 recovers
reconcile panics by default (`RecoverPanic` defaults to true and `cmd/main.go` does not set it), so a
panic in the renderer or the kernel is turned into a reconcile error and the worker carries on. An
inline release would never run on that path, the slot would leak, and at the default of 1 every later
render of either kind would block forever while the pod stays healthy. The platform lease beside it
is released the same way (`defer releaseLease()`, `kernel_module_renderer.go:82`).

That covers the section the owner named (the lease, acquisition by module fetch or package load,
synthesis and the render build, plus the adapter's conversion of `Compiled` into resources) and the
export after it, so the flag is a true process-wide bound on builds held in memory (section 4).
Taking the slot in the reconcile layer rather than inside `KernelModuleRenderer` and
`KernelPackageRenderer` has two effects: the stub renderers the reconcile tests inject are bounded
too, so the cross-kind bound is testable without a registry; and the slot is taken before the
renderer takes its platform lease, so a render waiting for a slot pins no platform generation and
renders against the newest one when it starts. A ModulePackage fetches and extracts its artifact
before it waits, so at most N extracted artifacts sit on the `/tmp` emptyDir per controller.

**A cancelled wait commits nothing.** A wait cut short by a cancelled context (manager shutdown)
returns the context error to controller-runtime with no status patch, no event and no metrics. Both
kinds install their deferred status commit before the render, and the zero value of `Outcome` is
`NoOp` (`outcome.go:12`), so a plain early return would take the NoOp branch: mark Ready "Reconciliation
succeeded", reset failure counters, clear `NextRetryAt` and record a successful no-op. One mechanism
serves both kinds: a `skipCommit` flag, set only on a cancelled slot wait, makes the deferred block
return before it touches status or records metrics.

- ModuleInstance: the reconcile sets `skipCommit` and returns `ctrl.Result{}, err` directly.
- ModulePackage: `renderModulePackage` returns `(*convertedRender, *phaseFail, error)`; the
  third value is non-nil only for a cancelled wait. The caller sets `skipCommit` and returns
  `ctrl.Result{}, err`, so the error is not folded into the `phaseFail` path (which always returns a
  nil error with a requeue).

Both return before any render-error classifier runs, so a context error is never read as a render or
acquisition failure.

### 3. Reconcile concurrency stays per controller

Each controller keeps `MaxConcurrentReconciles` at the flag's value. The slot pool is the render
bound; reconcile concurrency only decides how many reconciles may be waiting on it or doing
non-render work. Keeping it at N means a ModulePackage deletion, prune, suspend or CLI-owned
reconcile never queues behind N ModuleInstance renders, and the cost of a waiting reconcile is one
worker goroutine (plus, for a ModulePackage, its extracted artifact). Decoupling it into a second
flag was rejected (Principle VII: no new knob without a need), and halving it per controller cannot
express an odd total. The `MaxConcurrentRenders` field comments say the controller uses it as its
reconcile concurrency, and that renders across both kinds are bounded by the shared slots.

### 4. Export under the slot, then drop the CUE-backed resources

The rendered values pin the whole build until they are exported, and the export is not cheap: the
render digest (`status.RenderDigest`) and the unstructured conversion (`toUnstructuredSlice`) each
called `cue.Value.MarshalJSON` on every resource, two full exports of the rendered set, plus a sort
that reads paths from the CUE values. The memprobe baseline for a cert-manager-sized module (42
objects) measured the heap peak during that conversion at 1920 MiB, above the render's own 1896 MiB.
A slot released when the renderer returns would therefore let the next render start while the
previous build is still being exported, so at N=1 two builds could be resident at their peaks.

The reconcile instead exports inside the slot, through one helper both kinds use:

```go
// convertRender exports each rendered resource to JSON once, hashes those
// bytes into the render digest, decodes the same bytes into the unstructured
// copies for apply, and then drops result.Resources.
func convertRender(result *render.RenderResult) (*convertedRender, error)
```

- `status.RenderDigestJSON` marshals each resource once and returns the digest (same bytes, same
  order, so the same value `RenderDigest` gives) together with the JSON. The conversion decodes
  those bytes, so the reconcile exports the rendered set once, not twice. The renderer already
  exports every resource once for the inventory entries (`buildInventoryEntries` calls
  `ToUnstructured`), so the total is two exports per resource, down from three. Both run inside the
  slot.
- On every exit, failures included, it sets `result.Resources = nil` (a deferred assignment) before
  it returns, still inside the slot. Every later
  phase reads `convertedRender.resources` or the result's plain data (inventory entries, warnings,
  required contracts, platform identity).
- A failure keeps its reason: a marshal failure marks the object Stalled with `RenderFailed`
  ("computing render digest"), a decode failure with `ApplyFailed` ("converting resources"), as
  before. The helper returns a `*conversionError` carrying that reason.
- ModuleInstance: `renderAndConvertInstance` renders and converts inside the `Run` closure. A
  conversion failure still returns the render result, so `ModuleResolved` and the render
  diagnostics are reported as before. The helper keeps `ReconcileModuleInstance` below the gocyclo
  limit of 30.
- ModulePackage: `renderModulePackage` converts inside its closure when the render succeeded with
  the expected kind, and returns the `*convertedRender`; `computeModulePackageDigests` and
  `applyAndPruneModulePackage` take it. The conversion now also runs on the no-op path. Since the
  digest already exported every resource, the extra cost there is the JSON decode only.

After the slot is released a reconcile holds no CUE value from its render, so the flag bounds the
builds resident in the process, not only the renders in flight.

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

80% of 4096 MiB is 3276.8 MiB. `GOMEMLIMIT` already counts the Go heap, goroutine stacks and
runtime metadata; the headroom covers what it does not see (memory allocated outside the Go
runtime, the binary's mapped pages, and kernel page cache charged to the container's cgroup) and
the transient overshoot while a render's peak is being collected. The comment above `resources` keeps its account
of why the limit is 4Gi and adds the soft limit and the shared render bound.

## Risks / Trade-offs

- [Throughput at the default] → a cluster with both kinds now renders them serially. That is the
  documented meaning of the default; the flag raises it. Holding the slot through the export
  lengthens each slot by the export, which the render digest already paid before this change.
- [Something else retains the build] → `RenderResult` is the only value the reconcile keeps across
  phases, and its other fields are plain data. If a heap profile later shows the build still
  reachable after conversion, the measurement task finds it; this change does not claim a number.
- [A literal drifts from the limit] → a kustomize patch that raises the memory limit without
  touching `GOMEMLIMIT` leaves the soft limit low, which costs extra GC but never an OOMKill. One
  that lowers the limit below `GOMEMLIMIT` silently disables the soft limit. The manifest comment
  and `docs/RENDERING.md` say to move both, and a test pins both values in `dist/install.yaml`.
- [Slot starvation] → a channel semaphore is not FIFO, so under sustained load a reconcile can wait
  longer than its arrival order suggests. Every slot is released on every return path, a panic
  included (section 2).
- [Cross-kind head-of-line blocking] → nothing on the render path has a timeout, so a render stuck
  on registry I/O holds its slot until it returns. At the default of 1 that now delays every render
  of both kinds, where before it delayed only its own kind. Accepted at the default of 1 and
  documented in the proposal and `docs/RENDERING.md`; a render timeout is a separate change and out
  of scope here.
- [Slots count renders, not bytes] → at N above 1, N renders of the largest module can still
  coincide. The sizing rule in `docs/RENDERING.md` stays: size N against the largest module.
