# Orchestration: name-contract-collisions

This file is the brief for the worker agent that implements this change and for the supervisor that stitches the change set together. The same text sits in every change of the set; only the title and the "This change" section differ.

## Change set

Five OpenSpec changes, planned together on 2026-09-30, in four repos. Four of them do one job:

- core stops failing to evaluate a platform that enables two majors of one catalog sharing contract keys. It folds only the keys with exactly one enabled definer and reports the rest as `collisions` and `collidingEntries`, with `routable` false.
- The library turns a collision into a typed render refusal and decodes it in `Contracts()`.
- The operator and cli name the collision.

The fifth (E) is independent: it makes the operator's build-compatibility verdict deterministic on a platform carrying two majors of one catalog. The fix is an interim safety net and does not add side-by-side majors; enhancement 0026 D9 later makes two majors legitimate through per-resolution builds (0026 OQ17 and 05-risks recommend both fixes independent of 0026). No change claims a 0026 decision, so none carries an `enhancement.yaml`.

| ID | Repo | Change | Branch | Wave | Starts when | Merges when |
| --- | --- | --- | --- | --- | --- | --- |
| A | core | `fold-colliding-contract-keys` | `feat/fold-colliding-contract-keys` | 1 | now | first; release cut after |
| E | opm-operator | `index-build-compat-by-major` | `fix/index-build-compat-by-major` | 1 | now | any time, independent of A to D |
| B | library | `refuse-colliding-contracts` | `feat/refuse-colliding-contracts` | 2 | A released | after A is released |
| C | opm-operator | `name-contract-collisions` | `fix/name-contract-collisions` | 3 | B's branch pushed | after B is released |
| D | cli | `name-contract-collisions` | `fix/name-contract-collisions` | 3 | B's branch pushed | after B is released |

What each hands on:

- **A** publishes, in core `2.0.0-alpha.13` (actual tag reported under `surface`):
  - `#ContractInventory.collisions: [...#ContractFQNType]`: sorted keys with more than one enabled definer.
  - `#ContractInventory.collidingEntries: [#ContractFQNType]: [...#ModulePathType]`: the sorted registry keys defining each.
  - `routable: len(overSubscribed) == 0 && len(collisions) == 0`.
- **B** reads them in the render build and in `Contracts()`, raises a typed refusal, moves `DefaultSchemaModule` to A's tag, and publishes the interface below in a library release.
- **C** and **D** consume B's release.
- **E** consumes nothing and hands on nothing.

## Interface B -> C, D

The contract C and D consume. B may refine names only by reporting the change under `deviations`; C and D code against what B reports under `surface`.

```go
package platform // github.com/open-platform-model/library/opm/platform

type ContractInventory struct {
	// ...existing fields unchanged...

	// Routable is true exactly when OverSubscribed and Collisions are both empty.
	Routable bool `json:"routable"`

	// Collisions lists, ascending, every contract key more than one enabled
	// registry entry's catalog lists. Such a key is in none of DefinedBy,
	// RequiredBy, Unfulfilled or Comparable, so Fulfilled and Discriminated
	// can read true while Collisions is non-empty; Routable is false.
	Collisions []string `json:"collisions"`

	// CollidingEntries maps each Collisions key to the sorted registry keys
	// (path@major) of the enabled entries listing it.
	CollidingEntries map[string][]string `json:"collidingEntries"`
}

// Contracts() decodes an absent collisions/collidingEntries as empty: every
// core before A's tag fails to evaluate a colliding platform at acquire.
```

```go
package errors // github.com/open-platform-model/library/opm/errors

type ContractCollision struct {
	Key      string   `json:"key"`
	Catalogs []string `json:"catalogs"` // sorted registry keys, path@major
}

// Raised (joined, first) by the render gate, platform-wide, under SkipUnprovided too.
type ContractCollisionsError struct{ Contracts []ContractCollision }

// Raised only when #contracts.routable is false and no over-subscription or collision row explains it.
type NotRoutableError struct{}
```

`kernel.RenderDiagnostics.Collisions []oerrors.ContractCollision` (sorted by key) and `RenderDiagnostics.Routable bool`. `schema.DefaultSchemaModule` = A's tag. `schema.ProvidedBySince` (the floor) is unchanged. `UnresolvedDemand.Colliding []string` is diagnostic only.

The rule behind it (A computes it, B reads it, C and D print it):

1. A definer is an ENABLED registry entry whose catalog lists the key in `#resources`, `#traits` or `#blueprints`. A disabled entry never counts.
2. A key with exactly one definer folds into `defined` and `definedBy` as before. A key with more is a collision: it is in `collisions` and `collidingEntries` and in none of `defined`, `definedBy`, `requiredBy`, `unfulfilled` or `comparable`. `providedBy` and `overSubscribed` are unaffected and can co-occur with a collision.
3. `routable` is false while any collision exists. `fulfilled` and `discriminated` can still read true (the stated limitation), so no consumer reads either as safe while `collisions` is non-empty.
4. The render refuses a colliding platform with `ContractCollisionsError`, whatever the instance and whatever `SkipUnprovided` says. Its rows are `{key, catalogs: collidingEntries[key]}` for every key in `collisions`, guarded on presence. Absence is provably empty: an older core fails to evaluate such a platform. A `routable` false that no row explains raises `NotRoutableError`.
5. The operator words it as reason `ContractCollisions`, ahead of `OverSubscribedContracts` and `ComparablePredicates`. The cli prints a colliding-contracts section and counts collisions in the routable verdict and the exit message.

## Worker protocol

One worker agent per change, in its own git worktree of the change's repo.

1. Create the worktree from fresh `origin/main`, on the branch named in the change set: from the repo root, `git fetch origin`, then `git worktree add .claude/worktrees/<change> -b <branch> origin/main`, then work inside that directory. Read the repo's `AGENTS.md` and `openspec/config.yaml` first; they bind. The repo-specific setup in this file comes next.
2. Run the repo's apply workflow on the change: `openspec instructions apply --change <change> --json`, then `tasks.md` section by section. The commit task that closes each section is the only commit you make.
3. After the last section is green, push the branch: `git push -u origin <branch>`. Do not archive the change, open a PR, merge, tag or release; the supervisor does.
4. On a blocker, stop and report it. Do not widen scope, and do not edit another repo; a design question goes in the report.
5. End with exactly this block as your final message:

```text
change:      <ID> <repo>/<change>
branch:      <branch> @ <head sha> (pushed: yes|no)
sections:    <n>/<total> committed
commits:     <sha> <subject>   (one line per commit)
gates:       <command> -> pass|fail   (one line each; name every failing or skipped test)
surface:     <what the next change consumes: exported symbols, flags, SPEC sections, output strings>
deviations:  <every departure from design.md or this file, with the reason; "none">
supervisor:  <what only the supervisor can do: a release, a pin bump, merge order, a decision; "none">
follow-ups:  <work found outside this change's repo; "none">
```

## Supervisor protocol

1. **Wave 1.** Launch the workers for A and E, each with its change's `orchestration.md` as its brief.
2. **Check each report.** Compare `deviations` and `surface` against `design.md` and the interface above. Send a worker back with a precise ask rather than fixing its branch yourself.
3. **Finalize each change.** For each, in the worktree: `openspec archive <change> --yes`, commit the archive (`chore(openspec): archive <change>`), push, open the PR per the repo's `AGENTS.md`. Merge order: A, then B, then C and D; E whenever it is green. After A merges, merge the core release PR that release-please opens and note the released version; after B merges, do the same for the library release PR.
4. **Wave 2.** Once A is released, launch B's worker with the core version. Do NOT run the workspace root `task deps:update` yet. Run it once B is released, and land its per-repo output (`fix(deps)` / `test(fixtures)` per the workspace commit skill). This departs from the previous set on purpose: a kernel older than B renders a colliding platform pinned to A's core (duplicate objects, measured), so no workspace platform moves to A's core before a refusing kernel exists.
5. **Wave 3.** Once B's branch is pushed, launch C's and D's workers. Until B is released they develop against B's pushed head as a Go pseudo-version. Once B is released, tell each worker the version; it rebases on `origin/main`, pins the release, reruns its gates and pushes. Then finalize C and D as in step 3.
6. **Follow-ups.** Run the out-of-repo follow-ups each change's `orchestration.md` lists, after the merge they wait on.

Every PR follows its repo's `AGENTS.md`: a body of at most 250 words, no bare `@name`, only the plain co-author trailer. Merging, releasing and pushing to `main` need the user's go-ahead.

## This change (C)

**Release class.** `fix` overall, PATCH. Sections 1 and 3 are `fix(deps)` (a `go.mod` bump changes the image), section 2 is `fix(controller)`. None is `chore` or `test`, so every section releases. The PR title carries `fix(controller)`. One Ready reason (`ContractCollisions`) is added; no API type or field changes. No `enhancement.yaml`: no 0026 decision is fully delivered by the set.

**Worktree setup (opm-operator).**

- Branch `fix/name-contract-collisions` from `origin/main`, worktree at `opm-operator/.claude/worktrees/name-contract-collisions`. The planning commit (`chore(openspec): plan name-contract-collisions`) is already on this branch; rebase it onto fresh `origin/main` before section 1 if main has moved (in particular if E has merged).
- Export the workspace registry mapping in two lines (a one-line `export A=x B="$A"` leaves `OPM_REGISTRY` empty), plus the force switch, so the registry-backed Platform specs (`buildKernelOrSkip`) run instead of skipping and a skip becomes a failure:

  ```bash
  export CUE_REGISTRY='opmodel.dev=ghcr.io/open-platform-model,testing.opmodel.dev=ghcr.io/open-platform-model,registry.cue.works'
  export OPM_REGISTRY="$CUE_REGISTRY"
  export OPM_TEST_REGISTRY_FORCE=1
  ```

- Run the gates through `task dev:test`: it installs envtest into `./bin`, sets `KUBEBUILDER_ASSETS` and runs every package but `/e2e`. It passes `CUE_REGISTRY` through as GHCR by default, which is what this change wants; never set `TEST_CUE_REGISTRY` and never start the local registry (Registry Policy rule 3 is not triggered: no fixture changes).
- For a hand-run `go test` (a focused Ginkgo spec, the red runs), `KUBEBUILDER_ASSETS` must be ABSOLUTE; `setup-envtest` prints a relative `bin/k8s/...` path, which breaks as soon as `go test` changes into the package directory:

  ```bash
  export KUBEBUILDER_ASSETS="$(realpath "$(./bin/setup-envtest use 1.35.0 --bin-dir ./bin -p path)")"
  go test ./internal/controller -run TestInventoryRefusal
  go test ./internal/controller -run TestControllers -ginkgo.focus="refuses a colliding platform"
  ```

  The task-level env of `task dev:test` does not reach a hand-run `go test`; export the four variables above in the shell.

**Hazards.**

- **Red first, no red commit.** Section 2 adds the four `TestInventoryRefusal` rows and the Ginkgo spec on the unchanged gate and must see exactly those fail: a collision-only inventory reports `OverSubscribedContracts` and "0 over-subscribed contracts", and a collision beside another finding is not named. Record that output under `gates` as `<test> on the old gate -> fail (expected): <row names>`. The section's only commit is after the fix, green.
- **The operator counts nothing.** Colliding keys come from `Collisions`, their entries from `CollidingEntries`. Never derive entries from `DefinedBy` (it lacks colliding keys by construction) or from the registry.
- **Precedence is part of the contract.** `ContractCollisions` ahead of `OverSubscribedContracts` ahead of `ComparablePredicates`, every finding in one message, collision first. Do not drop the other findings when a collision exists.
- **Fail closed.** A non-empty `Collisions` refuses whatever `Routable` reads, and `Routable: false` with no rows still refuses (design.md § 2).
- **Messages are order-independent and the inventory is not mutated.** Sort into copies; `failReconcile` gates its warning event on an unchanged message, and the store may hold the inventory.
- **Generated files.** Section 2 changes an API doc comment: run `task dev:manifests dev:generate` and `task operator:installer`, commit the regenerated `config/crd/bases/opmodel.dev_platforms.yaml` and `dist/install.yaml`, and never hand-edit either.
- **`hack/fixtures.sh` and `test/fixtures/fixtures.go`** are byte-identical copies of the cli's (`task fixtures:lint` at the workspace root). This change must not touch them.
- **No enhancement reference in output strings.** The collision and backstop messages carry none; a code comment may cite `0026:D9` once at a symbol if the rationale needs it.
- **`go.mod` never reaches a PR on a pseudo-version.** Section 3 pins B's release.
- Commit messages are not Markdown: write path majors glued (`opmodel.dev/catalogs/opm@v5`), never a bare at-sign followed by a name, and no body line starting with a word followed by an opening parenthesis (the squash body reaches release-please).

**Waits on:** B's branch pushed (to start) and B's release (to finish). Section 1 also needs A's core tag on GHCR, which B's own start condition already guarantees. Sections 1 and 2 develop against B's pushed head as a Go pseudo-version (`go get github.com/open-platform-model/library@<sha>`). Section 3 pins B's release, which the supervisor hands over; if it is not out when section 2 is committed, stop there, push, and report under `supervisor` that section 3 waits on it. After the release: rebase on `origin/main`, pin it, rerun every gate and push. Everything in this change codes against B's reported `surface`; a name B changed is fixed where it is used and reported under `deviations`. Rebase over E if it has merged (no file overlap expected).

**Hands off:** nothing downstream consumes this change. Under `surface` report: the reason name (`ContractCollisions`), the collision finding text (header and row format), the unroutable backstop text, the precedence, and the library version pinned.

**Follow-ups outside this repo (supervisor):**

1. The operator's published fixtures (`test/fixtures/modules/*`, `test/fixtures/modulepackages/*`) and the sample Platform (`config/samples`) still pin their older core. They move to A's core through the workspace `task deps:pins:fixtures` and `task deps:update`, run only after B is released (supervisor step 4).
2. The public site sources this repo's `docs/site/` (`opmodel.dev/site/scripts/sources.mjs`), so the new `operator-conditions.md` row reaches it on the site's next build; nothing to edit there. The site's colliding-contracts diagnostics entry is B's (library `docs/site`).
3. In enhancements: 0026 OQ17's resolution cites this change once it lands (A's follow-up 2).
