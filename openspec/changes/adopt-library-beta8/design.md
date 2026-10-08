## Context

See proposal.md for the motivation. Library v1.0.0-beta.8 holds three changes over beta.7: library#222 (a token endpoint refusal is typed), library#223 (the public surface is tidied; the old names stay as deprecated shims) and library#227 (the kernel names every unset required `#config` value). The archived change `adopt-library-beta7` is the model for this one.

## Goals / Non-Goals

**Goals:**

- The same pin set as the cascade's branch, on today's `main`.
- No use of a deprecated library name, proven by a build against a library without the shims.
- A verdict with evidence for each behaviour change of beta.8, and a test that fails on the next bump if a verdict stops holding.

**Non-Goals:**

- Removing the values pre-check in `KernelModuleRenderer` (the held branch `fix/drop-values-pre-validate`).
- Using `*oerrors.ConfigValidationError` to classify a values failure.
- Any edit in `internal/apply`, and any edit in `internal/reconcile` beyond the `IdentityError` forms.

## Research & Decisions

### How the bump is made

**Context**: The repo's task `task -x deps:cascade` moves the library and then the catalog, core, fixture and cli pins the resolver reports.
**Explored**: The cascade's branch (`origin/deps/cascade`, opm-operator#272) moves `go.mod` and `go.sum` only.
**Decision**: Run `go get github.com/open-platform-model/library@v1.0.0-beta.8` and `go mod tidy`, the two commands of the task's library step, as `adopt-library-beta7` did.
**Rationale**: The result MUST equal the cascade's diff, and it does: the changed lines are identical to `git diff origin/main...origin/deps/cascade -- go.mod go.sum`. The Go checksum database verified the module, the `go.mod` hash of the library is unchanged (its own requirements did not move), and no new module enters `go.sum`. The operator module's pins and the fixtures name core and the catalog, not the library, so none follows.

### The deprecated names (library#223)

**Context**: beta.8 keeps two shims for the operator: `IdentityError` still has a value receiver plus an `As` method that lets a value target match the pointer the library now returns, and `catalog.Source` is a deprecated alias of `module.Source`.
**Explored**: `staticcheck` SA1019 names the alias use (`internal/controller/transformerregistration_catalog_test.go`), which is why `Lint` fails on opm-operator#272. It does not name the `IdentityError` value forms: the deprecation notice sits on the `As` method, which the operator never calls by name. A grep finds them: `errors.AsType[oerrors.IdentityError]` in `internal/reconcile/resolution.go` and `internal/render/acquire_error_test.go`, and four value literals used as errors in tests. No other name library#223 removed or deprecated is used: `opm/helper/objectset`, `platform.Source`, the four `schema.*Metadata` names and the twelve moved `schema` paths have no reference in the operator.
**Decision**: Build `&oerrors.IdentityError{...}` and match `*oerrors.IdentityError` everywhere; import `opm/module` for `module.Source`.
**Rationale**: On beta.8 both forms match both ways, so the edit changes no behaviour today, and it is the form that survives the shim removal. The proof is a build, not a grep: `go build ./...` and `go vet ./...` (which compiles every test) run against a copy of the library at v1.0.0-beta.8 with the receiver made a pointer and `IdentityError.As` and `catalog.Source` deleted, through `-modfile` so the worktree's `go.mod` does not change.

```go
// internal/reconcile/resolution.go: the only non-test Go edit of this change.
if _, ok := errors.AsType[*oerrors.IdentityError](err); ok {
	return true
}
```

### `ValidateConfigDetailed` returns a marked error (library#223)

**Context**: The renderer's values pre-check calls `Kernel.ValidateConfigDetailed`, which now returns `*oerrors.ConfigValidationError` around the unchanged CUE error tree.
**Explored**: The pre-check words the error with `cueerrors.Errors` and `cueerrors.Positions` and formats it with `%s`, so the error it returns holds no chain. A probe on both library versions gives the same text for an unset value, a wrong type and an undeclared key.
**Decision**: No edit. The existing message tests pin it.

### Token endpoint answers (library#222)

**Context**: `oerrors.Classify` now reads the status of a token endpoint's answer where no typed status is left, and classifies by it instead of as `FetchUnreachable`.
**Explored**: A local registry that answers every request 401 with a Bearer challenge, and whose token endpoint answers a fixed status, probed on both versions.

| Site | Token endpoint | beta.7 | beta.8 |
| --- | --- | --- | --- |
| Direct fetch (module or catalog) | 401 | unauthorized, 401, not transient | the same |
| Direct fetch | 403 | not found, no status | the same |
| Direct fetch | 429 | other, 429, not transient | the same |
| Direct fetch | 503 | other, 503, transient | the same |
| Dependency load (`cue/load`) | 401 | unreachable, no status, transient | unauthorized, 401, not transient |
| Dependency load | 403 | not found, no status | the same |
| Dependency load | 429 | unreachable, no status, transient | other, 429, not transient |
| Dependency load | 503 | unreachable, no status, transient | other, 503, transient |

The message text is the same on both versions. The library also names a third form, a client that refreshes a token it holds; two catalog acquisitions on one kernel against a registry that issues an expiring token did not produce it, so it is not pinned here.

The operator reads the classification in two ways. `IsTransientFailure` is true for every `*FetchError` of any kind, so a ModuleInstance, a ModulePackage and the Platform retry on the backoff as a non-stalled `ResolutionFailed` on both versions. `keepsVerdict` (the TransformerRegistration reconciler) also requires `oerrors.ErrTransient`, so for an accepted claim the two rows that lost `transient` move from held to refused.
**Decision**: No edit to a classifier. Pin the table's dependency-load rows at the package load site, and pin `keepsVerdict` on the same errors.
**Rationale**: The new outcome is what `registration-acceptance` already requires ("refused credentials" refuse the claim) and what the conditions page says for a 429. The hold of opm-operator#262 covers a registry that does not answer or answers 5xx, and both still hold through a token endpoint. So the change is a fix of a misread form, not a new rule, and the PR title carries no `!`.
**Alternative considered**: Keep holding a claim on a token endpoint's 429, because a rate limit passes. Rejected here: a direct 429 already un-accepts (`transformerregistration_transient_test.go`), so it would be a new rule for one form, and that is the owner's decision, not the bump's.

### Unset required values (library#227)

**Context**: When a required `#config` value is unset, the kernel's refusal now holds one finding for every such value at `values.<field>`, and nothing else.
**Explored**: The hello fixture with `#config: {note: string, db: host: string, greeting: string}` and one component that reads `greeting`, probed on both versions with `spec.values` of `{}`.

| Report | beta.7 | beta.8 |
| --- | --- | --- |
| ModuleInstance (pre-check, then synthesis) | `validating values against the module's #config: #config.note: incomplete value string (<f>:4:8); #config.db.host: incomplete value string (<f>:5:12); #config.greeting: incomplete value string (<f>:6:12)` | the same text |
| Kernel synthesis alone, `Error()` | `Kernel.SynthesizeInstance: instance "needy": not fully concrete: components.hello.spec.configMaps.hello.data.greeting: incomplete value string` | `... not fully concrete: values.note: incomplete value string (and 2 more errors)` |
| Kernel synthesis alone, each finding with its positions | one finding, at the component path, positioned in the catalog and in core | `values.note ... (<f>:4:8); values.db.host ... (<f>:5:12); values.greeting ... (<f>:6:12)` |

**Decision**: Keep the pre-check. Pin that the two reports name the same fields at the same positions.
**Rationale**: The ModuleInstance condition message and event note do not move, because the pre-check runs first. On beta.8 the two reports agree finding for finding: the same fields, positions and text, with `values.` where the pre-check writes `#config.`. Two facts matter to the held branch that removes the pre-check. First, the kernel's `Error()` names only the first field and counts the rest, so that branch has to word the error tree finding by finding, as `cueFindings` does, or a user loses the other names. Second, the agreement is for unset values only: for a wrong type the kernel writes `#module.#config.<field>` and for an undeclared key both write `field not allowed`; this change pins neither.

The note of a render failure event is the error text, with no cut. The three-value message above is under 400 characters with a long file path; nothing bounds it for a module with many unset values, on beta.7 and on beta.8 alike. The test asserts the pinned message is under the 1024-character limit of `events.k8s.io/v1`. A bound is not part of this change.

## Reconcile phase impact

- Source: none.
- Render: none for a ModuleInstance or a ModulePackage. The kind of a typed fetch failure changes for two forms; the outcome (retry on the backoff) does not.
- Apply: none. Prune: none.
- Status: a TransformerRegistration that was accepted is refused with `CatalogUnresolved`, instead of held, when its catalog's dependency load meets a token endpoint that answers 401 or 429.

## Risks / Trade-offs

- [An accepted claim is un-accepted when the token endpoint rate-limits a dependency load] → It is the rule for a direct 429 today; the conditions page names it. The claim is judged again after 30 minutes.
- [The pinned token forms depend on text the embedded CUE produces] → The library owns that match (its ADR-014) and tests it; the operator's tests fail on a bump that brings back the old answer.
- [No e2e run in this change] → The CI e2e job on the PR is the proof. No e2e or integration assertion names an identity form, a token answer or a required-value message that this change moves.

## Migration Plan

None for users. Rollback is a revert of the bump commit; the pointer forms of `IdentityError` do not match on beta.7, so the revert has to take the whole commit.
