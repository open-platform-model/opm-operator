## Why

The operator pins library v1.0.0-beta.8. Library v1.0.0-beta.9 (2026-10-09, library#228) removes five names before v1.0.0: `ApplyInput.Admit`, `DeleteInput.Admit`, `IdentityError.As`, the value receiver of `IdentityError.Error`, and the alias `catalog.Source` (library ADR-016 and the closing note of ADR-015). The operator must pin a release without them. The release cascade opens its own bump pull request; this reviewed change replaces it and records the proof that the operator uses none of the five.

## What Changes

- `go.mod` and `go.sum` move `github.com/open-platform-model/library` from v1.0.0-beta.8 to v1.0.0-beta.9. No other pin moves: the library's own `go.mod` is the same at both tags, so no indirect module moves.
- Two comments in `internal/apply` (`prune.go` on `judgeDelete`, `guard.go` on `Guard`) say "Admit is never set: it is for the operator install only". The field no longer exists, so the sentence goes. No code line changes.
- No production code changes. The operator never set `Admit`, and it left `catalog.Source` and the value forms of `IdentityError` when it adopted beta.8 (opm-operator#273).
- No behaviour changes. The library states that no verdict of `CanApply` or `CanDelete` changes for a caller that did not set `Admit`; the unedited ownership tests of the operator check that here.

SemVer class: PATCH. The PR title is `fix(deps): bump library to v1.0.0-beta.9`, with no `!`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None. No requirement changes, so the change carries no delta spec (`skip_specs: true`).

## Impact

- Affected kinds: none. ModuleInstance, ModulePackage, Platform and TransformerRegistration keep every condition, reason, message and retry.
- Dependencies: one direct Go dependency moves; no new module enters `go.sum`.
- Code: two comment sentences in `internal/apply`. A parallel change (`adopt-kubernetes-lifecycle-package`) edits that package; the two edits are comment-only and listed by line in the design.
- Tests: none added, none edited.
- Generated files, CRDs, `dist/install.yaml`, the operator module: no change.
