# Design: migrate-kernel-api-and-verdicts

## Context

See `proposal.md` § Why for the motivation. The design-relevant state:

- The operator already constructs one long-lived `*kernel.Kernel` with `kernel.WithRegistry` (`library-kernel-runtime`, "Kernel configured from existing inputs"). Every `loaderfile.LoadOptions{Registry: r.Registry}` at an acquire call site therefore repeats a mapping the Kernel already holds; the renderers' `Registry` fields exist only to feed those options.
- `internal/moduleacquire/acquire.go` already calls `Kernel.AcquireModuleFromRegistry(ctx, path, version)` — the post-`one-api-tier` signature. Its own `registry string` parameter is already vestigial. The registry-path acquisition needs no edit.
- `internal/render/kernel_module_renderer.go` compiles `values.Raw` with `cue.Filename("values")` and passes the resulting `cue.Value` as `synth.InstanceInput.Values`. The new `kernel.InstanceInput.Values` is `[]kernel.Source`, where a `Source` pairs a value with an `Origin` that MUST equal the `cue.Filename` the value was compiled under. `Kernel.LoadSourceFromBytes(origin, b)` builds both halves correctly in one call.
- `render.RenderResult.Warnings []string` is an operator type. It is constructed in exactly one place (`resultFromRender`) and read by `reportRenderDiagnostics` -> `emitRenderWarnings`, which passes it to `WarningTracker.Update(key, warnings)` and then to `distinctSorted`. So the strings are simultaneously the event text, the dedup key and the change-detection key.
- `Store.kernelMu` serialises every call that evaluated in the Kernel's shared context (`internal/platform/store.go:67-71`). The library-side reason for it disappears with `kernel-owns-no-build-context`, whose every verb builds in a context it creates and releases. `Store.mu` and the generation leases are a different mechanism (they guard the record and the directory a render reads) and stay.
- `internal/reconcile/resolution.go` routes on `*UnresolvedDemandsError`, `*UnmatchedComponentsError`, `*SkewError` and `IdentityError` — all of which keep their names and receivers. Only the over-subscription type changes, and it appears in one comment and one test constructor.

Constraint: the target library alpha does not exist yet. Every task must be verifiable against a local `replace` directive and must leave `go.mod` pinned to a real published version until the alpha lands.

## Goals / Non-Goals

**Goals:**

- One green tree at the end, with intermediate states green wherever the compiler allows it.
- No CRD field, condition reason, event reason, metric or flag changes: this is a dependency migration.
- Every fact the kernel's warning strings named is still named, by operator-authored text.
- The warning tracker stops treating a library sentence as its identity key.

**Non-Goals:**

- Changing what the operator renders, applies or prunes, or the render digest.
- Reshaping `render.RenderResult`. `Warnings []string` stays a `[]string`; only its producer and the tracker's key change.
- Fixing `module-acquisition`'s stale requirement text (it still names `LoadModuleFromRegistry` + `NewModuleFromValue`, which the code stopped using before this change). That is pre-existing spec drift, not this migration's to correct.
- Removing `moduleacquire.Acquire`'s vestigial `registry` parameter.
- Adopting anything else new in the target library alpha beyond what these two changes force.

## Decisions

### The three library migrations land as one change, not three

**Context**: `one-api-tier`, `cue-owned-verdicts` and `kernel-owns-no-build-context` are separate library changes.
**Explored**: (A) one operator change per library change, acquire-surface first; (B) one change.
**Decision**: B.
**Rationale**: all three ship in the same library alpha, so there is no `go.mod` pin at which only the first is present; under (A) the first would be verifiable only against a library commit. `kernel_module_renderer.go` is edited by all three (values stack, the warnings producer, the gate release), so (A) also edits it three times. The batch is small — about a dozen files — and the task groups are individually reviewable.

### Raw values become one source with a CR-field origin

**Context**: `InstanceInput.Values` is now a `[]Source`, and a `Source`'s `Origin` must match the `cue.Filename` its value was compiled under or error attribution breaks.
**Explored**: (A) keep the current `CompileBytes(..., cue.Filename("values"))` and wrap it in a hand-built `Source{Value: v, Origin: "values"}`; (B) call `Kernel.LoadSourceFromBytes(origin, raw)` and let the kernel maintain the invariant; (C) pass an empty stack and keep filling values elsewhere.
**Decision**: B, with the origin naming the CR field the bytes came from (`spec.values`) rather than the bare word `values`.
**Rationale**: (A) restates a contract the library documents as easy to get wrong and the compiler cannot check. (C) is not available — the operator must not fill values into an evaluated instance. The origin upgrade is free at the same call site and turns an anonymous position into one an operator reading an event can act on.

### `RenderResult.Warnings` keeps its type; the tracker's key stops being the sentence

**Context**: the library removed `RenderResult.Warnings []string`; the operator has its own field of the same name, whose strings are also the tracker's change-detection key.
**Explored**: (A) reshape `render.RenderResult` to carry typed advisory rows and format at the event site; (B) keep `[]string`, add one operator formatter at the single construction site, and leave the tracker keyed on the strings; (C) B plus keying the tracker on the underlying facts.
**Decision**: C.
**Rationale**: (A) changes a struct several files read for no behavior difference. (B) leaves the bug the library change exposed: the tracker's identity is a sentence, so any rewording — now the operator's own, and therefore likelier to change — re-emits every event for every object on the next reconcile. Keying on the facts (path plus both versions; component plus trait) makes rewording free, which is the point of owning the wording. The event text still comes from the same formatter, so the emitted strings are unchanged this time. The facts have to travel: `render.RenderResult` gains `UnhandledTraits map[string][]string` beside the `ResolvedVersions` rows it already carried, plain data the tracker reads for its key. `Warnings []string` and every reader of it are untouched, so this is an addition, not the reshape (A) rejected.

### A values source is checked against `#config` before synthesis

**Context**: the "values error names its origin" scenario relies on the kernel attributing a `#config` violation to the source's origin. `Kernel.SynthesizeInstance` does run that per-source check, but only after the instance build succeeds; the build bakes the merged values into the module's own tree, so for any module whose component consumes the offending value (the `hello` fixture, every real module) the build fails first and the error names the component path (`#components.hello.spec.configMaps.hello.data.message: conflicting values string and 42`) with no origin at all. The library's own test for the attribution uses a module with `#components: {}`.
**Explored**: (A) accept the build's error and drop the scenario; (B) call `Kernel.ValidateConfigDetailed(mod.ConfigSchema(), sources)` before synthesis, the kernel's documented validation entry (the `cli` render workflow already runs it over its `-f` files), and word its CUE findings with their positions; (C) ask `library` to run its per-source check before, not after, the build.
**Decision**: B, with (C) noted for `library` as a follow-up.
**Rationale**: (A) leaves the origin unreachable, which was the point of naming it. (B) is one kernel call on the same values, inside the same gate, and reports `#config.message: conflicting values 42 and string (... spec.values:1:13)`; it also asserts concreteness against `#config` with the module's defaults applied, which synthesis would have refused a step later anyway. (C) is the right long-term home but blocks on a library change this migration does not own.

### A ModuleInstance without values synthesizes with the empty document

**Context**: the "no values" scenario says the module's `#config` defaults apply. The task planned an empty source stack for that case; the new test (W1 of the verification) showed synthesis refusing it with `values: incomplete value _` even though every `#config` field of the fixture has a default. The core schema declares `#ModuleInstance.values: _` and unifies `#module.#config` with it; with no values file the field stays the open `_`, which is never concrete. This predates the migration: the alpha.26 zero-`cue.Value` path left the same field unfilled, and no test rendered without values.
**Explored**: (A) keep the empty stack and drop the scenario; (B) supply `{}` under the `spec.values` origin when the CR carries no values; (C) ask `core` to default `values` to `{}`.
**Decision**: B. `spec.values` is optional on the CRD, so absent values are a first-class input and must mean "all defaults".
**Rationale**: (A) makes an optional CRD field mandatory in practice. (B) is what the schema's own values unification does with an authored `values: {}`: defaults fill, required fields without defaults still fail concreteness, and the pre-synthesis `#config` check covers the empty document the same way. (C) is a schema change with its own consumers to weigh; worth raising, not blocking here.

### The compiled adapter changes its import, not its shape

**Context**: `opm/core` is deleted and `Compiled` moves to `opm/kernel` with the same four fields.
**Decision**: `pkg/core/compiled_adapter.go` swaps the import and the parameter type; the field copy, the nil guard and the operator's `Resource` are untouched.
**Rationale**: the operator wraps the type on arrival precisely so a library-side move like this is a one-line edit. This is that edit.

### The kernel gate goes with the shared context

**Context**: `kernelMu` exists because a `cue.Context` is not safe for concurrent builds and the Kernel held one. The library's `Kernel` is now documented as safe for concurrent use across its methods, every verb building in its own context.
**Explored**: (A) keep the gate as a throttle on acquisition and synthesis; (B) delete it.
**Decision**: B. `kernelMu`, `AcquireKernel` and the three acquire-and-release sites go; nothing replaces them.
**Rationale**: (A) keeps a mutex whose stated reason is gone and a second concurrency knob no flag reads: `--max-concurrent-renders` already bounds the render reconcilers, and the Platform reconciler builds one generation at a time by construction. Memory follows the artifacts a reconcile holds, which the library change bounds per call; the platform build's cost is paid once per generation as before.

## Risks / Trade-offs

- [The target library alpha does not exist, so the work cannot be finished in one sitting] → every task is staged against a local `replace github.com/open-platform-model/library => ../library`, with the pin bump as the last task; a reviewer can run the whole change before the release exists.
- [Re-keying the warning tracker could suppress an event that should fire, or fire one that should not] → the key is a superset of what the sentence encoded (the sentence was derived from exactly these facts), so no distinct finding collapses; a test covering "same facts, different wording" and "different facts" is a task, not an assumption.
- [The operator's warning wording diverges from the CLI's for the same fact] → accepted, and the point of the library change: the operator emits Kubernetes events with a dedup key, the CLI prints lines. The facts named are pinned by the `events-emission` and `kernel-module-renderer` deltas so neither frontend can silently drop one.
- [A concurrency defect in the library's per-verb contexts would surface in the operator first] → the library change ships a race test running acquire and synth from several goroutines on one Kernel; task 5.4 runs an envtest with `--max-concurrent-renders=2` so two ModuleInstances acquire, synthesize and render concurrently, under the race detector.
- [Changing the values origin from `values` to `spec.values` changes error text an e2e test may assert on] → the origin appears in a values-validation failure message; the task checks the e2e suite for that assertion rather than assuming none exists.

## Migration Plan

1. Add `replace github.com/open-platform-model/library => ../library` locally (as landed: `v1.0.0-alpha.27` was already published with the first two library changes and equal to `library` main, so it was pinned directly). Land the acquire-surface group (task 1) — mechanical and compiler-guided.
2. Land the values-stack change (task 2), then the compiled adapter and the verdict type renames (task 3).
3. Land the warnings producer and the tracker re-key (task 4), the group with actual behavior in it.
4. Delete the kernel gate and repoint the smoke check (task 5).
5. Run the full gate and the e2e suite (task 6).
6. When the library alpha is published, replace the `replace` directive with the real pin and re-run the gate (task 7). Rollback is a re-pin to `v1.0.0-alpha.26` together with a revert of this change; no CRD, persisted status field or event reason changes shape.

## Open Questions

- Which alpha number carries all three library changes. It does not affect the specs, the approach or the task breakdown — only the literal in `go.mod` at the last task. Resolved: `v1.0.0-alpha.28`.
