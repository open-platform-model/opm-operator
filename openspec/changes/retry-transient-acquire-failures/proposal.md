## Why

A short registry outage stalls work that would succeed a minute later. Both reconcile loops treat a failed module acquisition as terminal:

- **ModuleInstance.** A registry fetch failure surfaces as `acquiring module: ...` (`internal/render/kernel_module_renderer.go`, `synthesize`). No typed cause and no matched text classifies it, so `renderFailureReason` returns `RenderFailed`, `classifyRenderError` marks the instance Stalled, and it requeues on the 30-minute `StalledRecheckInterval`. The `GenerationChangedPredicate` on the watch means nothing re-triggers it sooner.
- **ModulePackage.** The Flux artifact fetch already retries as transient, but `Kernel.AcquireInstanceFromDir` then resolves the package's CUE dependencies through the registry. A registry blip there is wrapped `loading package: ...`, matches the string fallback `isResolutionErrorMsg`, and stalls the package as `ResolutionFailed` for 30 minutes.

The classification also leans on message text. `isResolutionError` matches `loading synthesized release` (nothing produces it any more; only a test stub does) and `synthesizing release`. `isResolutionErrorMsg` matches `loading synthesized instance` (dead), `loading package` and `resolving` (reachable only through the `loading package` wrap). A reworded error silently changes a status reason.

Owner decision (2026-10-02 walkthrough, task a3): retry as transient now, through an operator-only sentinel (`ErrAcquire`) read with `errors.Is`, and delete the string matchers. Typed library errors (task d1: `ErrTransient`, `*FetchError`, `opmerrors.Classify`) will refine this later; this change needs no library release.

## What Changes

- **An operator sentinel, `render.ErrAcquire`.** Both renderers mark an acquisition failure with it: `KernelModuleRenderer` around `moduleacquire.Acquire`, and `KernelPackageRenderer` around `Kernel.AcquireInstanceFromDir` (the `loading package` site, after the existing `ErrWrongKind` to `ErrUnsupportedKind` branch). The error message and every typed cause underneath stay reachable: the status message reads exactly as today, and `errors.AsType` still finds the library's typed errors.
- **Untyped acquire and load failures retry as transient.** When a render error carries `ErrAcquire` and none of the typed terminal causes, both classifiers mark `Ready=False` with reason `ResolutionFailed` (not Stalled), emit a Warning event and return `FailedTransient`. The ModuleInstance requeues through `retryIntervalFor`; the ModulePackage through `modulePackageBackoff`. Both walk the existing bounded backoff, 5s doubling to the 5-minute cap. A permanent not-found (a typo in `spec.module.path`) therefore retries every 5 minutes instead of every 30, which the owner accepted as the cost of an operator-only fix.
- **Typed terminal causes stay stalled.** `oerrors.IdentityError`, `oerrors.ErrWrongKind`, `oerrors.ErrInvalidPackage`, `oerrors.ErrMissingRequiredField` and `*oerrors.UnresolvedDemandsError` keep `Stalled=True` and the 30-minute recheck, with reason `ResolutionFailed`. On the ModulePackage path `ErrWrongKind` keeps its own `UnsupportedKind` reason, as today.
- **The string matchers are deleted.** `isResolutionError` (ModuleInstance) and `isResolutionErrorMsg` (ModulePackage) go, and `renderFailureReason` loses its string-fallback parameter: a render error is classified by its type or sentinel, never by its words. One consequence is visible: a failed `Kernel.SynthesizeInstance` (`synthesizing release: ...`) no longer matches text, so it reports `RenderFailed` instead of `ResolutionFailed`. It stays Stalled on the 30-minute recheck, as today.
- **Tests and docs.** The reconcile-level classifier tables lose the string-fallback rows and gain the transient and terminal acquire rows; the envtest stub `resolutionErrorRenderer` (which returns the dead `loading synthesized release` text) is replaced by an `ErrAcquire` stub, and envtest specs cover the transient retry on both kinds. `docs/RENDERING.md` and `docs/site/diagnostics/operator-conditions.md` describe the new split.

## Classification

**PATCH** (pre-GA; ships as the next `-beta.N` under a `fix` commit). No API type, field, CRD schema or reason constant changes. The behaviour change is confined to failing objects. An untyped acquire or load failure moves from Stalled (30m) to a transient `ResolutionFailed` on the bounded backoff (5m cap). A ModuleInstance whose acquisition fails without a typed cause changes reason from `RenderFailed` to `ResolutionFailed`. A failed synthesis changes reason from `ResolutionFailed` to `RenderFailed`. Complexity (Principle VII): one sentinel, one wrapper and one shared predicate replace two string matchers, so the change removes more classification logic than it adds.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `reconcile-backoff`: a new requirement makes an untyped acquire or load failure a `FailedTransient` outcome on the bounded backoff for both kinds, keeps the typed terminal causes stalled, and forbids classifying a render error by its message text.
- `module-instance-synthesis`: "Status reporting" says a module that cannot be acquired without a typed terminal cause is `ResolutionFailed` and not stalled; "End-to-end release scenarios" changes the not-found scenario to retry on the backoff.
- `modulepackage-artifact-loading`: "CUE evaluation with registry resolution" says a dependency that cannot be resolved is a transient `ResolutionFailed`, and an evaluation error at render stays `RenderFailed` and stalled.

## Impact

- `internal/render/kernel_module_renderer.go`, `internal/render/kernel_package_renderer.go` (the sentinel and its two wrap sites).
- `internal/reconcile/resolution.go` (the shared predicate; `renderFailureReason` without its fallback parameter), `internal/reconcile/moduleinstance.go` (`classifyRenderError`; `isResolutionError` deleted), `internal/reconcile/modulepackage.go` (`renderModulePackage`, `renderErrorReason`; `isResolutionErrorMsg` deleted).
- Tests: `internal/reconcile/resolution_test.go`, `internal/controller/testhelpers_test.go`, `internal/controller/moduleinstance_reconcile_test.go`, `internal/controller/modulepackage_controller_test.go`.
- Docs: `docs/RENDERING.md`, `docs/site/diagnostics/operator-conditions.md`.
- Downstream: none. No library pin, no cli change. Task d1's typed library errors will later narrow which untyped failures are retried; this change leaves one predicate for d1 to extend.
- No enhancement decision backs this change, so no `enhancement.yaml`.
