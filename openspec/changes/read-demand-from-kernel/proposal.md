## Why

`internal/render/demand.go` walks the synthesized instance's `components` and collects every `#resources` and `#traits` key, so the reconciler can record `status.requiredContracts` for the registration removal guard and the shrink refusal (0015:D3/D16). Library `v1.0.0-beta.6` computes the same list inside the render build and returns it as `RenderResult.Diagnostics.RequiredContracts`, sorted, deduplicated and never nil (0013:D24, library#194). 0013:D24 says frontends read the demand from there instead of walking the instance: the kernel is the one party that sees components that exist only inside the render, and a second walk in the operator is kernel knowledge duplicated in a frontend. The kernel's list also fails closed, where the operator's walk read a component without `#resources` as no demand.

## What Changes

- **Demand comes from the kernel.** `internal/render/demand.go` is deleted. `resultFromRender` fills `RenderResult.RequiredContracts` from `out.Diagnostics.RequiredContracts` for both renderers, keeping an empty, non-nil slice when the kernel returns none. The `declaredContracts` calls and their `reading the instance's contract demand` error wraps go from `KernelModuleRenderer.RenderModule` and `KernelPackageRenderer.Render`. The status field, its shape, and when the reconciler writes it do not change.
- **Tests.** The kernel-list demand pins (the exact per-fixture FQN table in `test/integration/reconcile/kernel_module_renderer_test.go`, `test/integration/reconcile/backup_fixture_test.go`, and a new assertion on `KernelPackageRenderer`'s list for the `hello` package) pass unchanged on the kernel's list; `internal/controller/moduleinstance_demand_test.go` pins the reconciler's write rule on stub renders and keeps passing.
- **Specs.** `kernel-module-renderer` gains a requirement for where the demand comes from. `module-instance-synthesis` fixes its stale "Invalid values" scenario, which still names `ParseModuleRelease`, to the refusal the operator reports today.

### The values pre-validate stays

`KernelModuleRenderer.synthesize` keeps its `Kernel.ValidateConfigDetailed` check of `spec.values` against the module's `#config`. Library#197 made `SynthesizeInstance` attribute a values conflict to its source, but synthesis checks values without concreteness, so deleting the pre-validate would stop refusing a required `#config` value left unset that no component reads. That is a behaviour change, not a refactor. The pre-validate stays until library#211 settles whether the kernel refuses an unset required `#config` value itself; the operator deletes it in a change of its own after that.

## Release notes (user-visible changes)

None. No condition, reason, message or status value changes.

## Classification

**PATCH** after GA; pre-GA it ships as the next `-beta.N` under a `refactor:` title. No API type, CRD schema, status field, reason constant or message changes. Complexity (Principle VII): one operator-side computation is deleted; nothing is added.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `kernel-module-renderer`: a new requirement, "The render reports the instance's contract demand from the kernel", says both renderers report the kernel's list and no operator walk exists.
- `module-instance-synthesis`: "End-to-end release scenarios" replaces the stale `ParseModuleRelease` wording of "Invalid values" with the refusal the renderer reports.

`reconcile-loop-assembly` ("The instance's contract demand is recorded on its status") is unchanged: the reconciler writes what the render reports on a successful render and keeps the previous value on a failed one. The kernel also sets the demand on a `*RenderError`; this change does not read it there (design.md D2).

## Impact

- Code: `internal/render/demand.go` (deleted), `internal/render/kernel_module_renderer.go`, `internal/render/kernel_package_renderer.go`, `internal/render/module.go` and `internal/reconcile/moduleinstance.go` (doc comments).
- Tests: `internal/render/kernel_module_renderer_test.go`, `test/integration/reconcile/kernel_package_renderer_test.go`; the demand pins above run unchanged.
- Dependencies: none. opm-operator#248 already moved the operator to library `v1.0.0-beta.6`; this change builds on opm-operator#249 and changes no pin.
- Merge order: the parallel render-timeout change (`bound-render-time`) shares one file with this change, `internal/reconcile/moduleinstance.go`, where this change edits only the comment above `mi.Status.RequiredContracts`. The other two files an earlier draft of this change shared with it (`docs/site/diagnostics/operator-conditions.md` and `internal/reconcile/resolution_test.go`) are no longer touched, because the values half was dropped. Whichever merges second merges `main`.
- Downstream: none. The cli reads its own demand.
- Enhancement: `enhancement.yaml` declares 0013 with no decision claimed. 0013:D24's requirement (a synthesised secret component counted in the demand) is the kernel's to satisfy and has no synthesised component to count yet; this change moves the operator onto the kernel's list, which delivers no decision by itself.
