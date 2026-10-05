## Context

Line numbers are at `2fe3c66` (opm-operator#249); re-check them before editing.

**Demand.** `internal/render/demand.go:43` `declaredContracts(inst)` walks `inst.Package` at `schema.Components` and reads each component's `#resources` and `#traits` keys (`cue.Def`), sorting and deduplicating them. It returns an empty slice for an instance with no components, and nothing for a component without `#resources` or `#traits`. It is called in `KernelModuleRenderer.RenderModule` (`kernel_module_renderer.go:125-128`) and `KernelPackageRenderer.Render` (`kernel_package_renderer.go:86-93`), each after a successful `Kernel.Render`, and the result is passed to `resultFromRender(out, identity, contracts)` (`kernel_module_renderer.go:233`). The reconciler copies `RenderResult.RequiredContracts` to `status.requiredContracts` after a successful render (`internal/reconcile/moduleinstance.go:463`); the removal guard (`internal/controller/transformerregistration_controller.go`, `transformerregistration_dependents.go`) and the shrink refusal (`internal/shrink/shrink.go`) read the status field. ModulePackage status has no demand field; its renderer fills the result field so both paths agree.

Library `v1.0.0-beta.6` (`opm/kernel/render.go:216-234`, `render_decode.go:96-99`): `RenderDiagnostics.RequiredContracts` is every `#resources` and `#traits` key of every component, sorted in byte order and deduplicated, an omitted component under `RenderInput.SkipUnprovided` included (the operator does not set it), not narrowed to provider-fulfilled contracts, empty and never nil. It is set on every `RenderResult` and on every `*RenderError`; a plain error carries none. It fails closed: a component whose `#resources` is missing or does not evaluate fails the render with a plain error.

**Values.** `KernelModuleRenderer.synthesize` (`kernel_module_renderer.go:142-189`) acquires the module, loads `spec.values` (or `{}`) as one source with origin `spec.values`, runs `r.Kernel.ValidateConfigDetailed(mod.ConfigSchema(), sources)` (`:175`), and on failure returns `validating values against the module's #config: ` + `cueFindings(err)` (`:176`, a `%s`, so the chain is cut). It then calls `Kernel.SynthesizeInstance` and wraps a failure as `synthesizing release: %w` (`:186`). `cueFindings` (`:195-213`) lists each CUE finding followed by its positions.

Library `v1.0.0-beta.6` `Kernel.SynthesizeInstance` (`opm/kernel/synth.go:110-215`): on a build failure with values, it rebuilds without the values file and checks the compiled values against that build's `#config` without concreteness; a conflict is returned as `Kernel.SynthesizeInstance: instance "<name>": <cue error list>` at the sources' positions. On a successful build it runs the same check (no concreteness) and then `processInstance` asserts concreteness of the whole built instance.

Reconcile phase impact: Render only. Source, Apply, Prune and Inventory are untouched. Status: `status.requiredContracts` keeps its writer and its rule; a failing instance's `Ready` message changes as listed in proposal.md, its reason and Stalled condition do not.

## Goals / Non-Goals

**Goals:**

- No operator code computes contract demand; both renderers report the kernel's list (0013:D24).
- No operator code checks values against `#config`; synthesis is the one check.
- A `spec.values` violation still names `spec.values` with line and column, and still reports `RenderFailed` with `Stalled=True`.
- The user-visible wording before and after is pinned by a test, and the difference is in the release notes.

**Non-Goals:**

- Writing the demand on a failed render (D2).
- Changing the library's attribution of a missing required value (proposal release notes 2 and 3; a library follow-up if wanted).
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

### D3. The pre-validate goes; the findings wording stays

**Context**: Deleting the pre-validate alone makes a values error read `synthesizing release: Kernel.SynthesizeInstance: instance "x": #module.#config.message: 2 errors in empty disjunction: (and 2 more errors)`: a CUE error list's `Error()` keeps only the first finding and no positions, so `spec.values` disappears from the message, breaking the spec scenario "A values error names its origin" and the integration test that pins it.
**Explored**: Measured against library `v1.0.0-beta.6` with the `hello` fixture (`testing.opmodel.dev/modules/operator/hello@v0` `v0.0.13`) and a scratch copy whose `#config` has required fields without defaults:

| `spec.values` | Before (pre-validate) | After (synthesis, findings worded) |
| --- | --- | --- |
| `{"message": 42}` | `validating values against the module's #config: #config.message: 2 errors in empty disjunction:; #config.message: conflicting values 42 and "hello from opm" (module.cue:30:21, spec.values:1:1, spec.values:1:13); #config.message: conflicting values 42 and string (module.cue:30:11, spec.values:1:1, spec.values:1:13)` | `synthesizing release: Kernel.SynthesizeInstance: instance "<name>": ` + the same findings with `#module.#config.message` and the same positions |
| `{"bogus": 1}` | `validating values against the module's #config: field not allowed (spec.values:1:2)` | `synthesizing release: Kernel.SynthesizeInstance: instance "<name>": field not allowed (spec.values:1:2)` |
| `{"count": 1}`, `message: string` required and read by a component | `... #config.message: incomplete value string (module.cue:NN:NN)` | `synthesizing release: Kernel.SynthesizeInstance: instance "<name>": not fully concrete: components.<component>.spec.configMaps.<name>.data.message: incomplete value string (configmap.cue:71:18, module_instance.cue:103:12)` |
| `{"message": "x"}`, `count: int` required, read by nothing | `... #config.count: incomplete value int (module.cue:NN:NN)` | synthesis succeeds |

Rows 3 and 4 were measured on a scratch copy of `hello`. The change pins them through the operator on a published fixture, `required_values` (`testing.opmodel.dev/modules/operator/required_values@v0`), whose `#config` has `message: string` (read by its one component) and `count: int` (read by nothing), neither with a default. It is a CUE module root of its own, as synthesis requires for an acquired module, sits on the testing domain like every fixture (never `opmodel.dev/*`), carries an `identity/` package, and its core and catalog pins move with `task deps:cascade` like the other fixtures. The tests drive it through `KernelModuleRenderer.RenderModule`, so they pin what the operator reports, not only what the library does.

Every refused row reports `RenderFailed` and `Stalled=True` before and after: neither error carries a typed resolution cause or `render.ErrAcquire`, so `renderFailureReason` (`internal/reconcile/resolution.go:120`) returns `RenderFailed`.
**Decision**: Delete the `ValidateConfigDetailed` call and its wrapper. Wrap a `SynthesizeInstance` failure in an error type whose `Error()` is `synthesizing release: ` + the library frame + `cueFindings` of the CUE error in the chain, and whose `Unwrap` returns the library error, so `IsTransientFailure` and every `errors.Is`/`AsType` still reach the typed cause:

```go
type synthesisError struct{ err error }

func (e *synthesisError) Error() string { return "synthesizing release: " + describe(e.err) }
func (e *synthesisError) Unwrap() error { return e.err }

// describe keeps the library's frame (the text before the first CUE error in
// the chain) and replaces the CUE error's one-line summary with every finding
// and its positions. A registry fetch failure, an error with no CUE error in
// its chain, or one whose message does not end with the CUE error's own,
// reads as err.Error().
func describe(err error) string {
	if _, ok := errors.AsType[*oerrors.FetchError](err); ok {
		return err.Error()
	}
	ce, ok := errors.AsType[cueerrors.Error](err)
	if !ok {
		return err.Error()
	}
	msg := err.Error()
	frame, found := strings.CutSuffix(msg, ce.Error())
	if !found {
		return msg
	}
	return frame + cueFindings(ce)
}
```

A registry fetch failure reads exactly as today. Its chain does hold a CUE error: synthesis loads through the library's loader, which wraps `oerrors.Classify(instances[0].Err)`, a `cue/load` error list, in a `*oerrors.FetchError` whose message is that list's own, so without the explicit check `describe` would rewrite a transient fetch failure into the findings form. A failure with no CUE error in its chain (a missing-input sentinel, a context deadline) also reads unchanged.

`cueFindings` lists at most ten findings and then `; and N more`. The pre-validate's findings were bounded by the size of `spec.values`; a synthesis failure is not. The whole-instance concreteness check can list one finding for every incomplete field of every component, and neither the condition message (CRD `maxLength` 32768) nor the history entry is truncated, so an unbounded list could get the status patch itself rejected. Ten findings with their positions keep the message to a few kilobytes, as CUE's own one-line `(and N more errors)` summary kept it bounded before. The section that lands this pins the measured rows first against the current code and then against the new code.
**Rationale**: The owner decision is that the operator deletes its pre-validate once the library attributes values errors itself. The positions are the part of the old message a user acts on, and keeping them costs one helper the renderer already has. The frame changes because the error now comes from a different call; the alternative of keeping the old frame would need the operator to recognise a values error by content, which the library does not type.

### D4. Release notes 2 and 3 are accepted, not worked around

**Context**: Rows 3 and 4 of D3's table are behaviour changes, not wording: synthesis does not check values for concreteness on its attribution path, so a missing required value is reported where it is read, or not at all when nothing reads it.
**Decision**: The change accepts both, states them in the release notes, and confirms row 4 through a full `RenderModule` in section 2 (a module whose unread required field is unset renders). If the render refuses after all, release note 3 is removed.
**Rationale**: Keeping a concreteness pre-check in the operator is the pre-validate the owner decided to delete. Whether synthesis should attribute an unset required value to `#config` is the library's call; this change records the gap for it.

## Sections

1. Demand from the kernel (D1, D2): delete the walk; the demand pins pass unchanged.
2. Values check is synthesis's (D3, D4): add the `required_values` fixture, pin the current wording, reasons and outcomes, delete the pre-validate, move the pins to the new outcome, docs.

Each section ends green and leaves `main` releasable on its own; either can merge without the other.

## Risks / Trade-offs

- [The kernel's list differs from the walk on a fixture] → The integration table in `test/integration/reconcile/kernel_module_renderer_test.go` pins the exact FQN sets the walk produced for `hello`, `hello_web` and the other fixtures, and `backup_fixture_test.go:144` pins a trait demand; section 1 runs them with the registry forced (`OPM_TEST_REGISTRY_FORCE=1`) so a skip cannot pass for green. The library's own parity test compares its list with this walk.
- [A component without `#resources` now fails the render] → Already live: library `v1.0.0-beta.6`'s build fails closed before the operator's walk could run. Nothing in this change alters it.
- [The `describe` frame recovery depends on the library framing its error with `%w` prefixes] → It falls back to the full library message when the suffix does not match, so the worst case is the old one-line summary, never a lost error. A unit test pins both branches.
- [Merge conflict with the render-timeout change] → The shared file is `docs/site/diagnostics/operator-conditions.md`, where it adds a `RenderTimedOut` row beside the rows this change edits; whichever merges second merges `main`. A timeout during synthesis reads `synthesizing release: context deadline exceeded`, and `synthesisError.Unwrap` keeps `errors.Is(err, context.DeadlineExceeded)` true, so its classification is unaffected.
- [A new fixture to keep current] → `required_values` follows the fixture rules (identity package, testing domain, `task deps:cascade` moves its pins); `.tasks/cascade/test.sh`'s S2 golden list names its identity file.

## Migration Plan

No migration. The status field and its write rule are unchanged; a ModuleInstance's next successful render writes the kernel's list, which equals the walk's for every pinned fixture.

## Open Questions

None blocking. Whether the library should attribute an unset required `#config` value to the `#config` field (release notes 2 and 3) is a library question this change does not decide.
