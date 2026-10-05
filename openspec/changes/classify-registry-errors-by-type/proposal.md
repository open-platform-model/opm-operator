## Why

The operator decides "retry or stall" for a failed render with an operator-only sentinel, `render.ErrAcquire`, added by `retry-transient-acquire-failures` as a stop-gap. It marks the acquisition phase, not the cause, so it gets two cases wrong:

- **Author defects retry forever.** On a ModulePackage, every failure of `Kernel.AcquireInstanceFromDir` is marked, so a CUE syntax error, values that conflict with `#config` or non-concrete values retry on the 5-minute backoff as a transient `ResolutionFailed`. Nothing but an edit can fix them. opm-operator#190 recorded this gap and the operator-conditions page says "until typed library errors land".
- **Registry blips after acquisition stall.** A registry failure during `Kernel.SynthesizeInstance` or `Kernel.Render` (the render build resolves the platform's and the module's CUE dependencies) carries no mark, so it stalls as `RenderFailed` for 30 minutes. That is the defect the retry-transient-acquire-failures change fixed for acquisition (a fetch failure must not stall for 30 minutes), still open after it.

The Platform reconciler has its own best-effort probe (`isTransientFailure`: a `net.Error` timeout, any `*url.Error`, `context.DeadlineExceeded`), a second classification that can drift from the first.

Library `v1.0.0-beta.6` now types these failures (0021:D8:R12): `oerrors.ErrTransient`, `*oerrors.FetchError` with a `Kind` and an HTTP `Status`, and `oerrors.Classify`, applied at every site where a registry fetch or a dependency resolution leaves the library. A failure that is not a registry interaction (a syntax error, a conflict, an import no module provides) stays unclassified.

The library typed these errors first so that the operator and the cli could refine the stop-gap. The operator's policy keeps its intent: a registry fetch failure of any kind retries with backoff, because a late publish or a fixed credential recovers on the next attempt; the typed terminal causes stall; and an unclassified error from a CUE build or values evaluation stalls, because only an edit fixes it.

opm-operator#248 moved the operator to library `v1.0.0-beta.6`, swapped `opm/helper/objectset` for `opm/k8s/object` and carried the core `v2.0.0-beta.4` migration note. This change builds on it and changes no dependency.

## What Changes

- **One typed predicate, `reconcile.IsTransientFailure`.** An error is transient when its chain holds a `*oerrors.FetchError` of any kind and none of the typed terminal causes: `oerrors.IdentityError`, `oerrors.ErrWrongKind`, `oerrors.ErrInvalidPackage`, `oerrors.ErrMissingRequiredField`, `*oerrors.UnresolvedDemandsError`, `*oerrors.UnmatchedComponentsError`. It replaces `isTransientAcquireFailure` (which keyed on `render.ErrAcquire`) in both reconcile loops and the Platform reconciler's `isTransientFailure` (which keyed on `net.Error` and `*url.Error`). The Platform alone also keeps `context.DeadlineExceeded` on its short recheck, as today, because `platformmodule.Closure` returns `ctx.Err()` raw. It reads types only, never message text.
- **It applies to every kernel call.** The classifiers see every renderer error, so a fetch failure from acquisition, values compile, `SynthesizeInstance`, `Render` or `AcquireInstanceFromDir` retries the same way.
- **`render.ErrAcquire` narrows to a reason marker.** It no longer decides retry. It keeps deciding the Ready reason of a stalled acquisition failure (`ResolutionFailed`, the retry-transient-acquire-failures design's reason mapping: ResolutionFailed for every `ErrAcquire` failure). No reason constant and no message text changes in this change's classification code, and the stalled reason of an acquisition failure stays `ResolutionFailed`. The one reason move is a registry failure after acquisition, from `RenderFailed` to `ResolutionFailed` (release note 3).
- **Tests.** A table of fetch kinds against the three phases, the ModulePackage syntax-error regression driven through the real renderer, registry-free renderer tests that pin the library's classification at the operator's wrap sites, envtest specs on both kinds, and the Platform probe's table.
- **Specs and docs.** `reconcile-backoff` replaces its acquisition requirement with a fetch-failure one; `modulepackage-artifact-loading` replaces its evaluation requirement (the scenario "Package that fails to load without a typed cause retries" turns false); `module-instance-synthesis`, `platform-reconciler` and `modulepackage-reconcile-loop` are amended. The last fixes the stale "Render failure" scenario. `modulepackage-kernel-rendering` separates a registry fetch failure from the "Ordinary evaluation error keeps RenderFailed" scenario. `docs/RENDERING.md` and `docs/site/diagnostics/operator-conditions.md` lose their "until typed library errors land" wording.

## Release notes (user-visible condition changes)

The PR title is `fix:` and its body MUST name these, because the condition a failing object reports changes:

1. **A ModulePackage with an author defect now stalls.** A CUE syntax error, values that conflict with `#config` or non-concrete values in the package report `Ready=False`, `Stalled=True`, reason `ResolutionFailed` (unchanged), and requeue on the 30-minute recheck instead of the 5-minute-capped backoff. Fix the package; a new artifact revision re-triggers the reconcile at once.
2. **A ModuleInstance acquisition failure that is not a registry failure now stalls**, with the same reason `ResolutionFailed`, on the 30-minute recheck.
3. **A registry failure during instance synthesis or render now retries.** It used to report `Stalled=True` with reason `RenderFailed`; it now reports `Ready=False` with reason `ResolutionFailed`, no `Stalled` condition, on the backoff.
4. **A Platform whose catalog pin is not found or whose registry refuses the credentials now rechecks after 1 minute instead of 30.** Its condition (`BuildFailed`, `Stalled=True`) is unchanged. A registry error the library does not classify waits 30 minutes, as any unrecognised cause did before.

A not-found module or package and a refused credential keep retrying on the backoff, as today. This change's own classification code changes no status or event message.

## Classification

**PATCH** (pre-GA; ships as the next `-beta.N` under a `fix` title). No API type, CRD schema or reason constant changes, and this change's classification code changes no message. The behaviour change is confined to failing objects and listed above. The library bump and its core move shipped separately as opm-operator#248 under `fix(deps)!`, so this change's title is a plain `fix:`. Complexity (Principle VII): one typed predicate replaces two (`isTransientAcquireFailure` and the Platform probe).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `reconcile-backoff`: "Acquisition failures without a typed terminal cause are transient" is REMOVED and replaced by "Registry fetch failures are transient wherever they occur" (typed `*FetchError` of any kind, any phase; unclassified failures stall).
- `module-instance-synthesis`: "Status reporting" and "End-to-end release scenarios" say a registry fetch failure retries in any phase and an unclassified acquisition failure stalls.
- `modulepackage-artifact-loading`: "CUE evaluation with registry resolution" is REMOVED and replaced by "CUE evaluation with typed registry failure classification".
- `modulepackage-reconcile-loop`: "Full reconcile loop execution" splits the stale "Render failure" scenario into a package load failure (stalled `ResolutionFailed`), an evaluation failure at render (stalled `RenderFailed`) and a registry failure (transient).
- `modulepackage-kernel-rendering`: "Unresolved platform demands classify as resolution failures" says a registry fetch failure is not a refusal and retries; "Ordinary evaluation error keeps RenderFailed" excludes it.
- `platform-reconciler`: "Transient failures retry faster than semantic failures" names the library's typed fetch failures instead of network and URL errors.

## Impact

- Code: `internal/reconcile/resolution.go`, `moduleinstance.go`, `modulepackage.go`; `internal/render/kernel_module_renderer.go` (sentinel comment); `internal/controller/platform_controller.go` (probe deleted, shared predicate called).
- Tests: `internal/reconcile/resolution_test.go`, `internal/render/acquire_wrap_test.go`, `internal/render/kernel_module_renderer_test.go`, `internal/controller/platform_transient_test.go`, `internal/controller/testhelpers_test.go`, `internal/controller/moduleinstance_reconcile_test.go`, `internal/controller/modulepackage_controller_test.go`, `test/integration/reconcile/suite_test.go` and its specs.
- Docs: `docs/RENDERING.md`, `docs/site/diagnostics/operator-conditions.md`.
- Downstream: none. The cli moves to `Classify` in its own change; the two are independent.
- Merge order in this repo: this change first, then the planned render-timeout and package-renderer changes, which edit the same renderer files.
- Enhancement: `enhancement.yaml` declares 0021 with no decision claimed. 0021:D8 is a set of GA exit criteria, and R12 is the library's to satisfy; this change consumes it.
