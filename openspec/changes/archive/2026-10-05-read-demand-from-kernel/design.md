## Context

Line numbers are at `2fe3c66` (opm-operator#249); re-check them before editing.

**Demand.** `internal/render/demand.go:43` `declaredContracts(inst)` walks `inst.Package` at `schema.Components` and reads each component's `#resources` and `#traits` keys (`cue.Def`), sorting and deduplicating them. It returns an empty slice for an instance with no components, and nothing for a component without `#resources` or `#traits`. It is called in `KernelModuleRenderer.RenderModule` (`kernel_module_renderer.go:125-128`) and `KernelPackageRenderer.Render` (`kernel_package_renderer.go:86-93`), each after a successful `Kernel.Render`, and the result is passed to `resultFromRender(out, identity, contracts)` (`kernel_module_renderer.go:233`). The reconciler copies `RenderResult.RequiredContracts` to `status.requiredContracts` after a successful render (`internal/reconcile/moduleinstance.go:463`); the removal guard (`internal/controller/transformerregistration_controller.go`, `transformerregistration_dependents.go`) and the shrink refusal (`internal/shrink/shrink.go`) read the status field. ModulePackage status has no demand field; its renderer fills the result field so both paths agree.

Library `v1.0.0-beta.6` (`opm/kernel/render.go:216-234`, `render_decode.go:96-99`): `RenderDiagnostics.RequiredContracts` is every `#resources` and `#traits` key of every component, sorted in byte order and deduplicated, an omitted component under `RenderInput.SkipUnprovided` included (the operator does not set it), not narrowed to provider-fulfilled contracts, empty and never nil. It is set on every `RenderResult` and on every `*RenderError`; a plain error carries none. It fails closed: a component whose `#resources` is missing or does not evaluate fails the render with a plain error.

**Values.** `KernelModuleRenderer.synthesize` runs `r.Kernel.ValidateConfigDetailed(mod.ConfigSchema(), sources)` before `Kernel.SynthesizeInstance`, with `requireConcrete` set. This change leaves it as it is (D3).

Reconcile phase impact: Render only. Source, Apply, Prune and Inventory are untouched. Status: `status.requiredContracts` keeps its writer and its rule; no condition, reason or message changes.

## Goals / Non-Goals

**Goals:**

- No operator code computes contract demand; both renderers report the kernel's list (0013:D24).
- No user-visible change: the demand pins pass unchanged, and every failure reads as before.

**Non-Goals:**

- Writing the demand on a failed render (D2).
- Deleting the values pre-validate (D3).
- Any change to `ModuleInstanceStatus`, the CRDs, the removal guard or the shrink refusal.

## Research & Decisions

### D1. `resultFromRender` reads the demand off the render output

**Context**: The demand used to come from a caller-supplied parameter because the kernel reported only matched pairs.
**Explored**: Keep the parameter and pass `out.Diagnostics.RequiredContracts` at both call sites; or read it inside `resultFromRender`.
**Decision**: `resultFromRender(out, identity)` reads `out.Diagnostics.RequiredContracts` itself; the `contracts` parameter goes. It MUST normalise a nil slice to an empty, non-nil one, so a status write never alternates between absent and empty even if the library's own normalisation changed. The doc comment on `RenderResult.RequiredContracts` (`internal/render/module.go:44-53`) says the list is the kernel's, computed in the render build from the instance alone and not narrowed by the platform.
**Rationale**: Both renderers already go through `resultFromRender`; reading the field there is the single place, and it makes "both renderers fill it the same way" true by construction.

The API doc of `ModuleInstanceStatus.RequiredContracts` (`api/v1alpha1/moduleinstance_types.go`, "read off the synthesized instance's components") stays unchanged on purpose. It is still true of the kernel's list, which is read off the same components inside the render build, and any edit there regenerates the CRD and the operator module's `zz_generated_crds.cue`, which cuts a module release from a refactor.

```go
return &RenderResult{
	// ...
	RequiredContracts: demandOf(out.Diagnostics), // nil -> []string{}
	PlatformIdentity:  identity.String(),
}, nil
```

### D2. A failed render still leaves the previous demand

**Context**: The kernel also sets the demand on a `*RenderError`, so the operator could write it on a refused render.
**Explored**: (A) read `rerr.Diagnostics.RequiredContracts` and write it on a refusal; (B) keep the current rule.
**Decision**: (B). This change does not read the demand from a `*RenderError`. `reconcile-loop-assembly` requires that a reconcile that does not render successfully leaves `status.requiredContracts` at its previous value.
**Rationale**: The rule is deliberate: the last successful render is what the cluster holds, and its demand is what the guards protect. A refused render after a spec edit could report a smaller demand than the applied objects still depend on, and writing it would let a provider be removed under them. Changing that is a behaviour change of the guard, outside a refactor. The source of the demand moves; when it is written does not.

### D3. The values pre-validate stays

**Context**: The plan also deleted the `ValidateConfigDetailed` pre-validate, since library#197 makes `SynthesizeInstance` attribute a values conflict to the values source. Measured against library `v1.0.0-beta.6`, synthesis checks values without concreteness, and `processInstance` checks concreteness only of what the built instance reads. Deleting the pre-validate therefore lets a ModuleInstance render with a required `#config` value left unset that no component reads, where today it stalls; and an unset value that a component reads would be reported at the component path instead of at `#config.<field>`.
**Explored**: (A) delete it and accept the loosening as a release note; (B) keep it until the library decides.
**Decision**: (B). The pre-validate, its wording and its `cueFindings` helper stay unchanged; a comment on the call names library#211, which asks whether the kernel refuses an unset required `#config` value. The operator deletes the pre-validate in a change of its own once that is settled.
**Rationale**: A refactor ships no behaviour change. Whether an unset required value is refused is the kernel's call, and moving it there first keeps the operator from loosening what it accepts in the meantime.

## Sections

1. Demand from the kernel (D1, D2): delete the walk; the demand pins pass unchanged.
2. Verification: the Kind e2e and the full gates.

Section 1 ends green and leaves `main` releasable on its own.

## Risks / Trade-offs

- [The kernel's list differs from the walk on a fixture] → The integration table in `test/integration/reconcile/kernel_module_renderer_test.go` pins the exact FQN sets the walk produced for `hello`, `hello_web` and the other fixtures, and `backup_fixture_test.go:144` pins a trait demand; section 1 runs them with the registry forced (`OPM_TEST_REGISTRY_FORCE=1`) so a skip cannot pass for green. The library's own parity test compares its list with this walk.
- [A component without `#resources` now fails the render] → Already live: library `v1.0.0-beta.6`'s build fails closed before the operator's walk could run. Nothing in this change alters it.
- [Merge conflict with the render-timeout change] → The one shared file is `internal/reconcile/moduleinstance.go`, where this change edits only the comment above `mi.Status.RequiredContracts`; whichever merges second merges `main`.

## Migration Plan

No migration. The status field and its write rule are unchanged; a ModuleInstance's next successful render writes the kernel's list, which equals the walk's for every pinned fixture.

## Open Questions

None blocking. Whether the kernel refuses an unset required `#config` value is library#211; the operator's pre-validate waits on it.
