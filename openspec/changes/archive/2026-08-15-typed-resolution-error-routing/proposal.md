# Proposal: typed-resolution-error-routing

> Follow-up to `operator-library-retarget` (recorded there as a follow-up candidate, task 5.3 /
> design § "New failure classes ride existing routing"). The ordering gate is satisfied:
> `v1.0.0-alpha.9` is tagged and both prerequisite changes are complete.

## Why

Since the core-v2 retarget (library `v1.0.0-alpha.13`), two typed failure classes reach the
ModuleInstance render path that the classifier cannot see:

- `oerrors.IdentityError` from module acquire (returned bare): a published module's declared
  identity disagrees with the coordinate it was fetched by.
- `*oerrors.UnresolvedDemandsError` from compile (possibly joined with
  `*compile.UnmatchedComponentsError`): the rendered module demands contracts the materialized
  platform does not provide (0010 D28 made this fail instead of warn).

`classifyRenderError` (`internal/reconcile/moduleinstance.go:822`) routes to
`ResolutionFailedReason` only through `isResolutionError`'s string match ("loading synthesized
release" / "synthesizing release"), which misses both classes — they land on the coarser
`RenderFailedReason`. The failures surface correctly (stalled, Warning event, stalled recheck)
but under a reason that hides *what kind* of failure it is: an operator watching conditions
cannot distinguish "your module/platform disagree about contracts" from "your CUE failed to
evaluate". `ResolutionFailedReason` exists precisely for the former family and is barely
reachable today.

This was deliberately excluded from the retarget by its charter (0010: no feature code); this
change is the recorded home for it.

## What Changes

- **`internal/reconcile`**: a shared typed helper (design D1) checks
  `errors.AsType[oerrors.IdentityError]` (value type — no star) and
  `errors.AsType[*oerrors.UnresolvedDemandsError]`; both `classifyRenderError`
  (moduleinstance.go) and `renderErrorReason` (modulepackage.go) call it ahead of their string
  fallbacks, which stay for loader-path errors that carry no type. Both render paths wrap with
  `%w` end to end, so the types are visible on both; `IdentityError` is unreachable on the
  package path (no registry acquire) but the shared helper keeps the paths from drifting.
- **Tests**: unit coverage for both typed classifications (bare `IdentityError`, wrapped/joined
  `UnresolvedDemandsError`) plus a guard that `ErrPlatformNotReady` and plain render errors keep
  their current routing.
- **Spec deltas**: `module-instance-synthesis` (MODIFIED: Status reporting) and
  `modulepackage-kernel-rendering` (ADDED requirement) — see design D2 for why the proposal's
  earlier candidates were wrong.

## Capabilities

### New Capabilities

<!-- none -->

### Modified Capabilities

- `module-instance-synthesis` — the Status reporting requirement's `ResolutionFailed`
  boundary widens to cover identity mismatch and unresolved platform demands.
- `modulepackage-kernel-rendering` — new requirement: unresolved platform demands on the
  package render path classify as `ResolutionFailed`.
- (`status-conditions` is unchanged — no new reason constants.)

## Impact

- **SemVer: fix-shaped within the alpha line.** Condition *reasons* change for two failure
  classes (`RenderFailed` → `ResolutionFailed`); no CRD change, no new vocabulary — both reasons
  already exist in `internal/status/conditions.go`. Anything alerting on `RenderFailed`
  specifically will see these two classes move.
- **Ordering**: lands only after the `operator-library-retarget` + `examples-fleet-core-v2`
  release is cut (`v1.0.0-alpha.9`); this change deliberately did not ride that crossing.
  That gate is now satisfied — `v1.0.0-alpha.9` is tagged.
- **Not included**: the second retarget follow-up — wiring `OPM_TEST_REGISTRY_FORCE` (or a
  GHCR-tolerant helper) so the registry-backed integration tier stops silently skipping in CI —
  is a test-infrastructure concern with no shared code surface; it gets its own change.
