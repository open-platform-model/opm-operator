## Why

Every ModuleInstance and ModulePackage reconcile renders, even when nothing it renders from has changed. A render is the operator's most expensive step: one cert-manager-sized render peaks at about 2 GiB RSS, and the shared render slots (`--max-concurrent-renders`) serialise every other render behind it. Most renders find nothing to do. A ModulePackage renders on every `spec.interval` (default 5 minutes) and nearly always ends `NoOp`. An operator restart lists every object and renders all of them, and because the in-memory platform store is still empty, each of those renders first fails `PlatformNotReady` (Ready flips to False) and renders again once the platform is regenerated.

The owner settled this in the kernel-plan walkthrough (task g3, 2026-10-02): "Skip render when a pre-render input key (source, values, platform identity, operator/library version, skew) matches; drift via throttled re-render (at most every N minutes), works before g1. Additive v1alpha1 status field holds the input key. After j1." j1 has merged (opm-operator#191): `Platform.status.packageIdentity` is the field that names the pin set a workload renders against, and the key uses that field.

## What Changes

- **A render input key.** A digest over the inputs a render is a function of: the source digest (a ModuleInstance's `path@version` digest, a ModulePackage's Flux artifact digest), the config digest (`spec.values`; empty for a ModulePackage), the platform package identity (`packageIdentity`, the pin-set field the Platform watch predicate reads), the resolved catalog skew policy, the operator version and the library version. A key with an empty part is incomplete, and an incomplete key is never recorded and never matches.
- **A new status field, `status.lastAppliedInputs`, on ModuleInstance and ModulePackage.** It holds the key (`digest`) and the time of the render that confirmed it (`renderedAt`). It is written with the inputs the render actually used (the package identity and skew policy the render leased from the platform store, not the ones read before it), on a successful apply and on a `NoOp` that rendered. A failed, refused or panicking attempt leaves it as it was, so the key moves only when the cluster holds what those inputs produce.
- **The render reports its skew policy.** `render.RenderResult` gains `SkewPolicy` beside `PlatformIdentity`; both kernel renderers fill it from the platform record they leased.
- **The skip.** Before it renders, each reconcile computes the key from the object's spec or resolved source, `Platform.status.packageIdentity`, the Platform's resolved `spec.skewPolicy`, and the running operator and library versions. It skips the render (no render slot, no platform lease, no artifact fetch, no drift detection, no status patch) only when all of these hold: the key is complete and equals `status.lastAppliedInputs.digest`; `renderedAt` is younger than the drift render interval; the object is `Ready=True` with reason `ReconciliationSucceeded`; `status.observedGeneration` equals `metadata.generation`; and, for a ModulePackage, the resolved source (revision, digest, URL) equals `status.source`, so a new Flux revision with an unchanged digest still renders once and records the revision. Otherwise it renders exactly as today. A skipped ModuleInstance reconcile returns with no requeue; a skipped ModulePackage reconcile requeues on its `spec.interval`, as a `NoOp` does.
- **Throttled re-render for drift: `--drift-render-interval`** (default `30m`). A reconcile whose inputs are unchanged still renders once the last confirming render is older than this, so drift detection keeps running, at most once per interval per object. `0` disables the skip and the record on a `NoOp`: every reconcile renders, and a `NoOp` writes what it wrote before this change (an apply still records the key beside the other `lastApplied*` fields). A negative value is refused at startup.
- **An upgrade always renders.** The operator and library versions are part of the key, so the first reconcile after either changes renders. This is the rule a later change to the stored digests relies on (the planned move of the inventory and render digests to the library's `opm/k8s/inventory` package changes every stored digest once, and must apply once). Its basis is that every operator release changes `version.Version`; a development image built from `main` without a version bump keeps its key, so on a dev or kind cluster such a one-time apply waits up to the interval unless the manager runs with `--drift-render-interval=0`.
- **Generated files.** `config/crd/bases`, `dist/install.yaml` and the operator module's generated CRD data are regenerated with the repository tasks, never by hand.

## Classification

**MINOR** after GA: an additive optional status field on `v1alpha1`, an additive `RenderResult` field and a new flag with a default. During beta it ships as the next `1.0.0-beta.N` under a `feat` PR title. No field is removed or renamed. Behaviour visible to users: with the default, a reconcile within 30 minutes of the last confirming render of unchanged inputs no longer renders, so `Drifted` is re-evaluated at most every 30 minutes per object instead of on every reconcile, and a ModulePackage's `NoOp` event is no longer emitted on every interval. `--drift-render-interval=0` restores rendering on every reconcile.

The regenerated `modules/opm_operator/zz_generated_crds.cue` is the forced exception in `AGENTS.md`: a CRD change regenerates the module's data in the same PR, so the squash commit also lands in the module's changelog and opens a module release PR, which the module's release gate holds until an operator release carrying this `config/` reaches the module.

Complexity (Principle VII): one pure key function, one status field (a two-member struct), one flag, and one pre-render check per reconcile loop. The skip conditions are each one comparison; each exists to keep a skip from hiding something a render would have found (an input change, a stale render, a failed attempt, an unobserved spec edit, an unrecorded source revision). The alternative the owner's decision rules out is caching render output (g1, kept for later); this change stores no rendered objects.

## Capabilities

### New Capabilities

- `render-input-key`: the key's parts, when it is recorded, when a reconcile skips its render, the drift render interval, and the version rule for upgrades.

### Modified Capabilities

- `reconcile-loop-assembly`: "Status always patched" names `lastAppliedInputs` in the `NoOp` patch set and says a reconcile that skips its render patches nothing.
- `modulepackage-reconcile-loop`: "Status always patched" says the same for ModulePackage; "Reconcile triggers" says an interval requeue renders only when an input changed or the drift render interval has passed.
- `drift-detection`: "Drift runs on no-op reconciles" applies to every reconcile that renders; a skipped reconcile runs no drift detection.
- `kernel-module-renderer`: both kernel renderers report the skew policy they rendered under.

## Impact

- API: `api/v1alpha1/common_types.go` (the `RenderInputs` type), `api/v1alpha1/moduleinstance_types.go`, `api/v1alpha1/modulepackage_types.go` (the field), `api/v1alpha1/zz_generated.deepcopy.go` (regenerated).
- Key and inputs: `internal/status/digests.go` (`RenderInputKey`), `internal/version/version.go` (`Library()`), `internal/platform` (the skew resolution moves here from the Platform controller, plus the singleton name), `internal/render/module.go` and both kernel renderers (`SkewPolicy`).
- Reconcile: `internal/reconcile/moduleinstance.go`, `internal/reconcile/modulepackage.go`, a new `internal/reconcile/inputs.go` (the pre-render key, the skip test, the record helper).
- Wiring: `cmd/main.go` (`--drift-render-interval`, the versions), `internal/controller/moduleinstance_controller.go` and `modulepackage_controller.go` (pass the interval and versions), `internal/controller/platform_controller.go` (uses the moved skew resolution).
- Generated: both CRDs under `config/crd/bases/`, `dist/install.yaml`, `modules/opm_operator/zz_generated_crds.cue`.
- Docs: `docs/RENDERING.md` (a section on the skip and the flag), `docs/site/diagnostics/operator-conditions.md` (the `Drifted` row).
- Tests: `internal/status`, `internal/version`, `internal/platform`, `internal/render`, `internal/controller` (envtest), `test/integration/reconcile` (drift and the renderers), the kind e2e suite (run once, unchanged).
- Builds on #236 (the deferred-commit shape and the recovered-panic branch), #237 (the ModulePackage Platform watch predicate) and #242 (`lastAppliedVersion`): a skipped reconcile leaves `lastAppliedVersion` as it was, as its requirement already says.
- Downstream: none required. The cli does not read or write the field. While a ModuleInstance is CLI-owned the operator does not reconcile it, and the handback is a spec edit, which always renders.
- No enhancement decision backs this change (g3 is a kernel-plan walkthrough decision), so there is no `enhancement.yaml`.
