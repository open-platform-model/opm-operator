## Why

The operator does two jobs itself that library `v1.0.0-beta.6` now does in the kernel.

- **Contract demand.** `internal/render/demand.go` walks the synthesized instance's `components` and collects every `#resources` and `#traits` key, so the reconciler can record `status.requiredContracts` for the registration removal guard and the shrink refusal (0015:D3/D16). The kernel now computes the same list inside the render build and returns it as `RenderResult.Diagnostics.RequiredContracts`, sorted, deduplicated and never nil (0013:D24, library#194). 0013:D24 says frontends read the demand from there instead of walking the instance: the kernel is the one party that sees components that exist only inside the render, and a second walk in the operator is kernel knowledge duplicated in a frontend. The kernel's list also fails closed, where the operator's walk read a component without `#resources` as no demand.
- **Values pre-validate.** `KernelModuleRenderer.synthesize` checks `spec.values` against the module's `#config` through `Kernel.ValidateConfigDetailed` before it calls `Kernel.SynthesizeInstance`. It was added because synthesis used to report a values conflict at the component that consumed the value, with no position in `spec.values`. Since library#197, `SynthesizeInstance` attributes a values conflict to the values source itself, at that source's own positions, on both its success and its failure path. The pre-validate now compiles and checks the same values a second time.

## What Changes

- **Demand comes from the kernel.** `internal/render/demand.go` is deleted. `resultFromRender` fills `RenderResult.RequiredContracts` from `out.Diagnostics.RequiredContracts` for both renderers, keeping an empty, non-nil slice when the kernel returns none. The `declaredContracts` calls and their `reading the instance's contract demand` error wraps go from `KernelModuleRenderer.RenderModule` and `KernelPackageRenderer.Render`. The status field, its shape, and when the reconciler writes it do not change.
- **The values pre-validate goes.** The `Kernel.ValidateConfigDetailed` call and its `validating values against the module's #config` wrapper are deleted. A values error now comes from `Kernel.SynthesizeInstance`. The renderer words a synthesis failure as `synthesizing release: ` followed by the library's message, with each CUE finding listed with its source positions, so a values error still names `spec.values` and its line and column. The `cueFindings` helper stays, as the wording of a synthesis failure, and lists at most ten findings followed by `; and N more`, so a large module cannot push a condition message past the CRD's length limit. A synthesis failure whose chain holds the library's typed registry fetch failure keeps the library's message unchanged, and the error chain is kept, so the typed classification of a registry fetch failure during synthesis is unchanged.
- **Tests pin the user-visible result before and after.** The wording and condition reason of a `spec.values` violation, and the outcome of a required `#config` value left unset (read by a component, and read by nothing), are pinned through `KernelModuleRenderer` and the ModuleInstance reconciler against the current code first, then moved to the new outcome in the same section; tasks.md 2.3 records the before and after messages the tests printed. A new published test fixture, `required_values`, carries the two required fields. The kernel-list demand pins (the exact per-fixture FQN table in `test/integration/reconcile/kernel_module_renderer_test.go`, `test/integration/reconcile/backup_fixture_test.go`, and a new assertion on `KernelPackageRenderer`'s list for the `hello` package) must pass unchanged on the kernel's list; `internal/controller/moduleinstance_demand_test.go` pins the reconciler's write rule on stub renders and keeps passing.
- **Specs and docs.** `kernel-module-renderer` gains a requirement for where the demand comes from and amends the values requirement; `module-instance-synthesis` fixes its stale "Invalid values" scenario. `docs/site/diagnostics/operator-conditions.md` drops the `validating values against the module's #config` message from the `RenderFailed` row.

## Release notes (user-visible changes)

The PR title is `refactor:`. Its body MUST name these, because what a failing ModuleInstance reports changes:

1. **The message of a `spec.values` violation changes; its condition does not.** It still reads `Ready=False`, `Stalled=True`, reason `RenderFailed`, and still lists each finding with its positions in `spec.values`. What changes is the frame and the path: `validating values against the module's #config: #config.message: conflicting values 42 and string (... spec.values:1:1, spec.values:1:13)` becomes `synthesizing release: Kernel.SynthesizeInstance: instance "<name>": #module.#config.message: conflicting values 42 and string (... spec.values:1:1, spec.values:1:13)`. A field `#config` does not allow reads `field not allowed (spec.values:1:2)` under the same new frame.
2. **A required `#config` value left unset is reported where a component reads it.** The pre-validate refused it as `#config.<field>: incomplete value <type>` at the field's position in the module. Synthesis attributes a values conflict to its source but does not check values for concreteness on that path (library#197); concreteness is checked on the whole built instance. The error therefore names the component path that reads the value, for example `not fully concrete: components.required.spec.configMaps.required.data.message: incomplete value string`, with positions in the catalog and core files rather than in the module. Condition and reason are unchanged (`RenderFailed`, `Stalled=True`).
3. **A required `#config` value that no component reads is no longer refused.** Nothing in the build consumes it, so synthesis succeeds, and the render does not depend on it. Before, the pre-validate stalled such an instance. Section 2 confirms this through a full render before the release note is final.

Every other synthesis failure (a build error inside the module) gains source positions in its message, in the same `finding (positions)` form, at most ten findings followed by `; and N more`; its frame and condition are unchanged. A registry fetch failure during synthesis reads exactly as before.

## Classification

**PATCH** after GA (a refactor with a changed failure message); pre-GA it ships as the next `-beta.N` under a `refactor:` title. No API type, CRD schema, status field or reason constant changes. Complexity (Principle VII): two operator-side computations are deleted; nothing is added beyond the error wording helper the pre-validate already had.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `kernel-module-renderer`: "Render a ModuleRelease through the kernel" says the values check is synthesis's, with no operator pre-validate, and that the error lists findings with their `spec.values` positions; a new requirement, "The render reports the instance's contract demand from the kernel", says both renderers report the kernel's list and no operator walk exists.
- `module-instance-synthesis`: "End-to-end release scenarios" replaces the stale `ParseModuleRelease` wording of "Invalid values" with the synthesis refusal.

`reconcile-loop-assembly` ("The instance's contract demand is recorded on its status") is unchanged: the reconciler writes what the render reports on a successful render and keeps the previous value on a failed one. The kernel also sets the demand on a `*RenderError`; this change does not read it there (design.md D2).

## Impact

- Code: `internal/render/demand.go` (deleted), `internal/render/kernel_module_renderer.go`, `internal/render/kernel_package_renderer.go`, `internal/render/module.go` (doc comment).
- Tests: `internal/render/kernel_module_renderer_test.go`, `internal/reconcile/resolution_test.go`, `test/integration/reconcile/kernel_module_renderer_test.go`, `test/integration/reconcile/kernel_package_renderer_test.go`; the demand pins above run unchanged.
- Fixtures: a new module fixture `test/fixtures/modules/required_values` (`testing.opmodel.dev/modules/operator/required_values@v0`, version `0.0.1`), published through the fixtures pipeline like the others; PR CI seeds it from the tree, and `.tasks/cascade/test.sh`'s golden list gains its identity file because `task deps:cascade` advances every fixture.
- Docs: `docs/site/diagnostics/operator-conditions.md`.
- Dependencies: none. opm-operator#248 already moved the operator to library `v1.0.0-beta.6`, which carries both kernel changes; this change builds on opm-operator#249 and changes no pin.
- Merge order: the parallel render-timeout change (`bound-render-time`) does not edit `internal/render/kernel_module_renderer.go`; it edits `docs/site/diagnostics/operator-conditions.md` (a `RenderTimedOut` row beside the `ResolutionFailed` and `RenderFailed` rows this change edits), which is the one shared file. Whichever merges second merges `main` and resolves. A render timeout during synthesis then reads `synthesizing release: context deadline exceeded`, and `synthesisError.Unwrap` keeps `errors.Is(err, context.DeadlineExceeded)` true, so that change's classification is unaffected.
- Downstream: none. The cli reads its own demand and values errors.
- Enhancement: `enhancement.yaml` declares 0013 with no decision claimed. 0013:D24's requirement (a synthesised secret component counted in the demand) is the kernel's to satisfy and has no synthesised component to count yet; this change moves the operator onto the kernel's list, which delivers no decision by itself.
