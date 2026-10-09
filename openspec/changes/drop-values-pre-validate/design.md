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

## After library v1.0.0-beta.8

The first build of this change (2026-10-08, library v1.0.0-beta.7) was held: for an unset required value that a component reads, the kernel named component paths and only the first finding, so the claim "a user loses nothing" above was false for the common case. Library v1.0.0-beta.8 (library#227) closes it: `processInstance` reports one finding for every unset required value at `values.<field>` and nothing else while any is unset. `main` pins beta.8 (opm-operator#273). The sections above are kept as written on beta.7; where they differ from this section, this section holds. Measured on 2026-10-09 with the tests of `internal/render/required_values_test.go`.

### What the kernel's text still lacks

**Context**: on beta.8 the kernel's error tree holds every unset value, but `Error()` of a wrapped CUE list is the first finding and `(and N more errors)`, with no position. A ModulePackage reported that text as it was (`acquireFailed("loading package", err)`).

**Decision**: both renderers word a kernel refusal from the typed tree (`cueerrors.Errors`, `cueerrors.Positions`): the caller's prefix, the kernel's text in front of its first finding, then every finding with its positions (`withFindings`, `internal/render/findings.go`). The wording keeps the error chain. The gate decision of 2026-10-08 that left the ModulePackage wording for later is replaced by the brief of 2026-10-09, which asks for both kinds.

**Rationale**: the only text read from the kernel's message is the frame in front of the first finding, and it is cut by position of that finding's own text, not matched against a pattern. No finding is taken from text. If the text does not hold the first finding, the error keeps its own text.

### Positions of a package

**Context**: the operator extracts a ModulePackage artifact to a new temporary directory on every reconcile (`fetchModulePackageArtifact`), and CUE positions are absolute paths. A stalled package is rechecked every 30 minutes.

**Explored**: (A) positions as CUE reports them: the condition message of an unchanged package changes on every recheck and no two events are equal. (B) no positions on a package: loses where in the package the value is. (C) positions under the package's CUE module root written relative to it; all others (the CUE module cache) as reported.

**Decision**: (C). The root is the nearest directory at or above the package directory that holds `cue.mod`. A ModuleInstance keeps absolute positions: its module lies in the CUE module cache, whose paths are stable, and `main` printed the same positions.

### A registry fetch failure keeps its text

**Context**: a package whose import cannot be fetched fails with a CUE error that holds one position, and its text ends in the registry's answer (`... 401 Unauthorized`), which `internal/render/token_endpoint_test.go` (opm-operator#273) pins.

**Decision**: `withFindings` leaves an error with a `*oerrors.FetchError` in its chain as its own text, on both kinds. This is a test of the type, not of the text.

### Bounds

**Context**: nothing bounded the text of a render failure. The condition message has `maxLength: 32768` in the generated CRDs. events.k8s.io/v1 refuses a note over 1024 bytes and the client-go recorder does not cut (`internal/status/claims.go` records the limit for the two notes the operator already bounds). One unset value costs about 60 characters plus its file path; twenty unset values of a module in the CUE module cache pass 1024.

**Explored**: (A) one bound of 1024 on the error text: no edit in `internal/reconcile`, but the condition then names about six of twenty values although it could hold all. (B) the error text bounded at the condition limit, and the event note bounded at 1024 by a function the two event lines call.

**Decision**: (B). `findingsError.Error()` keeps whole findings up to 32768 bytes; `render.EventNote(err)` keeps whole findings up to 1024 bytes and ends `; and <N> more findings`; a long text with no findings is cut between two characters and ends ` ... (<N> more characters)`. Only the event of the stalled branch is changed in each kind: that is where a values refusal goes. The transient branches (a registry fetch failure, a platform not ready) are not touched.

**Rationale**: the condition is where a user reads the list, so it keeps all of it. An event that the API server refuses is lost, so a cut event is better than none.

### Messages, before (`main` at 7cf939d) and after

Module `#config: {note: string, db: host: string, greeting: string, port: int & >0 | *80}`; a component reads `greeting`. `<f>` is the module's file; `<MI>` is `synthesizing release: Kernel.SynthesizeInstance: instance "needy": `; `<MP>` is `loading package: Kernel.AcquireInstanceFromDir: instance "needy": `; `<old>` is `validating values against the module's #config: `.

| Kind | Defect | Before | After |
| --- | --- | --- | --- |
| ModuleInstance | three unset | `<old>#config.note: incomplete value string (<f>:4:8); #config.db.host: incomplete value string (<f>:5:12); #config.greeting: incomplete value string (<f>:6:12)` | `<MI>not fully concrete: values.note: incomplete value string (<f>:4:8); values.db.host: incomplete value string (<f>:5:12); values.greeting: incomplete value string (<f>:6:12)` |
| ModuleInstance | wrong type | `<old>#config.note: conflicting values string and 7 (mismatched types string and int) (<f>:4:8, spec.values:1:1, spec.values:1:9)` | `<MI>#module.#config.note: conflicting values string and 7 (mismatched types string and int) (<f>:4:8, spec.values:1:1, spec.values:1:9)` |
| ModuleInstance | constraint | `<old>#config.port: 2 errors in empty disjunction:; #config.port: conflicting values 80 and -1 (<f>:7:20, spec.values:1:1, spec.values:1:53); #config.port: invalid value -1 (out of bound >0) (<f>:7:14, spec.values:1:53)` | the same three findings and positions after `<MI>`, path `#module.#config.port` |
| ModuleInstance | undeclared key | `<old>field not allowed (spec.values:1:46)` | `<MI>field not allowed (spec.values:1:46)` |
| ModulePackage | three unset | `<MP>not fully concrete: values.greeting: incomplete value string (and 2 more errors)` | `<MP>not fully concrete: values.greeting: incomplete value string (instance.cue:22:13); values.db.host: incomplete value string (instance.cue:23:13); values.note: incomplete value string (instance.cue:25:12)` |
| ModulePackage | wrong type | `<MP>#module.#config.note: conflicting values string and 7 (mismatched types string and int)` | the same and ` (instance.cue:25:12, instance.cue:35:16)` |
| ModulePackage | constraint | `<MP>#module.#config.port: 2 errors in empty disjunction: (and 2 more errors)` | `<MP>#module.#config.port: 2 errors in empty disjunction:; #module.#config.port: conflicting values 80 and -1 (instance.cue:24:21, instance.cue:35:57); #module.#config.port: invalid value -1 (out of bound >0) (instance.cue:24:15, instance.cue:35:57)` |
| ModulePackage | undeclared key | `<MP>field not allowed` | `<MP>field not allowed (instance.cue:35:51)` |

Reason, `Stalled` and the retry are the same before and after in every row: `RenderFailed`, stalled, 30-minute recheck on a ModuleInstance; `ResolutionFailed`, stalled, 30-minute recheck on a ModulePackage. Every other package load failure that holds CUE findings and is not a registry fetch failure gains its positions and its later findings the same way.

## Reconcile phase impact

- Source: none.
- Render: ModuleInstance only. One kernel call fewer on every render; a values defect is refused by synthesis.
- Apply, Prune: none. A refused render still applies and prunes nothing.
- Status: message text only, as in the tables. Since the section "After library v1.0.0-beta.8": the ModulePackage message too, and the note of the Warning event of a stalled render failure on both kinds.

What opm-operator#260 to #273 delivered is in other phases or other files and is not touched: no file under `internal/apply`, `internal/inventory` or `internal/status` changes. In `internal/reconcile` two lines change, the `Eventf` of the stalled branch in `classifyRenderError` (`moduleinstance.go`) and in `renderModulePackage` (`modulepackage.go`), plus tests in `resolution_test.go`.

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
