# Orchestration: single-source-provider-count

This file is the brief for the worker agent that implements this change and for the supervisor that stitches the change set together.

## Change set

Four OpenSpec changes, planned together on 2026-09-29, each in its own repo. Together they make the render build's provider count the only count: core computes it once as `#contracts.providedBy`, the library render reads it instead of its own guard, and the operator and cli name the providing catalogs from it. No enhancement entry backs them (they correct the delivery of 0015:D2/D18, and 0015 is being closed as delivered), so none carries an `enhancement.yaml`.

| ID | Repo | Change | Branch | Wave | Starts when | Merges when |
| --- | --- | --- | --- | --- | --- | --- |
| A | core | `count-providers-per-registry-entry` | `feat/count-providers-per-registry-entry` | 1 | now | first; release cut after |
| B | library | `read-provider-count-from-core` | `feat/read-provider-count-from-core` | 2 | A released | after A is released |
| C | opm-operator | `single-source-provider-count` | `fix/single-source-provider-count` | 3 | B's branch pushed | after B is released |
| D | cli | `single-source-provider-count` | `fix/single-source-provider-count` | 3 | B's branch pushed | after B is released and the cli `deps:update` commit is on main |

What each hands on:

- **A** publishes `#ContractInventory.providedBy: [#ContractFQNType]: [...#ModulePathType]` (sorted registry keys), recounted `overSubscribed`/`unfulfilled`, in core 2.0.0-alpha.12 (actual tag reported under `surface`).
- **B** reads it in the render build (single source), decodes it, floors core, and publishes the interface below in a library release.
- **C** and **D** consume B's release.

## Interface B → C, D

The contract C and D consume. B may refine names only by reporting the change under `deviations`; C and D code against what B reports under `surface`.

```go
package platform // github.com/open-platform-model/library/opm/platform

type ContractInventory struct {
	// ...existing fields unchanged...

	// ProvidedBy maps every provider-fulfilled contract FQN some enabled
	// transformer requires (defined by an enabled catalog or not) to the
	// sorted registry keys (path@major) of the enabled entries whose
	// transformers require it. OverSubscribed is exactly its keys with two
	// or more entries; a key a defined provider contract lacks is Unfulfilled.
	ProvidedBy map[string][]string `json:"providedBy"`
}

// Contracts() refuses a platform whose #contracts lacks providedBy, naming
// the field and core 2.0.0-alpha.12.
```

```go
package errors // github.com/open-platform-model/library/opm/errors

// Returned (wrapped) by Kernel.Render before staging, and by Contracts(),
// when the platform module pins a core release predating a field the kernel reads.
type PlatformCoreTooOldError struct { Platform, Field, Since string }
```

`schema.DefaultSchemaModule` = `opmodel.dev/core@v2.0.0-alpha.12`. `OverSubscribedContract{Key, Catalogs}` and the render refusal text are unchanged.

The rule behind it (A computes it, B reads it, C and D print it):

1. Which transformers count: every transformer of every enabled registry entry, iterated per entry. Only required demands (`requiredResources`, `requiredTraits`) count; optional demands and `requiredLabels` never do.
2. Fulfilment is read from the transformer's own requirement (`req.fulfilment == "provider"`), never from the defining catalog's member; counting never depends on whether an enabled entry defines the contract.
3. The key is the registry key, path with major. Two adapters in one entry are one provider; two majors of one catalog are two; two entries are two whether or not any enabled entry defines the contract.
4. `overSubscribed` is every `providedBy` key with more than one entry. `unfulfilled` is every defined provider-fulfilled resource or trait with no `providedBy` key; a contract no enabled catalog defines is never unfulfilled.
5. The render reads the same values: `match.#providers = platform.#contracts.providedBy`; the over-subscription rows are `{key, catalogs: providedBy[key]}` for every key in `overSubscribed`, iterating `providedBy` unconditionally (a presence fallback is fail-open on older cores, measured); `gate: match.resolved & platform.#contracts.routable`.

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

1. **Wave 1.** Launch the worker for A, with this change's `orchestration.md` as its brief.
2. **Check each report.** Compare `deviations` and `surface` against `design.md` and the interface above. Send a worker back with a precise ask rather than fixing its branch yourself.
3. **Finalize each change.** For each, in the worktree: `openspec archive <change> --yes`, commit the archive (`chore(openspec): archive <change>`), push, open the PR per the repo's `AGENTS.md`. Merge order: A, then B, then C and D. After A merges, merge the core release PR that release-please opens and note the released version; after B merges, do the same for the library release PR.
4. **Wave 2.** Once A is released, launch B's worker with the core version. Then run the workspace root `task deps:update` and land its per-repo output (`fix(deps)` / `test(fixtures)` per the workspace commit skill); cli's must be on main before D merges.
5. **Wave 3.** Once B's branch is pushed, launch C's and D's workers. Until B is released they develop against B's pushed head as a Go pseudo-version. Once B is released, tell each worker the version; it rebases on `origin/main`, pins the release, reruns its gates and pushes. Then finalize C and D as in step 3.
6. **Follow-ups.** Run the out-of-repo follow-ups each change's `orchestration.md` lists, after the merge they wait on.

Every PR follows its repo's `AGENTS.md`: a body of at most 250 words, no bare `@name`, only the plain co-author trailer. Merging, releasing and pushing to `main` need the user's go-ahead.

## This change (C)

**Release class.** `fix` overall, PATCH. Sections 1 and 4 are `fix(deps)` (a `go.mod` bump changes the image), sections 2 and 3 `fix(controller)`. None is `chore` or `test`, so every section releases. The PR title carries `fix(controller)`. No enhancement entry backs it, so no `enhancement.yaml`.

**Worktree setup (opm-operator).**

- Branch `fix/single-source-provider-count` from `origin/main`, worktree at `opm-operator/.claude/worktrees/single-source-provider-count`. The planning commit (`chore(openspec): plan single-source-provider-count`) is already on this branch; rebase it onto fresh `origin/main` before section 1 if main has moved.
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
  go test ./internal/controller -run TestControllers -ginkgo.focus="TransformerRegistration acceptance"
  ```

  The task-level env of `task dev:test` does not reach a hand-run `go test`; export the four variables above in the shell.

**Hazards.**

- **Red first, no red commit.** Section 2 runs the acceptance suite with the core-shaped fixture on the unchanged code and must see exactly the five specs in design.md § 2 fail (the self-refusal among them). Record that output under `gates` as `<focus> on the old check -> fail (expected): <spec names>`. The section's only commit is after the switch, green. Section 3 does the same for the refusal table (`TestInventoryRefusal` rows).
- **Measure the stamp before trusting it.** Section 1.3 reads a real built platform's transformer `metadata.modulePath`. If it is not `<path without major>/transformers`, correct the fixture formula in design.md § 2 before section 2; the fix itself does not read stamps.
- **The fixture keeps the stamped fold.** After the switch nothing reads `#composedTransformers`; the helper keeps emitting it so a regression that reads it again fails. Do not "simplify" it away.
- **Registry-backed specs skip silently** without `CUE_REGISTRY` or when core `2.0.0-alpha.12` is not on GHCR. `OPM_TEST_REGISTRY_FORCE=1` turns that into a failure; report any skip under `gates`.
- **Generated files.** Section 3 changes an API doc comment: run `task dev:manifests dev:generate` and `task operator:installer`, commit the regenerated `config/crd/bases/opmodel.dev_platforms.yaml` and `dist/install.yaml`, and never hand-edit either.
- **`hack/fixtures.sh` and `test/fixtures/fixtures.go`** are byte-identical copies of the cli's (`task fixtures:lint` at the workspace root). This change must not touch them.
- Commit messages are not Markdown: write path majors glued (`opmodel.dev/catalogs/k8up@v2`), never a bare at-sign followed by a name, and no body line starting with a word followed by an opening parenthesis (the squash body reaches release-please).

**Waits on:** B's branch pushed (to start) and B's release (to finish). Sections 1 to 3 develop against B's pushed head as a Go pseudo-version (`go get github.com/open-platform-model/library@<sha>`). Section 4 pins B's release, which the supervisor hands over; if it is not out when section 3 is committed, stop there, push, and report under `supervisor` that section 4 waits on it. After the release: rebase on `origin/main`, pin it, rerun every gate and push. Everything in this change codes against B's reported `surface`; a name B changed is fixed where it is used and reported under `deviations`.

**Hands off:** nothing downstream consumes this change. Under `surface` report: the refusal row format (`<contract> (defined by X) provided by <registry keys>`), the `ContractSubscribed` message naming a registry key, and that no condition reason was added or removed.

**Follow-ups outside this repo (supervisor):**

1. After the merge, the operator's published fixtures (`test/fixtures/modules/*`, `test/fixtures/modulepackages/*`) still pin their older core. They keep rendering (the render build evaluates the platform's core), but the workspace `task deps:pins:fixtures` moves them to the new core as a `test(fixtures)` PR when the supervisor chooses.
2. The operator sample Platform (`config/samples`) is re-pinned by the workspace `task deps:update` (step 4 of the supervisor protocol), not by this change.
3. The public site sources this repo's `docs/site/` (`opmodel.dev/site/scripts/sources.mjs`), so `operator-conditions.md` reaches it on the site's next build; nothing to edit there. The site's own over-subscribed-contracts diagnostics entry is B's (library `docs/site`).
