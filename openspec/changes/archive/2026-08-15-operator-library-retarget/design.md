# Design — operator-library-retarget

## Overview

One atomic crossing, staged so every commit before and after the flip is independently green: the library bump, the CRD subscription reshape, and the controller's input mapping move together (they are mutually compile-dependent); samples, tests, and the digest pin land around them. The companion change `examples-fleet-core-v2` moves the CUE fixture fleet and the publish pipeline.

## Research & Decisions

### The bump, the CRD, and the wiring are one change, not two

**Context**: the exploration initially proposed splitting the CRD change from the retarget.
**Explored**: compile feasibility of each ordering. At alpha.13, `synth.FilterSpec` does not exist, so `platform_controller.go`'s `Filter` mapping cannot compile. At alpha.8, `synth.SubscriptionSpec` has no `Version` field, so the new mapping cannot compile. A CRD-first change that adds `Version` unwired while keeping `Filter` mapped doubles the regeneration churn and still leaves the `Filter` deletion riding the bump.
**Decision**: one change; the flip commit carries `go.mod`, the API type, and the controller mapping. The library's own crossing (`library-core-retarget`: "the crossing is atomic — staged pre-steps, one flip commit") is the precedent.
**Rationale**: Principle VIII wants small *reviewable* steps, not steps that cannot build. Staging inside the change keeps commits green; the change boundary sits where independence is real (the fixture fleet).

### `Version`: CRD-required vs CRD-optional — decided by measurement

**Framing first**: a versionless subscription is **always rejected** on the v2 line, in every posture this section considers — the library refuses it at platform synthesis (`ErrSubscriptionMissingVersion`) before any registry I/O, and nothing versionless ever materializes. The measurement below selects only the *rejecting actor and moment*: the API server at admission (`required`, the preferred outcome) or the reconciler one loop later (`optional`, surfacing as `MaterializeFailed` naming the subscription path). There is no posture in which a version-free Platform functions.

**Context**: the stored `cluster` Platform singleton lacks `version` under `spec.registry.*`. Structural pruning removes the stale `filter` only on the next **spec** write — which the operator never performs (it patches status exclusively, on every reconcile), and custom-resource validation runs against the whole object on every write **including status-subresource patches**. If CRD-level `required` invalidates status patches against that stored object, the singleton hot-loops on patch errors with no self-heal path — the operator cannot fix it because fixing it requires a spec write it is designed never to make.
**Explored**: apiextensions validation ratcheting semantics (unchanged values are not re-validated) SHOULD protect a status-subresource patch that does not touch spec — but whether ratcheting covers a *missing required field inside a map value* is exactly the kind of edge worth measuring, and the operator's envtest rig (`k8s.io/api v0.36.2` line) is well past ratcheting GA.
**Decision**: an envtest verification is a **task, and its outcome selects the marker**:
1. Store a Platform under the old schema (with `filter`, without `version`), swap in the new CRD, patch status. If the patch succeeds → ship `+required` + `MinLength=1` (belt-and-braces over the library's own refusal).
2. If it fails → ship CRD-optional, and rely on `synth.ErrSubscriptionMissingVersion` (the library refuses a versionless subscription before any I/O) to surface the spec error as `MaterializeFailed` naming the subscription path.
**Rationale**: both postures are safe against *new* objects; the difference is only the stored-singleton migration path. Measuring costs one test; guessing wrong costs a hot-looping fleet. Either way the reconciler behavior is identical for valid specs. The CLI has been told to keep its CR reads tolerant of both missing-`version` and legacy-`filter` objects regardless.

### Sample pin: `2.0.0-alpha.3`, aligned with the CLI seed

**Context**: `config/samples/opmodel.dev_v1alpha1_platform.yaml` is applied verbatim by four e2e specs; D14 requires one published build, and the subscription key must carry `@vN` (`#ModulePathType`; the library's `majorAgrees` check also enforces key-major vs version-major).
**Decision**: `"opmodel.dev/catalogs/opm@v2": {version: "2.0.0-alpha.3"}` with a comment marking the pin load-bearing (bumped as an ordinary fixture update when the catalog releases). Same value as the CLI's `DefaultPlatformTemplate` seed and the library's own `opm_platform` fixture — three pins, one value, each carrying the same comment convention.

### New failure classes ride existing routing — no feature code

**Context**: at alpha.13, three new failure classes reach the reconcilers: `oerrors.IdentityError` from module acquire (bare) and from catalog materialize (wrapped in `*MaterializeError`), `synth.ErrSubscriptionMissingVersion` from platform synthesis, and `*oerrors.UnresolvedDemandsError` (possibly joined with `*compile.UnmatchedComponentsError`) from compile.
**Explored**: current routing. `platform_controller.go` already routes `*oerrors.MaterializeError` and prints kind/subscription/version/cause — catalog-side identity errors and version-not-published surface adequately there, on `MaterializeFailedReason` with the stalled recheck interval (correct: both can heal externally). Module-acquire `IdentityError` and unresolved demands land on `RenderFailedReason` via `classifyRenderError`, whose `isResolutionError` string-match misses both; `ResolutionFailedReason` exists unused.
**Decision**: change nothing. The slice charter (0010: D18 rejected operator-side adoption mechanisms; 06-operational: "no feature change of any kind") holds for everything except the unmappable CRD field. Typed routing for `IdentityError`/`UnresolvedDemandsError` onto `ResolutionFailedReason` is recorded here as a **follow-up candidate**, not done.
**Consequence stated honestly (fleet-visible)**: instances whose renders silently under-delivered (unhandled load-bearing traits, undemandable resources — previously discarded warnings: `RenderResult.Warnings` is read by nothing) now stall as `RenderFailed`. That is D28 behaving as designed; the operator's docs comment at `internal/render/module.go:21` ("e.g., unhandled traits") is updated to say only *optional* traits are warnings.

### The digest freeze becomes a test

**Decision**: `internal/status/digests_test.go` gains a golden pin: `ModuleSourceDigest("opmodel.dev/modules/test/podinfo@v0", "v0.1.3")` equals the literal sha256 string, with a comment naming the CLI's `sourceDigest` as the byte-identical peer that must move in lockstep or cross-actor no-op detection silently breaks. The CLI slice adds the mirror test.

### One-time `RenderDigest` movement is operational, not code

D36 removed the matcher's read of `metadata.labels`; when rendered output loses the `core.opmodel.dev/workload-type` label (a catalog-side change), every instance's `RenderDigest` moves once and the fleet performs one non-no-op reconcile. Nothing to code here; noted for the release notes.

## Reconcile phase impact (Source, Render, Apply, Prune, Status)

- **Source**: none (Flux artifact path untouched; `opm/helper/loader/file` unchanged in the library diff).
- **Render**: `Compile` MAY now fail with `*oerrors.UnresolvedDemandsError` (joined with `*compile.UnmatchedComponentsError`); module acquire MAY fail with `oerrors.IdentityError`. Both classify as `RenderFailed` → `MarkStalled`, Warning event, stalled recheck. `Warnings` narrows to effectively-optional traits only.
- **Apply / Prune**: none.
- **Status**: `Platform.status` unchanged in shape; materialize failures gain the new message classes through existing `MaterializeError` routing. `ModuleInstance` conditions unchanged in vocabulary; `RenderFailed` frequency may rise (D28).

## Technical Notes

### Flip-commit inventory

`go.mod`/`go.sum`; `api/v1alpha1/platform_types.go` (delete `SubscriptionFilter`, swap `Subscription.Filter` → `Version` with the measured marker); `task dev:manifests dev:generate` outputs (`config/crd/bases/opmodel.dev_platforms.yaml`, `zz_generated.deepcopy.go`); `task operator:installer` (`dist/install.yaml`); `internal/controller/platform_controller.go:242-252` mapping.

### Test inventory

- `synth.FilterSpec` deletions: `test/integration/reconcile/{kernel_module_renderer_test.go:93, kernel_package_renderer_test.go:106, example_modules_test.go:68}` → `SubscriptionSpec{Version: …}` with `OPM_TEST_CATALOG_RANGE` renamed to an exact-version knob (`OPM_TEST_CATALOG_VERSION`, default `2.0.0-alpha.3`).
- `internal/controller/platform_test.go:52` `SubscriptionFilter` literal → `Version`.
- Versionless `Subscription{}` at `internal/controller/platform_controller_test.go:157,194` and `test/integration/reconcile/platform_recovery_test.go:109` → author a version; add one test asserting the missing-version refusal surfaces as `MaterializeFailed` naming the subscription path.
- `acquire_test.go:70` parent-path assertion: keep against the v1-authored published fixture in this change (the library's tolerant fallback accepts it); the flip to the full-address assertion lands in `examples-fleet-core-v2` with the republished fixture.
- Envtest ratcheting verification per the decision above.
- Digest golden pin.

### Interim-red surface between the two changes

Registry-backed integration specs (`example_modules`, `kernel_*_renderer`) and the e2e fixture specs render v1-authored fixtures against a v2 platform/catalog → `no matching transformer`. Both tiers skip in PR CI (the integration tier requires a localhost registry mapping CI never sets; e2e requires creds), so the redness is local-only and closes when `examples-fleet-core-v2` lands. The release is cut after both.
