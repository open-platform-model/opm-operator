## Why

The operator decides "retry or stall" for a failed render with an operator-only sentinel, `render.ErrAcquire`, added by `retry-transient-acquire-failures` as a stop-gap. It marks the acquisition phase, not the cause, so it gets two cases wrong:

- **Author defects retry forever.** On a ModulePackage, every failure of `Kernel.AcquireInstanceFromDir` is marked, so a CUE syntax error, values that conflict with `#config` or non-concrete values retry on the 5-minute backoff as a transient `ResolutionFailed`. Nothing but an edit can fix them. Wave 1 recorded this gap (opm-operator#190) and the operator-conditions page says "until typed library errors land".
- **Registry blips after acquisition stall.** A registry failure during `Kernel.SynthesizeInstance` or `Kernel.Render` (the render build resolves the platform's and the module's CUE dependencies) carries no mark, so it stalls as `RenderFailed` for 30 minutes. That is the defect owner decision a3 fixed for acquisition ("fetch failures must not stall for 30 minutes"), still open after it.

The Platform reconciler has its own best-effort probe (`isTransientFailure`: a `net.Error` timeout, any `*url.Error`, `context.DeadlineExceeded`), a second classification that can drift from the first.

Library `v1.0.0-beta.6` now types these failures (0021:D8:R12): `oerrors.ErrTransient`, `*oerrors.FetchError` with a `Kind` and an HTTP `Status`, and `oerrors.Classify`, applied at every site where a registry fetch or a dependency resolution leaves the library. A failure that is not a registry interaction (a syntax error, a conflict, an import no module provides) stays unclassified.

Owner decisions (2026-10-02/03 walkthrough): d1, "Library first, then operator (after a3) and cli"; a3, "typed library errors (d1) refine later". The operator's policy follows the intent of a3: a registry fetch failure of any kind retries with backoff, the typed terminal causes stall, and an unclassified error from a CUE build or values evaluation stalls.

This is also the first operator change on library `v1.0.0-beta.6`. The cascade PR opm-operator#248 (open on 2026-10-05) moves the same pin, together with test and release-tool pins, but keeps the deprecated import. This change carries identical `go.mod` and `go.sum` lines for the library, so it merges cleanly before or after #248. It also carries the bump and moves the duplicate-identity check from the deprecated `opm/helper/objectset` to `opm/k8s/object`, which clears the SA1019 lint the deprecation raises.

## What Changes

- **Library `v1.0.0-beta.6`.** `go.mod`/`go.sum` move from `v1.0.0-beta.4`; `internal/reconcile/resolution.go`, `internal/render/kernel_module_renderer.go` and their tests import `opm/k8s/object` instead of `opm/helper/objectset` (`Duplicates`, `*DuplicateIdentitiesError`; the library documents them as identical). The diagnostics page's `DuplicateIdentities` row points at the new file.
- **One typed predicate, `reconcile.IsTransientFailure`.** An error is transient when its chain holds a `*oerrors.FetchError` of any kind and none of the typed terminal causes: `oerrors.IdentityError`, `oerrors.ErrWrongKind`, `oerrors.ErrInvalidPackage`, `oerrors.ErrMissingRequiredField`, `*oerrors.UnresolvedDemandsError`, `*oerrors.UnmatchedComponentsError`. It replaces `isTransientAcquireFailure` (which keyed on `render.ErrAcquire`) in both reconcile loops and the Platform reconciler's `isTransientFailure` (which keyed on `net.Error` and `*url.Error`). The Platform alone also keeps `context.DeadlineExceeded` on its short recheck, as today, because `platformmodule.Closure` returns `ctx.Err()` raw. It reads types only, never message text.
- **It applies to every kernel call.** The classifiers see every renderer error, so a fetch failure from acquisition, values compile, `SynthesizeInstance`, `Render` or `AcquireInstanceFromDir` retries the same way.
- **`render.ErrAcquire` narrows to a reason marker.** It no longer decides retry. It keeps deciding the Ready reason of a stalled acquisition failure (`ResolutionFailed`, the retry-transient-acquire-failures design's reason mapping: ResolutionFailed for every `ErrAcquire` failure). No reason constant and no message text changes in this change's classification code, and the stalled reason of an acquisition failure stays `ResolutionFailed`. The one reason move is a registry failure after acquisition, from `RenderFailed` to `ResolutionFailed` (release note 3).
- **Tests.** A table of fetch kinds against the three phases, the ModulePackage syntax-error regression driven through the real renderer, registry-free renderer tests that pin the library's classification at the operator's wrap sites, envtest specs on both kinds, and the Platform probe's table.
- **Specs and docs.** `reconcile-backoff` replaces its acquisition requirement with a fetch-failure one; `modulepackage-artifact-loading` replaces its evaluation requirement (the scenario "Package that fails to load without a typed cause retries" turns false); `module-instance-synthesis`, `platform-reconciler` and `modulepackage-reconcile-loop` are amended. The last fixes the stale "Render failure" scenario (follow-up x3 of the a3 review). `docs/RENDERING.md` and `docs/site/diagnostics/operator-conditions.md` lose their "until typed library errors land" wording.

## Release notes (user-visible condition changes)

The PR title is `fix:` and its body MUST name these, because the condition a failing object reports changes:

1. **A ModulePackage with an author defect now stalls.** A CUE syntax error, values that conflict with `#config` or non-concrete values in the package report `Ready=False`, `Stalled=True`, reason `ResolutionFailed` (unchanged), and requeue on the 30-minute recheck instead of the 5-minute-capped backoff. Fix the package; a new artifact revision re-triggers the reconcile at once.
2. **A ModuleInstance acquisition failure that is not a registry failure now stalls**, with the same reason `ResolutionFailed`, on the 30-minute recheck.
3. **A registry failure during instance synthesis or render now retries.** It used to report `Stalled=True` with reason `RenderFailed`; it now reports `Ready=False` with reason `ResolutionFailed`, no `Stalled` condition, on the backoff.
4. **A Platform whose catalog pin is not found or whose registry refuses the credentials now rechecks after 1 minute instead of 30.** Its condition (`BuildFailed`, `Stalled=True`) is unchanged. A registry error the library does not classify waits 30 minutes, as any unrecognised cause did before.

5. **The platform is generated against core `v2.0.0-beta.4`.** The library bump moves the core pin of the operator's generated platform module (`schema.DefaultSchemaModule`) from `opmodel.dev/core@v2.0.0-beta.2` to `@v2.0.0-beta.4`, which has stricter attachment maps (owner decision j3), and dependency resolution lifts every render build to it. A module whose attachment maps are mis-keyed now fails its render. The library also changed how a values conflict on a package load is attributed, so that message may read differently.

A not-found module or package and a refused credential keep retrying on the backoff, as today. This change's own classification code changes no status or event message.

## Classification

**PATCH** (pre-GA; ships as the next `-beta.N` under a `fix` title). No API type, CRD schema or reason constant changes, and this change's classification code changes no message. The behaviour change is confined to failing objects and listed above, together with what the library bump brings (release note 5). The library shipped its core move as `fix(deps)!`; whether this PR's title takes `!` is decided at PR time. Complexity (Principle VII): one typed predicate replaces two (`isTransientAcquireFailure` and the Platform probe).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `reconcile-backoff`: "Acquisition failures without a typed terminal cause are transient" is REMOVED and replaced by "Registry fetch failures are transient wherever they occur" (typed `*FetchError` of any kind, any phase; unclassified failures stall).
- `module-instance-synthesis`: "Status reporting" and "End-to-end release scenarios" say a registry fetch failure retries in any phase and an unclassified acquisition failure stalls.
- `modulepackage-artifact-loading`: "CUE evaluation with registry resolution" is REMOVED and replaced by "CUE evaluation with typed registry failure classification".
- `modulepackage-reconcile-loop`: "Full reconcile loop execution" splits the stale "Render failure" scenario into a package load failure (stalled `ResolutionFailed`), an evaluation failure at render (stalled `RenderFailed`) and a registry failure (transient).
- `platform-reconciler`: "Transient failures retry faster than semantic failures" names the library's typed fetch failures instead of network and URL errors.

## Impact

- Code: `go.mod`, `go.sum`; `internal/reconcile/resolution.go`, `moduleinstance.go`, `modulepackage.go`; `internal/render/kernel_module_renderer.go` (sentinel comment, import); `internal/controller/platform_controller.go` (probe deleted, shared predicate called).
- Tests: `internal/reconcile/resolution_test.go`, `internal/render/acquire_wrap_test.go`, `internal/render/kernel_module_renderer_test.go`, `internal/controller/platform_transient_test.go`, `internal/controller/testhelpers_test.go`, `internal/controller/moduleinstance_reconcile_test.go`, `internal/controller/modulepackage_controller_test.go`, `test/integration/reconcile/suite_test.go` and its specs.
- Docs: `docs/RENDERING.md`, `docs/site/diagnostics/operator-conditions.md`.
- Core pin: the generated platform module moves to `opmodel.dev/core@v2.0.0-beta.4` through the library's `schema.DefaultSchemaModule` (release note 5).
- Downstream: none. The cli moves to `Classify` in its own change (cli-d1); the two are independent.
- Merge order in this repo: this change first, then the render-timeout and `i3g2` changes, which edit the same renderer files.
- Enhancement: `enhancement.yaml` declares 0021 with no decision claimed. 0021:D8 is a set of GA exit criteria, and R12 is the library's to satisfy; this change consumes it.
