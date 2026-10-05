## Context

Every renderer error reaches one of two classifiers, which pick a Ready reason and a retry class:

- ModuleInstance: `classifyRenderError` (`internal/reconcile/moduleinstance.go`, about line 1182).
- ModulePackage: `renderModulePackage` (`internal/reconcile/modulepackage.go`, about line 566), with `renderErrorReason`.

Both call `isTransientAcquireFailure` (`internal/reconcile/resolution.go`): `errors.Is(err, render.ErrAcquire) && !isTerminalAcquireCause(err)`. A transient failure is `Ready=False`/`ResolutionFailed`, no `Stalled`, one Warning event, `FailedTransient` on the bounded backoff (5s doubling, 5m cap). Everything else is `MarkStalled` with `renderFailureReason(err)` (SkewRefused, DuplicateIdentities, ResolutionFailed for typed resolution causes and for anything under `ErrAcquire`, otherwise RenderFailed) on the 30-minute `StalledRecheckInterval`.

`render.ErrAcquire` is set at two sites: `KernelModuleRenderer.synthesize` around `moduleacquire.Acquire` (`acquiring module: ...`) and `KernelPackageRenderer.Render` around `Kernel.AcquireInstanceFromDir` (`loading package: ...`, after the `ErrWrongKind` branch).

The Platform reconciler (`internal/controller/platform_controller.go`, `failReconcile` and `isTransientFailure`, about lines 441-487) always marks a failure Stalled and only picks the requeue interval: 1 minute (`transientRecheckInterval`) when `isTransientFailure` sees a `net.Error` with `Timeout()`, any `*url.Error` or `context.DeadlineExceeded`, else 30 minutes.

What library `v1.0.0-beta.6` returns (re-checked against the tag):

| Operator call | Library site | Classified |
| --- | --- | --- |
| `moduleacquire.Acquire` → `Kernel.AcquireModuleFromRegistry` | `opm/internal/loader/registry.go` `classifyFetch` | registry fetch, with `Coordinate` |
| `Kernel.AcquireInstanceFromDir` | `opm/internal/loader/load.go:109` | `cue/load` instance error |
| `Kernel.LoadSourceFromBytes` (values) | `opm/kernel/source_loader.go:113` | `cue/load` instance error |
| `Kernel.Render` | `opm/internal/renderstage/stage.go:217` | render module load |
| `platformmodule.Closure` (Platform) | `opm/helper/platformmodule/closure.go:105` | module-file fetch; its `ctx.Err()` returns raw |
| `Kernel.AcquirePlatformFromDir` (Platform) | `opm/internal/loader/load.go:109` | `cue/load` instance error |
| schema `OCILoader` (core schema, startup) | `opm/schema/loader.go:164` | `cue/load` instance error; unreachable at reconcile time, see below |

`oerrors.Classify` wraps a recognised failure in `*FetchError{Kind, Coordinate, Status, Err}` whose `Error()` is the cause's message unchanged. It returns the error unchanged when it recognises nothing (a syntax error, a conflict, an import no module provides, a missing package under the main module's own path), when the chain holds `context.Canceled`, or when it already holds a `*FetchError`. `errors.Is(err, ErrTransient)` holds only for `FetchUnreachable` (no HTTP answer, deadline included) and a 5xx status.

The schema `OCILoader` also classifies, but no reconcile meets its error. The schema `Cache` never retries (owner decision i4) and memoizes the error, so a memoized `*FetchError` reaching a reconcile loop would retry on the backoff with no chance of recovery. Today `cmd/main.go` `verifyCoreSchema` primes the Cache at startup and exits on failure, so the path is closed; a later change that loads the schema lazily must stall that error, not retry it.

Reconcile phase impact: Render classification only (and the Platform's requeue interval). Source, Apply, Prune and Inventory are untouched. Status: the Stalled condition and, in one case, the Ready reason of a failing object change; `nextRetryAt` and `failureCounters.reconcile` follow the existing transient and stalled paths.

## Goals / Non-Goals

**Goals:**

- A registry fetch failure retries on the bounded backoff wherever a kernel call meets it: acquisition, values compile, synthesis, render, package load.
- A failure the library does not classify as a registry fetch stalls on the 30-minute recheck, closing the ModulePackage author-defect retry gap.
- The typed terminal causes stall, as today.
- One predicate for both reconcile loops and the Platform; no message text read anywhere.
- The operator builds on library `v1.0.0-beta.6` without the deprecated `opm/helper/objectset`.

**Non-Goals:**

- New reason constants, API or CRD changes, changed status or event messages.
- A fast retry tier for `ErrTransient` versus a slow one for not-found. The owner's a3 intent is that a fetch failure must not stall for 30 minutes; a typo'd version retrying on the 5-minute cap is the cost a3 already accepted.
- A render timeout (a later change, op-render-timeout). It decides there whether its own deadline counts as transient.
- Retrying the Flux artifact fetch differently (already `FetchFailed`, transient).
- The cli (cli-d1 is its own change).

## Research & Decisions

Explored: the wave-2 plan research for op-d1, re-checked against opm-operator `origin/main` at bcfa722 and library `v1.0.0-beta.6` (`opm/errors/fetch.go`, `classify.go`, and every `Classify(` call site).

### D1. One exported predicate, `reconcile.IsTransientFailure`

**Context.** Two loops and the Platform reconciler must agree on what retries.

**Explored.** (a) `errors.Is(err, oerrors.ErrTransient)` only; (b) any `*FetchError`; (c) keep `ErrAcquire` as the retry key and add `ErrTransient`.

**Decision.** (b), minus the terminal causes:

```go
// IsTransientFailure reports a failure a later attempt may get past with
// nothing changed: a registry fetch or dependency resolution failure the
// library typed (*oerrors.FetchError, any Kind), with no typed terminal cause
// in the chain.
func IsTransientFailure(err error) bool {
	if err == nil || isTerminalCause(err) {
		return false
	}
	_, ok := errors.AsType[*oerrors.FetchError](err)
	return ok
}
```

`isTerminalCause` is today's `isTerminalAcquireCause` renamed, because it now applies to every phase: `ErrWrongKind`, `ErrInvalidPackage`, `ErrMissingRequiredField`, and `isTypedResolutionError` (`IdentityError`, `*UnresolvedDemandsError`, `*UnmatchedComponentsError`).

**Rationale.** (a) would stall a not-found module, a 401 or a 429 for 30 minutes, which is the defect a3 fixed; the owner's a3 policy retries every fetch failure. (c) keeps the phase as the retry key, which is what let author defects retry and render-phase blips stall. A raw `context.DeadlineExceeded` is not in the shared predicate: no owner decision asks the reconcile loops to retry it, the reconcile context carries no deadline today, and the library already maps a deadline at a fetch site to `FetchUnreachable`. A render deadline belongs to op-render-timeout, which decides its own class. The Platform keeps it on the short recheck at its own call site (D4), because `platformmodule.Closure` returns `ctx.Err()` raw and the old probe treated it as transient. `context.Canceled` is not transient: the library leaves it plain on purpose, it means the caller is stopping, and a manager shutdown re-lists every object on start. The predicate is exported because `internal/controller` calls it; it lives in `internal/reconcile/resolution.go` beside the reason mapping.

### D2. `render.ErrAcquire` stays, as the reason marker only

**Context.** The plan proposed deleting `ErrAcquire`, `acquireError` and `acquireFailed` if nothing else needed them. `renderFailureReason` needs them: a stalled acquisition failure reads `ResolutionFailed` (the retry-transient-acquire-failures design's reason mapping: ResolutionFailed for every `ErrAcquire` failure), and without the mark an unclassified package load failure would read `RenderFailed`.

**Decision.** Keep the sentinel, the wrapper and both wrap sites unchanged. Rewrite the sentinel's doc comment: it marks the acquisition phase and decides the Ready reason of a stalled failure; it no longer decides retry. `renderFailureReason` keeps `errors.Is(err, render.ErrAcquire)` in its `ResolutionFailed` case.

**Rationale.** No reason constant and no message text changes, and the stalled reason of an acquisition failure stays `ResolutionFailed`. The one reason move is a registry failure after acquisition, from `RenderFailed` to `ResolutionFailed` (proposal release note 3). That keeps the operator-conditions page, alerts and the "Conditions and events text must not change" constraint true. Only the retry class of a failing object moves. Deleting the marker would rename the stalled reason of every package with a syntax error, a second user-visible change for no gain.

### D3. Where the predicate runs

**Decision.** `classifyRenderError` and `renderModulePackage` call `IsTransientFailure(err)` where they call `isTransientAcquireFailure(err)` today, after the `ErrPlatformNotReady` branch. The transient branch is unchanged: `ResolutionFailed`, no `Stalled`, Warning event, `FailedTransient` (ModulePackage: `modulePackageBackoff(pkg)`). The stalled branch is unchanged: `renderFailureReason` / `renderErrorReason`. The Platform's local `isTransientFailure`, which `failReconcile` calls, becomes `opmreconcile.IsTransientFailure(err) || errors.Is(err, context.DeadlineExceeded)`; its `net.Error` and `*url.Error` checks and the `net` and `net/url` imports go.

**Rationale.** The classifiers see every renderer error, so one call site per loop covers acquisition, values compile, synthesis, render and package load: no per-call wrapping is needed. A transient failure reads `ResolutionFailed` in every phase, because it is a failure to resolve an input, not a defect in the render.

### D4. The Platform's probe narrows to typed failures

**Context.** The old probe treated any `*url.Error` and any timing-out `net.Error` as transient. Every registry path the Platform reaches now passes through `Classify` (Closure, `AcquirePlatformFromDir`'s loader), and `Classify` maps a `net.Error` (which `*url.Error` is) to `FetchUnreachable`.

**Decision.** Drop the `net.Error` and `*url.Error` checks. A failure `IsTransientFailure` accepts (a `*FetchError` of any kind), or a raw `DeadlineExceeded`, rechecks after 1 minute; everything else after 30. The shared predicate picks the Platform's requeue interval only: the condition (`BuildFailed`/`GenerateFailed`, `Stalled=True`) is unchanged, so "one rule" holds for the retry decision, not for the condition.

**Rationale.** One rule in the operator. A not-found pin now rechecks every minute; that is the owner's a3 policy applied to the Platform, and a Platform reconcile is one closure walk, cheap at that rate. Section 1's spike confirms that an unreachable registry reaches `failReconcile` as a `*FetchError`; if it does not, the spike's finding goes here and the probe keeps its typed `net.Error` row.

### D5. Library bump and import swap ride section 1

**Decision.** `go get github.com/open-platform-model/library@v1.0.0-beta.6`, `go mod tidy`, and replace `opm/helper/objectset` with `opm/k8s/object` (`object.Duplicates`, `*object.DuplicateIdentitiesError`) in the four files that import it. Section 1 lands with no classification change, so `main` stays releasable if the series stops there.

**Rationale.** The classification sections need `*FetchError`. The cascade PR opm-operator#248 moves the same pin but keeps the deprecated import; this change carries identical library lines in `go.mod` and `go.sum`, so the two merge in either order. The bump also moves the generated platform's core pin to `opmodel.dev/core@v2.0.0-beta.4` (`schema.DefaultSchemaModule`, owner decision j3) and brings the library's changed values-conflict attribution on a package load; the proposal's release note 5 names both. The library keeps `opm/helper/objectset` as a deprecated copy and states the new home behaves identically, so the swap changes no message.

## Sections

1. Library `v1.0.0-beta.6`, the `opm/k8s/object` swap, and a spike: registry-free renderer tests that pin what the library returns at the operator's wrap sites (no behaviour change).
2. The typed predicate in both loops and the Platform, with unit tables and the stubs that model a registry failure moved onto `*FetchError`.
3. Envtest and integration coverage on both kinds, and the e2e run.
4. Docs.

## Risks / Trade-offs

- **A library site that forgets `Classify` stalls a registry blip** (30 minutes, not forever). Mitigation: the section 1 spike tests pin the classification at each operator wrap site, so a library regression fails here on the next bump.
- **The library's text fallback misclassifies an author defect as a fetch failure**, which would retry a defect. The library limits the fallback to registry-fetch phrasing; the syntax-error regression test in section 2 runs the real loader.
- **Not-found retries every 5 minutes (instances, packages) or every minute (Platform).** Accepted by a3. The registry sees one request per object per interval.
- **A render deadline would read as a fetch failure.** The library classifies a deadline inside a fetch as `FetchUnreachable`. op-render-timeout must check its own `ctx.Err()` before calling `IsTransientFailure`, or its timeout reports as a transient `ResolutionFailed`.
- **Same-file overlap.** op-render-timeout and op-i3g2 edit `kernel_module_renderer.go` and `kernel_package_renderer.go`; they start after this change merges.

## Migration Plan

None for users: no API change, and this change's classification code changes no message. The core pin move (release note 5) can surface a new render failure in a module with mis-keyed attachment maps; the fix is in the module. The release notes in the proposal list the condition changes a dashboard or alert keyed on `Stalled` will see.

## Open Questions

None.
