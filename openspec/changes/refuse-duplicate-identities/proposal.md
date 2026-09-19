## Why

Two rendered objects with one apply identity reach Flux as two writes to one object, and the last write wins silently: nothing in the render path or the apply path notices. Enhancement 0015 D15 names the case that makes this a decision rather than a nicety: a provider module shipping two `transformer-registration` components renders two cluster-scoped `TransformerRegistration` objects under one instance-derived name (D12), so the claim the operator judges is whichever component was applied last, and the other vanishes without a verdict. D15 says a second registration is refused loudly, naming both carrying components, before apply. Library `v1.0.0-alpha.33` ships the detector both runtimes call (`opm/helper/objectset`, released 2026-09-19); this change is the refusal in the runtime that applies to the cluster, which is where "before apply" is decided.

## What Changes

- **Library pin to `v1.0.0-alpha.33`.** The first release carrying `opm/helper/objectset`.
- **The render adapter refuses duplicates.** `resultFromRender`, the one adapter both renderers (ModuleInstance and ModulePackage) pass through, calls `objectset.Duplicates` on the kernel's compiled objects before building resources and inventory entries, and returns `*objectset.DuplicateIdentitiesError` when any identity is shared. Nothing reaches apply: no resource, no inventory entry, no digest.
- **A reason of its own.** `DuplicateIdentities` joins the Ready reasons: `Ready=False`, `Stalled=True`, the library's message verbatim (each identity once, every producing component and transformer), a Warning event on transition, requeue on the stalled interval. Both render-error classifiers route the typed error ahead of their string fallbacks, so the two reconcile loops cannot drift. Distinct from `RenderFailed` because the fix differs: the module author removes or renames a component, and nothing about the platform or the transformers is at fault.
- **Docs.** `docs/RENDERING.md`'s Ready-reason table gains the row, and the render section says the adapter checks identities before apply.
- **Not in this change.** Deduplicating or arbitrating between producers: two producers for one object is an authoring error (D15's "forbid, one per module"). A CLI refusal: the cli's sibling change, claiming nothing. An end-to-end fixture module with two registrations: the fixtures pin a catalog build that predates the registration contract, and the refusal is a pure function of the kernel's output, exercised without a registry (design.md).

## Impact

- **API types:** none. `PlatformStatus`, `ModuleInstanceStatus` and `ModulePackageStatus` are unchanged; a new reason string needs no schema change. `dist/install.yaml` and the CRDs do not regenerate.
- **Internal packages:** `internal/status/conditions.go` gains `DuplicateIdentitiesReason`; `internal/render/kernel_module_renderer.go` (`resultFromRender`) gains the check; `internal/reconcile/resolution.go` (`renderFailureReason`) gains the typed arm.
- **Controllers:** neither reconciler changes shape; each already turns a classified render error into its condition, event and requeue.
- **Tests:** unit tests over `resultFromRender` with hand-built `kernel.RenderResult` values (the `warnings_test.go` pattern), a classifier test for the new arm, and the existing registry-backed specs unchanged (the fixtures render distinct identities).
- **Downstream consumers:** the cli reads none of this. A module that trips the refusal was already losing an object at apply; the change is the refusal arriving where D15 puts it.
- **SemVer:** MINOR. One additive Ready reason, no field or existing reason changes.
- **Complexity (Principle VII):** one helper call and one classifier arm. Justified by D15 and by the class of bug it closes: any two components rendering one object name, not only registrations.
- **Release:** `feat`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `kernel-module-renderer`: the "Adapt compiled output to operator resources" requirement gains the duplicate-identity refusal as the first step of adaptation, before any resource or inventory entry is built.
- `modulepackage-kernel-rendering`: the render-classification requirement gains `DuplicateIdentities` as the reason for a refused duplicate, beside `ResolutionFailed`, `SkewRefused` and `RenderFailed`.
- `status-conditions`: the "Reason constants" requirement lists `DuplicateIdentities`.
