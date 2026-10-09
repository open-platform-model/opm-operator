## Context

The operator calls the library's ownership verdicts in two places: `apply.Guard` (`ownership.CanApply`, before every apply) and `judgeDelete` (`ownership.CanDelete`, before every prune and delete). It matches `*oerrors.IdentityError` in `internal/reconcile/resolution.go` to route an identity mismatch, and it names `module.Source` in one controller test. Library v1.0.0-beta.9 removes the deprecated or unused forms of exactly these three surfaces.

The production diff of the library between the two tags, read from the module cache, holds the five removals, the helpers that only `Admit` reached (`admittedForApply`, `admittedForDelete`, `installDeletable`, `carriesNoOtherIdentity`), comments, and one corrected decision reference in `opm/k8s/labels/labels.go`. Nothing else. The library's `go.mod` is byte-identical at both tags.

## Goals / Non-Goals

**Goals:**

- Pin library v1.0.0-beta.9 with a diff a reviewer reads in a minute.
- Record, for each removed name, that the operator does not use it, with a proof that fails when it does.

**Non-Goals:**

- Any behaviour change, any new test, any rename.
- Adopting a library package the operator does not use yet.

## Decisions

### The bump uses `go get` and `go mod tidy`, not `task deps:cascade`

`task deps:cascade` moves every upstream pin the resolver reports (catalog, core, fixtures, the cli version file), and it needs the `.github` checkout. This change moves the library only. The adoptions of beta.7 and beta.8 made the same choice. `go.sum` changes through the Go tool only, and the two changed lines are read.

Alternative: wait for the cascade's pull request and review that. Rejected: the brief asks for a reviewed change that replaces it.

### The proof for each removal is the compiler, `go vet` and a search

- `Admit`: a field that does not exist does not compile in a composite literal or a selector. `go build ./...` and `go vet ./...` (which compiles the tests) are the proof, with a search for `Admit` as the readable form.
- `catalog.Source`: the same; an unknown name does not compile.
- The value receiver of `IdentityError.Error`: a value literal used as an `error`, and `errors.AsType[oerrors.IdentityError]`, no longer compile.
- `IdentityError.As` and a value target of `errors.As`: this one form still compiles (`errors.As(err, &v)` with `v` an `IdentityError` value takes any pointer) and panics at run time. `go vet` reports it (the `errorsas` check). So `task dev:vet` is part of the proof, together with a search for every mention of `IdentityError`.

### The two stale comments go in this change

The sentence "Admit is never set: it is for the operator install only" describes a field of `ApplyInput` and `DeleteInput`. After the bump the field does not exist, so the sentence names nothing a reader can find, and its second half is false. The sentence is deleted in both places; no text replaces it, because there is nothing left to say about an override that does not exist.

Alternative: leave `internal/apply` untouched, because a parallel change edits the package. Rejected: a comment that names a missing field stays wrong until someone trips on it. The cost is a possible two-line comment conflict for the parallel branch, which the report lists by line.

### No delta spec

No requirement of the operator changes. The ownership specs describe verdicts the operator asks with no `Admit`, and those verdicts are the same. The change declares `skip_specs: true`.

## Risks / Trade-offs

- [A verdict changes for a live object in a way no operator test covers] → The library's diff removes only branches guarded by `in.Admit`; with `Admit` false, `!opmManaged(live) && !admitted(in)` equals `!opmManaged(live)`. Read in the module cache, not only taken from the release note.
- [A value-typed `errors.As` target hides in a test] → `go vet` and the search cover it.
- [The comment edit conflicts with the parallel branch] → Two comment lines; listed in the report.
