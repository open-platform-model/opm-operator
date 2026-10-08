## Context

See `proposal.md` for the motivation. The facts below were read from the tree at `0f75ccb` (library v1.0.0-beta.7 in `go.mod`) and measured with a throwaway test on 2026-10-08.

`KernelModuleRenderer.synthesizeFrom` (`internal/render/kernel_module_renderer.go:151`) does three things with `spec.values`:

1. loads them as one source with origin `spec.values` (`:168`);
2. runs `Kernel.ValidateConfigDetailed(mod.ConfigSchema(), sources)` and, on failure, returns `validating values against the module's #config: <findings>` (`:184`), where `cueFindings` (`:204`) writes every CUE finding with its positions and the error chain is dropped (`%s`);
3. calls `Kernel.SynthesizeInstance` and wraps a failure as `synthesizing release: %w` (`:195`).

At v1.0.0-beta.7 synthesis makes every check the pre-check makes:

- a type or constraint violation, or a disallowed field, on a successful build: `validateCompiled(configSchema, compiled, false)` (`opm/kernel/synth.go:196`), at the sources' own positions;
- the same on a build that fails because a component reads the value: `valuesConflict` (`synth.go:180`, `acquire.go:538`);
- an unset required value, read by a component or not: `processInstance` and `requiredConfigSet` (`opm/kernel/process.go:31,62`), framed `instance "<name>": not fully concrete: ...`.

Neither failure is marked `render.ErrAcquire` and neither holds a `*FetchError`, so both read `RenderFailed` and stall (`internal/reconcile/resolution.go:120`).

## Goals / Non-Goals

**Goals:**

- One check of `spec.values`: the kernel's.
- A user loses nothing the pre-check reported: every finding, and its positions.
- No reason, condition or retry class changes.

**Non-Goals:**

- The ModulePackage render path. Its message and reason for the same defect are named below and left as they are.
- Any library change, and any wait for a library release.
- Aligning the reason between the two kinds (owner question in `proposal.md` terms: not in this change).

## Research & Decisions

### What a user sees today and after

**Context**: the change must say what changes for a user before it deletes a check.

**Explored**: a throwaway test in `internal/render` (not committed) built the hello fixture with three more `#config` fields (`note: string`, `other: int`, `port: int & >0 | *80`, none read by a component) and ran each values document through the pre-check and through `Kernel.SynthesizeInstance` alone. `<f>` is the module's `extra.cue`, `<m>` its `module.cue`.

| `spec.values` | Today (pre-check) | Kernel alone, plain `%w` wrap | Kernel alone, findings written out |
| --- | --- | --- | --- |
| `{"other": 1}` | `validating values against the module's #config: #config.note: incomplete value string (<f>:3:16)` | `synthesizing release: Kernel.SynthesizeInstance: instance "needy": not fully concrete: values.note: incomplete value string` | `values.note: incomplete value string (<f>:3:16)` |
| `{}` | `...: #config.note: incomplete value string (<f>:3:16); #config.other: incomplete value int (<f>:4:17)` | `...: not fully concrete: values.note: incomplete value string (and 1 more errors)` | `values.note: incomplete value string (<f>:3:16); values.other: incomplete value int (<f>:4:17)` |
| `{"note":7,"other":1}` | `...: #config.note: conflicting values string and 7 (mismatched types string and int) (<f>:3:16, spec.values:1:1, spec.values:1:9)` | `...: instance "needy": #module.#config.note: conflicting values string and 7 (mismatched types string and int)` | `#module.#config.note: conflicting values string and 7 (mismatched types string and int) (<f>:3:16, spec.values:1:1, spec.values:1:9)` |
| `{"note":"n","other":1,"message":42}` (a component reads `message`) | `...: #config.message: 2 errors in empty disjunction:; #config.message: conflicting values 42 and "hello from opm" (...) (<m>:30:21, spec.values:1:1, spec.values:1:33); #config.message: conflicting values 42 and string (...) (<m>:30:11, spec.values:1:1, spec.values:1:33)` | `...: instance "needy": #module.#config.message: 2 errors in empty disjunction: (and 2 more errors)` | the three findings of the pre-check, with the path `#module.#config.message` and the same positions |
| `{"note":"n","other":1,"bogus":true}` | `...: field not allowed (spec.values:1:23)` | `...: instance "needy": field not allowed` | `field not allowed (spec.values:1:23)` |
| `{"note":7,"other":"x"}` | both findings, with positions | first finding, `(and 1 more errors)` | both findings, with positions |
| `{"message":42}` (conflict and unset) | the conflict only | the conflict only | the conflict only |
| `{"bogus":true}` (disallowed and unset) | `field not allowed` only | `field not allowed` only | `field not allowed` only |
| `{"note":` (not JSON) | `compiling values: parsing source "spec.values": ...` (before either check) | the same | the same |

**Decision**: the kernel finds every defect the pre-check finds, in the same precedence. It differs in three ways: the frame (`Kernel.SynthesizeInstance: instance "<name>": [not fully concrete: ]`), the path (`values.<field>` for an unset value, `#module.#config.<field>` for a conflict, where the pre-check says `#config.<field>`), and, through the plain wrap, the loss of positions and of every finding after the first.

**Rationale**: the first two are the kernel's wording and are accepted. The third is a loss for the user and is closed by the next decision.

### How a synthesis failure is worded

**Context**: `err.Error()` of a wrapped CUE error list prints the first finding and a count. The findings and their positions are still in the chain: `cueerrors.Errors` and `cueerrors.Positions` see through `%w` (third column above, produced by the existing `cueFindings` on the kernel's error).

**Explored**:

- (A) Plain `synthesizing release: %w`. Smallest diff. Loses `spec.values:<line>:<column>` and every finding after the first. It breaks the main-spec scenario "Invalid values" (`openspec/specs/module-instance-synthesis/spec.md:164`, "an error naming their positions in `spec.values`") and the integration spec `test/integration/reconcile/kernel_module_renderer_test.go:283`, which asserts the substring `spec.values`.
- (B) The kernel's text up to its first finding, then every finding with its positions. Keeps the frame, so the phrase `not fully concrete: values.note: incomplete value string` is the same on a ModuleInstance and a ModulePackage.
- (C) The findings only, without the kernel's frame. Loses `not fully concrete`, the one phrase the two kinds would share.
- (D) Wait for the library to format (library#215 is open and covers a narrower case). Blocks issue 258 on unplanned library work.

**Decision**: (B). The renderer keeps `cueFindings` and words a synthesis failure as `synthesizing release: ` + the kernel's frame + the findings. The returned error MUST keep its chain (`Unwrap`), because the classifiers read `*oerrors.FetchError` and the typed terminal causes from a synthesis failure. A failure with no CUE finding in its chain (a registry fetch, a cancelled context) MUST read exactly as today: `synthesizing release: <err>`.

```go
// findingsError words err with every CUE finding written out and keeps
// err's chain for the classifiers.
type findingsError struct {
	msg string
	err error
}

func (e *findingsError) Error() string { return e.msg }
func (e *findingsError) Unwrap() error { return e.err }

// withFindings returns err's text with its CUE findings written out in full:
// the text up to the first finding, then cueFindings(err). An error without a
// CUE error in its chain is returned as its own text.
func withFindings(err error) string {
	text := err.Error()
	var ce cueerrors.Error
	if !errors.As(err, &ce) {
		return text
	}
	first := cueerrors.Errors(err)[0].Error()
	i := strings.Index(text, first)
	if i < 0 {
		return text
	}
	return text[:i] + cueFindings(err)
}

inst, err := r.Kernel.SynthesizeInstance(ctx, kernel.InstanceInput{...})
if err != nil {
	return nil, &findingsError{msg: "synthesizing release: " + withFindings(err), err: err}
}
```

Expected message for `{"other": 1}`: `synthesizing release: Kernel.SynthesizeInstance: instance "needy": not fully concrete: values.note: incomplete value string (<f>:3:16)`.

**Rationale**: (B) is the only option under which no information a user has today is lost, and it needs no new concept: `cueFindings` exists. The string cut is presentation only; nothing classifies on it. The composition of frame and findings was not run as one function; each half was measured. Section 1 of `tasks.md` pins it red first.

### Reason vocabulary

**Context**: the same defect reads `RenderFailed` on a ModuleInstance and `ResolutionFailed` on a ModulePackage (`loading package: Kernel.AcquireInstanceFromDir: instance "needy": not fully concrete: values.note: incomplete value string`, marked `ErrAcquire` at `internal/render/kernel_package_renderer.go`, pinned by `TestKernelPackageRenderer_UnsetRequiredValueIsRefused`).

**Explored**: with the pre-check gone the ModuleInstance failure comes from `SynthesizeInstance`, wrapped without `ErrAcquire`, so `renderFailureReason` still returns `RenderFailed`. To give a ModulePackage `RenderFailed` for a values defect, the operator would have to tell a values defect from every other package load failure. The kernel returns no type for that on either instance verb: at library `origin/main` the `*ConfigValidationError` of library#223 is created in one place, `validateSources` behind `ValidateConfigDetailed` (`opm/kernel/validate.go:78`), and `git diff v1.0.0-beta.7 origin/main` of `synth.go`, `process.go` and `acquire.go` changes only a path constant. The classifier rule is "by its type or sentinel, never by its message text" (`internal/reconcile/resolution.go:117`).

**Decision**: this change keeps `RenderFailed` on a ModuleInstance and does not touch the ModulePackage reason. No reason changes.

**Rationale**: no reason a user may match on moves. Alignment needs a typed marker from both kernel instance verbs first; it is a later change in two repos and changes a reason on one kind, so it is an owner decision.

### Whether to wait for the next library release

**Context**: library#223 (`*ConfigValidationError`) is on library `main` and in no tag.

**Decision**: do not wait. The change needs only library#217, which is in the pinned v1.0.0-beta.7. After the change the operator has no call that returns `*ConfigValidationError`.

**Rationale**: the type marks the result of the call this change deletes.

## What else the pre-check gave, and where it goes

| The pre-check gave | After the change |
| --- | --- |
| Field paths as `#config.<field>` | `values.<field>` (unset) or `#module.#config.<field>` (conflict); the kernel's paths |
| The position of the `#config` declaration, and `spec.values:<line>:<column>` on a conflict | Kept, by writing the findings out |
| Every finding at once | Kept, by writing the findings out |
| A fixed prefix naming the check | Gone; the kernel's frame replaces it |
| A refusal before any build | Gone. A values defect now costs the synthesis build (two builds when a component reads the conflicting value) inside the render slot. A valid render saves one validation. Not measured. |
| A typed error (`*ConfigValidationError`, from the next library release) | Never reached the operator: the pre-check formatted with `%s`. Not available from synthesis. |

## Every condition, reason and message that changes

ModuleInstance only. `<frame>` is `synthesizing release: Kernel.SynthesizeInstance: instance "<name>": `.

| Defect in `spec.values` | Condition and reason, before and after | Message before | Message after |
| --- | --- | --- | --- |
| Required value unset | `Ready=False`, `Stalled=True`, `RenderFailed`; unchanged | `validating values against the module's #config: #config.<field>: incomplete value <type> (<file>:<l>:<c>)` | `<frame>not fully concrete: values.<field>: incomplete value <type> (<file>:<l>:<c>)` |
| Several required values unset | unchanged | one finding per field, joined by `; `, path `#config.<field>` | the same list after `<frame>not fully concrete: `, path `values.<field>` |
| Type or constraint conflict | unchanged | `validating values against the module's #config: #config.<field>: conflicting values ... (<positions>)` | `<frame>#module.#config.<field>: conflicting values ... (<positions>)`, same positions |
| Field not allowed | unchanged | `validating values against the module's #config: field not allowed (spec.values:<l>:<c>)` | `<frame>field not allowed (spec.values:<l>:<c>)` |
| Values that are not JSON | unchanged | `compiling values: ...` | unchanged |
| Any other synthesis failure with CUE findings (for example a non-concrete module field) | unchanged | `synthesizing release: <first finding>[ (and N more errors)]` | `synthesizing release: <kernel frame><every finding with positions>` |
| A registry fetch failure during synthesis | `Ready=False`, `ResolutionFailed`, not stalled; unchanged | `synthesizing release: <err>` | unchanged |

The Warning event of a failed render carries the same message and reason as the condition, so it changes with it. ModulePackage, Platform and TransformerRegistration: nothing changes.

## Reconcile phase impact

- Source: none.
- Render: ModuleInstance only. One kernel call fewer on every render; a values defect is refused by synthesis.
- Apply, Prune: none. A refused render still applies and prunes nothing.
- Status: message text only, as in the table.

What opm-operator#260 to #269 delivered is in other phases or other files and is not touched: no file under `internal/apply`, `internal/inventory`, `internal/status` or `internal/reconcile` changes except one test row.

## Risks / Trade-offs

- [The frame-plus-findings wording was measured in halves, not as one function] → section 1 of `tasks.md` writes the exact-message test first and sees it fail before the code changes.
- [A consumer matches the old prefix in a message] → the reason is stable and is the documented thing to match; the PR body names the old and the new text.
- [The string cut finds no first finding in the text, for an error shape not seen here] → `withFindings` falls back to the kernel's own text, which is today's behaviour.
- [A values defect holds a render slot for a build] → accepted; a stalled object is rechecked every 30 minutes, not in a loop.
- [The positions of a module from the registry are paths in the CUE module cache] → unchanged from today: the pre-check prints the same positions.

## Migration Plan

One PR, no ordering against another repo. Rollback is a revert of the PR. No object needs an edit: an object that is refused today is refused after, with the same reason.

## Open Questions

None that change the specs or the tasks. The owner questions (reason alignment across the two kinds, the wording option, the release class) are in the swarm report with a recommendation each; the artifacts are written on the recommended answers.
