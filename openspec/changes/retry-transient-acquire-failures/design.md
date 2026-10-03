## Context

Both reconcile loops hand a failed render to a classifier that picks a Ready reason and a retry class:

- ModuleInstance: `classifyRenderError` (`internal/reconcile/moduleinstance.go`). `ErrPlatformNotReady` is transient; everything else is `MarkStalled` with `renderFailureReason(err, isResolutionError)` and `FailedStalled`, requeued by `retryIntervalFor` on `StalledRecheckInterval` (30m).
- ModulePackage: `renderModulePackage` (`internal/reconcile/modulepackage.go`). Same shape, with `renderErrorReason` (unsupported kind first, then `renderFailureReason(err, isResolutionErrorMsg)`).

`renderFailureReason` (`internal/reconcile/resolution.go`) is shared and typed: skew, duplicate identities, then `isTypedResolutionError` (IdentityError, UnresolvedDemandsError, UnmatchedComponentsError), then the caller's string matcher, then `RenderFailed`.

Where acquisition fails today (library `v1.0.0-beta.1`):

| Path | Call | Wrap today | Can fail transiently | Typed terminal causes it can carry |
| --- | --- | --- | --- | --- |
| ModuleInstance | `moduleacquire.Acquire` over `Kernel.AcquireModuleFromRegistry` (`loader.FetchModule` with the kernel's registry env) | `acquiring module: acquiring module %q@%q: ...` | registry fetch | `IdentityError`, `ErrWrongKind`, `ErrInvalidPackage`, `ErrMissingRequiredField` (shape gate) |
| ModulePackage | `Kernel.AcquireInstanceFromDir` (`loader.LoadDir` with the kernel's registry env) | `loading package: ...` (after `ErrWrongKind` becomes `ErrUnsupportedKind`) | resolving the package's CUE dependencies | `ErrInvalidPackage`, `ErrMissingRequiredField` |

Neither library error tree distinguishes a network failure from a not-found or a CUE syntax error: those are raw `cue/load` and `modconfig` errors. Task d1 (library) will add `ErrTransient`, `*FetchError` and `opmerrors.Classify`; until then the operator can tell only "acquisition failed" from "acquisition failed with a typed terminal cause".

Reconcile phase impact: Render classification only. Source, Apply, Prune and Inventory are untouched. Status: the Ready reason and the Stalled condition of a failed acquisition change; `nextRetryAt` and `failureCounters.reconcile` follow the existing transient path.

## Goals / Non-Goals

**Goals:**

- A registry blip during acquisition retries on the bounded backoff (5s doubling, 5m cap) on both kinds.
- Typed terminal acquisition causes keep stalling on the 30m recheck.
- No render error is classified by its message text.

**Non-Goals:**

- Typed library errors (task d1) and a network-versus-not-found split. A permanent not-found retries every 5 minutes until d1 lands.
- Classifying failures after acquisition: values compile, `#config` validation, `SynthesizeInstance` and `Kernel.Render` keep their current stall.
- New reason constants, API or CRD changes.
- Retrying the Flux artifact fetch differently (already transient).

## Research & Decisions

Explored: the beta.1 kernel plan research for task a3, re-checked against `origin/main` at 9835474 and library `v1.0.0-beta.1`.

### D1. `render.ErrAcquire`, marked without changing the message

**Context.** The classifiers need a type-level signal that a failure happened during acquisition; today only the wrap text says so.

**Explored.** `fmt.Errorf("%w: %w", ErrAcquire, err)`; a library sentinel (task d1); a wrapper type that keeps the message.

**Decision.** `internal/render` declares `var ErrAcquire = errors.New("acquiring module source")` beside `ErrPlatformNotReady`. The two acquisition sites mark their error with it through one unexported helper whose result keeps the site's current message verbatim and unwraps to both the sentinel and the original error:

```go
// acquireError marks err as an acquisition failure (ErrAcquire) without
// changing its message; errors.Is finds the sentinel and errors.AsType still
// reaches every typed cause under err.
type acquireError struct {
	msg string
	err error
}

func (e *acquireError) Error() string   { return e.msg + ": " + e.err.Error() }
func (e *acquireError) Unwrap() []error { return []error{ErrAcquire, e.err} }

func acquireFailed(msg string, err error) error { return &acquireError{msg: msg, err: err} }

// kernel_module_renderer.go, synthesize:
//   return nil, acquireFailed("acquiring module", err)
// kernel_package_renderer.go, Render (after the ErrWrongKind branch):
//   return KindModuleInstance, nil, acquireFailed("loading package", err)
```

**Rationale.** `fmt.Errorf("%w: %w", ErrAcquire, err)` would prefix every status message and event with the sentinel's text, a user-visible rewording this change does not need. The helper keeps `status.conditions[Ready].message` byte-identical, which keeps the diagnostics docs' quoted messages true. The sentinel lives in the operator (owner decision a3): no library release, and d1 can later narrow it without touching the wrap sites.

### D2. One shared predicate decides transient versus terminal

**Context.** The owner's list of typed terminal causes must stall on both kinds, and the two loops must not drift.

**Explored.** A per-loop check in each classifier; one shared predicate in `resolution.go`.

**Decision.** `internal/reconcile/resolution.go` gains:

```go
// isTerminalAcquireCause reports a typed cause that retrying cannot fix.
func isTerminalAcquireCause(err error) bool {
	if errors.Is(err, oerrors.ErrWrongKind) ||
		errors.Is(err, oerrors.ErrInvalidPackage) ||
		errors.Is(err, oerrors.ErrMissingRequiredField) {
		return true
	}
	return isTypedResolutionError(err) // IdentityError, UnresolvedDemands, UnmatchedComponents
}

// isTransientAcquireFailure reports an acquisition failure with no typed
// terminal cause: a registry outage, a dependency that would not resolve,
// or (until typed library errors land) a not-found or a load error.
func isTransientAcquireFailure(err error) bool {
	return errors.Is(err, render.ErrAcquire) && !isTerminalAcquireCause(err)
}
```

Both classifiers consult it right after `ErrPlatformNotReady`:

```go
if isTransientAcquireFailure(err) {
	recorder.Eventf(obj, nil, corev1.EventTypeWarning, status.ResolutionFailedReason, "Render", "%s", err)
	status.MarkNotReady(obj, status.ResolutionFailedReason, "%s", err)
	return FailedTransient // MI: retryIntervalFor -> ComputeBackoff; MP: modulePackageBackoff(pkg)
}
```

**Rationale.** One function on both paths keeps the "shared classifier so they cannot drift" rule the `modulepackage-kernel-rendering` spec already states. `ErrMissingRequiredField` is not in the owner's list of four, but it is the shape gate's third sentinel beside `ErrInvalidPackage` and `ErrWrongKind` and names a package defect retrying cannot fix; the list is read as "the typed terminal causes", with this one added. `UnresolvedDemandsError` cannot sit under `ErrAcquire` today (it is a render verdict), but the predicate keeps it terminal so a later wrap cannot make it retry.

### D3. Reason: `ResolutionFailed` for every `ErrAcquire` failure

**Context.** Deleting the `loading package` matcher would move a ModulePackage `ErrInvalidPackage` from `ResolutionFailed` to `RenderFailed` unless the sentinel itself picks the reason. The owner named the stall, not the reason.

**Explored.** `RenderFailed` for terminal acquire causes (ModuleInstance status quo); `ResolutionFailed` for every `ErrAcquire` failure; a new reason constant.

**Decision.** A transient acquisition failure reports `ResolutionFailed`, not Stalled. A terminal one under `ErrAcquire` reports `ResolutionFailed` and Stalled: `renderFailureReason` adds `errors.Is(err, render.ErrAcquire)` to its `ResolutionFailed` case, after skew and duplicate identities. The `module-instance-synthesis` spec already defines `ResolutionFailed` as "the module cannot be resolved into a usable, trustworthy input", with Stalled only "when the failure is not transient", so no new reason is needed.

**Rationale.** One reason for every acquisition failure, transient or terminal, matching the spec's definition and keeping the ModulePackage reason as it is today. Consequences: on the ModuleInstance path an invalid or wrong-kind module moves from `RenderFailed` to `ResolutionFailed` (still Stalled); on the ModulePackage path every acquire failure was already `ResolutionFailed` through the `loading package` text.

### D4. Delete the string matchers and the fallback parameter

**Context.** The owner decision deletes the string matchers; one of them (`synthesizing release`) still matches a live error.

**Explored.** Keep `synthesizing release` as a matcher; wrap synthesis with `ErrAcquire`; let synthesis fall to `RenderFailed`.

**Decision.** `isResolutionError`, `isResolutionErrorMsg` and the `isResolutionMsg func(error) bool` parameter of `renderFailureReason` are deleted. `resolving` was reachable only through the `loading package` wrap, which `ErrAcquire` now covers. `synthesizing release` was live: a failed `SynthesizeInstance` now reports `RenderFailed` (Stalled, 30m), matching its scope as a post-acquisition failure.

**Rationale.** If synthesis turns out to fail transiently in practice (it loads the module's transitive dependencies through the same registry env), d1's typed errors are the place to catch it; wrapping it with `ErrAcquire` here would retry every values-concreteness error too.

## Risks / Trade-offs

- **A real typo retries forever on the 5m cap** instead of stalling on 30m. Six times the registry traffic for a broken object, and `Stalled` no longer flags it. Accepted by the owner; d1 restores a stall for a typed not-found.
- **CUE syntax errors in a ModulePackage's package retry as transient** (they surface from `LoadDir` untyped). Same trade-off, same refinement path.
- **Reason churn for alerting.** Anyone alerting on `RenderFailed` for unreachable modules sees `ResolutionFailed` instead. Pre-GA, documented in the diagnostics page.
- **Tests that relied on text.** `resolutionErrorRenderer` returns the dead `loading synthesized release` string in two places: `internal/controller/testhelpers_test.go` (the counter spec keeps its assertion once the stub returns an `ErrAcquire` error, since the counter increments on a transient outcome too) and `test/integration/reconcile/suite_test.go` (its three specs assert Stalled/ResolutionFailed, so that copy returns an `ErrAcquire`-marked `IdentityError` and stays on the stalled path).
- **A malformed `spec.module.version` retries on the 5m cap.** `module.NewVersion` fails with an untyped `parsing artifact version` error (library `loader/registry.go`), and the CRD checks only `MinLength=1` on the version. Same refinement path: d1's typed errors, or the library sibling accept-bare-semver-in-registry-verbs once the operator bumps its pin.
- **A stale main spec.** `modulepackage-reconcile-loop` "Render failure" ("the CUE package fails to evaluate" gives Stalled RenderFailed) predates the kernel path and already describes the retired `ReleaseReconciler`. A package that fails to load now retries as a transient `ResolutionFailed` (the `modulepackage-artifact-loading` delta says so); that main spec is left to the sync-stale-specs follow-up rather than modified here.
- **Textual conflicts with the sibling change bound-render-memory** (branch `fix/bound-render-memory`). Both edit the `RenderModule` call block in `moduleinstance.go`, `renderModulePackage`, the `stubRenderer`/`stubPackageRenderer` helpers and `docs/RENDERING.md`. There is no semantic conflict: the render-slot wait sits before the renderer and returns `ctx.Err()` unclassified. Whichever change merges second rebases onto the other, and the transient branch belongs inside the classifier, not around the slot acquire.

## Sections

1. Sentinel, wrap sites, shared predicate, both classifiers, matchers deleted, reconcile-level tests. `fix(controller)`; this is the release-bearing commit.
2. Envtest coverage for both kinds. `test(controller)`.
3. Docs. `docs`.
