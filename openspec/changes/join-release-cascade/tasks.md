Rebuilt on the Phase 3 wiring contract (version 3.1), `.github/openspec/changes/archive/2026-10-04-add-release-cascade-workflows/contract.md` in the `.github` repo, with the supervisor's addendum. Its §10.1 is the checklist; each task names the item it applies. The version 2 sections (a reusable notify workflow and receiver at `@main`) were committed earlier on this branch and are replaced by sections 2 to 4 below.

`<SHA>` is `2376ffae4bfc665f327d51581350dea694c01504`, the `.github` PR #9 squash commit on `main`, written `@<SHA> # .github main` (or `ref: <SHA> # .github main`).

Every section runs `actionlint` v1.7.12 (the scratchpad binary, contract §10) on the workflows it adds or changes, and the repo gates `task dev:fmt dev:vet dev:lint dev:test`. From section 4 on, `task cascade:wiring:check` too. No section adds a CI job or changes a required check's name.

## 1. Spike: read `.github` `main` at the pin

- [x] 1.1 `gh api repos/open-platform-model/.github/compare/<SHA>...main --jq .status` prints `identical`.
- [x] 1.2 At `<SHA>`: `cascade-notify` (inputs `tag`, `client-id`, `private-key`), `cascade-publish` (`dry-run`, `labels-managed`, `client-id`, `private-key`), `cascade-receive.yml` (`dry-run`, `gates-only`, `g2-mode`, `g3-mode`, `setup-go`, `setup-cue`, `cue-version`), `cascade-gates.yml` (`g2-mode`, `g3-mode`) and the resolver exist; `cascade-notify.yml` does not. Every nested third-party `uses:` is `owner/repo@<40-hex>`, so `sha_pinning_required` refuses nothing. Recorded in design.md ("What `.github` `main` carries at the pin").
- [x] 1.3 Record E1, E1b and E6 from A's archived `design.md`, with run URLs, in design.md ("Sandbox results").
- [x] 1.4 Rewrite proposal.md, design.md, the spec deltas and this file to contract 3.1 (§10.1 item 8), applying the earlier implementation review's open findings (design.md: `hack/crdref` gone, `publish-docs` needs, the E6 evidence, the CUE drift question).
- [x] 1.5 `openspec validate join-release-cascade --strict`, `task dev:fmt dev:vet dev:lint dev:test` green, then commit `docs(openspec): rebuild join-release-cascade on wiring contract 3.1`

## 2. Notify through the pinned action (§10.1 items 1, 2)

- [x] 2.1 Replace `release.yml`'s `notify-downstream` with contract §4.6's opm-operator block, byte for byte with `<SHA>`, as the last job. The comment above it says only that the job is caller-owned, declares `environment: cascade` and passes the key to the pinned `cascade-notify` action as an input.
- [x] 2.2 `git diff origin/main -- .github/workflows/release.yml` touches only the appended job; the "Workflows carry no tag mutation" search (release-automation spec) still finds no match.
- [x] 2.3 `actionlint .github/workflows/release.yml`, `task dev:fmt dev:vet dev:lint dev:test` green, then commit `ci(release): run the pinned cascade-notify action in a caller-owned job`

## 3. Receiver, gates caller and resolver pin (§10.1 items 2 to 5)

- [x] 3.1 Make `deps-cascade.yml` contract §5 with the §5.2 opm-operator `jobs:` map: `cascade-receive.yml@<SHA>`, no `labels-managed` on the `cascade` job, and the whole `publish` job with `cascade-publish@<SHA>` and `labels-managed: false`. Rewrite the header comment: the reusable workflow computes and posts the gates and holds no secret; the caller-owned `publish` job holds the key.
- [x] 3.2 `cascade-gates.yml`: only the `uses:` line changes, to `cascade-gates.yml@<SHA> # .github main`.
- [x] 3.3 `cascade-task.yml`: the resolver checkout's `ref:` becomes `<SHA> # .github main`; the header comment says the resolver comes from the pinned `.github` commit; the "Point S5 at the resolver" step loses its comment and its skip fallback and fails with `no cascade resolver at the pinned .github commit` (§10.1 item 3).
- [x] 3.4 `grep -rn -A1 'open-platform-model/.github' .github/workflows` shows five references, all `<SHA>` with ` # .github main`, and no `@main`.
- [x] 3.5 `actionlint` on the three files, `task dev:fmt dev:vet dev:lint dev:test` green, then commit `ci(cascade): publish through a caller-owned job and pin the cascade to .github main`

## 4. Wiring check in the Lint job, and Dependabot (§10.1 items 6, 7; the addendum)

- [ ] 4.1 Add `.tasks/cascade/wiring-check.sh`: the §10.1 item 6 script with `RECEIVER=true` and `PIN_COMMENT='.github main'`, the `release.yml` `env` deny-list replaced by the allow-list `REGISTRY IMAGE_NAME CUE_VERSION` (and a map check), and `runs-on: ubuntu-latest` asserted on both key-holding jobs. shellcheck clean.
- [ ] 4.2 Add `cascade:wiring:check` to `Taskfile.yml` next to `docs:pins:check`, and the step "Verify the cascade wiring" (`task cascade:wiring:check`) to `lint.yml`'s `Lint` job directly after "Install Task". No aggregate `check` task or Make target (opm-operator has none).
- [ ] 4.3 `.github/dependabot.yml`: add the `open-platform-model/.github*` ignore after the docs-kit entry in the `github-actions` entry, with the §10.1 item 7 comment.
- [ ] 4.4 Test the check: the real tree passes; each mutation of a copy is refused; the allowed edits pass. Record the result in design.md ("Wiring check, tested").
- [ ] 4.5 `actionlint .github/workflows/lint.yml`, `task cascade:wiring:check`, `task dev:fmt dev:vet dev:lint dev:test` green, then commit `ci(cascade): check the cascade wiring in the Lint job`

## 5. Docs and the re-grep (§10.1 items 8 to 11)

- [ ] 5.1 `AGENTS.md`: the cascade bullet describes the caller-owned notify and publish jobs, the reusable receive and gates workflows, the one-SHA pin moved only by a `ci(deps)` pin PR, the Dependabot ignore, and `task cascade:wiring:check` in the `Lint` job; add `task cascade:wiring:check` to the command list.
- [ ] 5.2 Replace every "no secret" wording about the notify or publish job (§10.1 item 9) and any recovery text that names a missing `cascade-notify.yml` (item 10).
- [ ] 5.3 Run the §10.1 item 11 re-grep; every hit is fixed or an allowed one (`labels-managed` on the publish step, `@main`/`ref: main` about something other than the cascade, "no secret" about `compute` or `gates`, superseded history). Record the remaining hits in the PR notes.
- [ ] 5.4 `openspec validate join-release-cascade --strict`, `task cascade:wiring:check`, `task dev:fmt dev:vet dev:lint dev:test` green, then commit `docs: describe the pinned cascade wiring in AGENTS.md`

## 6. Archive

- [ ] 6.1 Once the supervisor says the change may be archived, run `openspec verify` for `join-release-cascade` and resolve its findings. Then archive the change on this branch (`openspec archive join-release-cascade`), so the archive rides the implementing PR. Never push to `main` (workspace RELEASING.md, "Rulesets on main"). After archiving, check that `openspec/specs/cascade-receiver/spec.md` carries the real Purpose (not "TBD") and that `openspec validate cascade-receiver --strict` passes. Commit `chore(openspec): archive join-release-cascade`
