## Why

`--max-concurrent-renders` is documented as the memory bound on rendering, but it is not one. Both
render-bearing controllers take it as their own `MaxConcurrentReconciles` (`cmd/main.go:310,330`,
`moduleinstance_controller.go:129`, `modulepackage_controller.go:176`), so at the default of 1 a
ModuleInstance and a ModulePackage still render at the same time, and at N the process can have 2N
builds in flight. One cert-manager-sized render peaks at about 1.75 GB of heap (about 3.5 GB RSS)
against the shipped 4Gi limit, so two coinciding renders are an OOMKill, and an OOMKilled operator
gates every ModuleInstance in the cluster until it recovers.

Two smaller effects make it worse. A rendered resource (`pkg/core.Resource`) carries the CUE value it
came from, and a held value pins the whole build. The ModuleInstance reconcile keeps
`renderResult.Resources` reachable from the render (`internal/reconcile/moduleinstance.go:254`)
until the reconcile returns (`renderResult` is last read at `:452`), long after it was converted to
unstructured objects at `:294`; the ModulePackage reconcile does the same through
`applyAndPruneModulePackage` (`modulepackage.go:438`). And the Go runtime does not know the pod's
memory limit, so it collects against heap growth only and lets garbage from one render accumulate up
to the kill.

Owner decision (kernel plan walkthrough, 2026-10-03): "one render semaphore shared by both
controllers, plus nil-out of renderResult.Resources after conversion (decided earlier); GOMEMLIMIT
~80% of the pod limit in the manifest". This change is exactly those three items.

## What Changes

- **Rendered values are dropped once converted.** Right after `toUnstructuredSlice`, the
  ModuleInstance reconcile and `applyAndPruneModulePackage` set `renderResult.Resources` to nil.
  `RenderResult` holds no other CUE value (`InventoryEntries`, `Warnings`, `UnhandledTraits`,
  `ResolvedVersions`, `RequiredContracts` and `PlatformIdentity` are plain data), so from that point
  the reconcile no longer pins the build while it applies, prunes and patches status.
- **One render slot pool for the whole process.** `--max-concurrent-renders` becomes the number of
  render slots shared by the ModuleInstance and ModulePackage reconcilers. A reconcile takes a slot
  before it calls the renderer (lease, acquisition, synthesis, render) and gives it back when the
  renderer returns. At the default of 1, one render is in flight across both kinds; at N, N in
  total. Each controller keeps `MaxConcurrentReconciles` at the flag's value, so phases outside the
  render (apply, prune, deletion, suspend, CLI-owned handling) of one kind never queue behind the
  other kind's renders (design.md, "Reconcile concurrency stays per controller"). The flag's help,
  the controllers' field comments, `docs/RENDERING.md` and the kernel rule in `AGENTS.md` say what
  the flag bounds now and that no lock or ordering gate is held across a kernel call: a render holds
  its platform lease and one render slot, and neither orders or excludes particular calls. The slot
  is released by a deferred call, so a render that panics (controller-runtime recovers it) frees its
  slot too.
- **The manager sets a Go soft memory limit.** `config/manager/manager.yaml` sets
  `GOMEMLIMIT=3276MiB` on the manager container, about 80% of the 4Gi limit, as a literal next to
  the limit, and the memory comment there says the two move together. `dist/install.yaml` is
  regenerated.

## Classification

**PATCH** (pre-GA, released as `fix`, so the next `1.0.0-beta.N`). No API type, field, flag, reason
or condition is added or removed. One behaviour tightens: at the default of 1 a ModuleInstance render
and a ModulePackage render no longer overlap, so a cluster with both kinds renders them one after the
other. That is the documented meaning of the flag; an administrator who relied on the overlap raises
the flag to 2. It also brings cross-kind head-of-line blocking: nothing on the render path has a
timeout, so at the default of 1 a ModuleInstance render stuck on registry I/O now stalls every
ModulePackage render as well (and the other way round), where before it stalled only its own kind.
A render timeout would be a separate change.

Complexity (Principle VII): one small type (a counted slot pool with context-aware acquire) and one
field on each reconciler's params. Justified because the alternative bound, halving each
controller's reconcile concurrency, cannot express an odd total and would still let a ModulePackage
render while a ModuleInstance renders at the default.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `library-kernel-runtime`: "Single long-lived library Kernel" says no lock or ordering gate is held
  across a kernel call (a render holds its platform lease and one render slot, a memory bound and
  not a correctness gate); "Render
  concurrency is a manager flag bounded by memory" makes the flag a process-wide slot count shared by
  both kinds; two added requirements cover dropping rendered values after conversion and the Go soft
  memory limit in the shipped manifest.

## Impact

- `internal/render`: the slot pool type and its unit tests; the `KernelModuleRenderer` doc comment.
- `internal/reconcile/moduleinstance.go`, `internal/reconcile/modulepackage.go`: slot around the
  renderer call, nil-out after conversion; unit and integration tests in `internal/reconcile` and
  `test/integration/reconcile` (the test stub renderers return a copy per call).
- `internal/controller/moduleinstance_controller.go`, `modulepackage_controller.go`: a
  `RenderSlots` field passed to the params; comments.
- `cmd/main.go`: one pool constructed from the flag and handed to both reconcilers; flag help and
  the kernel comment.
- `config/manager/manager.yaml`, regenerated `dist/install.yaml`.
- `docs/RENDERING.md`, `AGENTS.md` (the "Every kernel call shares nothing" rule).
- No enhancement entry backs these decisions, so no `enhancement.yaml`.
