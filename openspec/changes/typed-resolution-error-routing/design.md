# Design — typed-resolution-error-routing

## Context

See proposal.md § Why for motivation. Facts that shape the approach, verified against
library `v1.0.0-alpha.13+` and the current operator tree:

- Both renderers wrap kernel errors with `%w` end to end
  (`internal/render/kernel_module_renderer.go`: "acquiring module: %w",
  "compiling module release: %w"; `internal/render/kernel_package_renderer.go`:
  "compiling module instance: %w"), so typed errors survive into both classifiers.
  Neither wrap string is matched by today's string-based classification — that is the
  gap being fixed.
- `oerrors.IdentityError` is a **value type** (value receiver on `Error()`), returned
  bare by `library/opm/helper/loader/registry/module.go` and wrapped once by the
  module renderer. The `errors.AsType` target is `oerrors.IdentityError`, not a pointer.
- `*oerrors.UnresolvedDemandsError` reaches the operator inside a bare
  `errors.Join(gate...)` from `library/opm/compile/module.go:142`, possibly alongside
  `*compile.UnmatchedComponentsError`. Both carry `Unwrap() []error`; `errors.AsType`
  traverses join trees, so one wrap layer plus the join is transparent.
- The transform-failure join (`compile/module.go:156`, "executing transforms") carries
  no typed resolution errors and must keep classifying as `RenderFailed`.
- `errors.AsType` is already the repo idiom (`internal/controller/platform_controller.go:138`).

### Reachability by path

| | ModuleInstance path | ModulePackage path |
| --- | --- | --- |
| `IdentityError` | Reachable via registry acquire | **Unreachable** — packages load from a Flux artifact, never acquire from the registry. (Materialize-time `IdentityError` belongs to the Platform reconciler and is already handled via `*MaterializeError`.) |
| `*UnresolvedDemandsError` | Reachable via kernel compile | Reachable via kernel compile |

## Goals / Non-Goals

**Goals:**

- Route both typed failure classes to `ResolutionFailedReason` on every path where
  they can occur, ahead of the string fallbacks.
- One shared classification helper so the two reconcile paths cannot drift.

**Non-Goals:**

- No new reason constants; no CRD or event-vocabulary change.
- No change to outcome/requeue behavior — both classes remain `FailedStalled` with
  the stalled recheck interval.
- No reworking of the string fallbacks; they stay for untyped loader-path errors.
- The second retarget follow-up (registry-backed integration-tier skipping) — separate change.

## Decisions

**D1 — Shared typed helper, per-path string fallbacks.** Add one helper in
`internal/reconcile` (e.g. `isTypedResolutionError(err) bool`) checking
`errors.AsType[oerrors.IdentityError]` and
`errors.AsType[*oerrors.UnresolvedDemandsError]`, called by both
`classifyRenderError` (moduleinstance.go) and `renderErrorReason` (modulepackage.go)
before their string checks.
*Why over recording a divergence (the proposal's either/or):* the package path
demonstrably receives typed `UnresolvedDemandsError` through `%w`, so string-only
classification there is simply the same bug. The `IdentityError` check is unreachable
dead code on the package path but harmless; a single helper beats two
almost-identical ones or a documented asymmetry.
*Why not match the new wrap strings instead:* strings are the fragile mechanism that
caused the gap; the library now exports types precisely so callers can branch on them.

**D2 — Delta specs land in `module-instance-synthesis` (MODIFIED: Status reporting)
and `modulepackage-kernel-rendering` (ADDED).** The proposal's original candidates
(`reconcile-loop-assembly`, `modulepackage-reconcile-loop`) do not mention resolution
routing at all. `module-instance-synthesis` owns the instance path's
`ResolutionFailed` contract. For the package path, `modulepackage-artifact-loading`'s
`ResolutionFailed` scenario covers registry/dependency resolution during artifact
loading — a different mechanism — while unresolved demands arise during kernel
rendering against the materialized platform, which `modulepackage-kernel-rendering`
owns; a new requirement there avoids distorting the artifact-loading contract.

**D3 — Typed checks run after the `ErrPlatformNotReady` gate.** Both classifiers
check the platform gate first (unchanged), preserving
`platform-gated-rendering`'s guarantee that the gate never reports
`ResolutionFailed` (guarded by
`internal/controller/moduleinstance_platform_gate_test.go:152`).

## Risks / Trade-offs

- **Alerting on `RenderFailed` sees two failure classes move to `ResolutionFailed`**
  → deliberate and fix-shaped within the alpha line; called out in proposal § Impact.
- **Library changes the join/wrap shape and typed routing silently degrades to the
  string fallback** → task 1.2's test pins classification against the library's
  actual join shape, so a shape change fails the operator's tests rather than
  silently downgrading the reason.
- **Dead `IdentityError` check on the package path misleads a future reader into
  thinking packages acquire from the registry** → the shared helper's comment records
  the reachability asymmetry (this table).
