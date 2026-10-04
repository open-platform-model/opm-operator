Depends on: `.github` `add-release-cascade-workflows` merged before this change's PR merges, with E1, E1b and E6 recorded in its `design.md` (proposal, "Depends on / gates"). Sections 2 to 4 may be written before that, but the PR merges only after it.

Every section runs `actionlint` (the local binary, contract §10) on the workflows it adds or changes, as well as the repo gates `task dev:fmt dev:vet dev:lint dev:test`. No section adds a CI job or changes a required check.

## 1. Spike: confirm the sandbox results this change relies on

- [ ] 1.1 Read `.github` `add-release-cascade-workflows`'s `design.md` and record in this change's design.md ("Research & Decisions") the E1, E1b and E6 results, each with its run URL.
  - E1: a called job's `environment: cascade` minted a token from the caller's Environment secret.
  - E1b: a branch run was refused by the Environment's branch policy.
  - E6: `sha_pinning_required: true` accepted or refused a `uses: …/cascade-*.yml@main` call.
- [ ] 1.2 If E1 failed (contract §13.1), or E6 refused `@main` (contract §15 item 2), stop here. Report the result to the supervisor and do not start section 2. The callers' shape is then a contract change or an owner decision. Contract §13.1 at `@main` cannot work in this repo without an owner decision either: its composite actions are action references, which `sha_pinning_required` refuses unless they are SHA-pinned (design.md, "Is `@main` allowed under `sha_pinning_required: true`?").
- [ ] 1.3 Confirm on `.github` `main` that `cascade-notify.yml`, `cascade-receive.yml` and `cascade-gates.yml` exist, and that their `workflow_call` inputs match contract §4.1, §6.1 and §8.3 (`tag`; `dry-run`, `gates-only`, `g2-mode`, `g3-mode`, `setup-go`, `labels-managed`; `g2-mode`, `g3-mode`). Record any difference in design.md and report it.
  - Check statically that every step `uses:` in those three files is `owner/repo@<40-hex>`: opm-operator's `sha_pinning_required` applies to every action step that runs in its runs, called workflows included. Stop and report on any miss.
- [ ] 1.4 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `docs(openspec): record the sandbox results join-release-cascade relies on`

## 2. Notify the cli after a release is published

- [ ] 2.1 Append the job `notify-downstream` to `.github/workflows/release.yml` after `publish-release`, exactly as design.md D1 shows:
  - `needs: [release-please, publish-release]`;
  - `if: needs.release-please.outputs.releases_created == 'true' && vars.CASCADE_NOTIFY != 'off'`;
  - `permissions: {contents: read}`;
  - `uses: open-platform-model/.github/.github/workflows/cascade-notify.yml@main` with `tag: ${{ needs.release-please.outputs.tag_name }}`;
  - no `secrets:`.

  Add a comment that says why it waits for `publish-release`.
- [ ] 2.2 Check that no other job changed (`git diff` touches only the appended lines), and that the "Workflows carry no tag mutation" search (release-automation spec) still finds no match.
- [ ] 2.3 `actionlint .github/workflows/release.yml`, `task dev:fmt dev:vet dev:lint dev:test` green, then commit `ci(release): notify the cli after an operator release is published`

## 3. Receiver

- [ ] 3.1 Add `.github/workflows/deps-cascade.yml`, exactly as design.md D2 shows. That is contract §5 with the opm-operator row of §5.1: cron `47 5 * * *`, `setup-go: true`, `labels-managed: false`, and no `setup-cue`, `cue-version` or `org-github-ref`.
- [ ] 3.2 Check by reading the file:
  - top-level `permissions: {}`;
  - the single job grants only `contents: read`, `pull-requests: read` and `statuses: write`;
  - no `secrets:` key and no `steps:`;
  - the concurrency expression is contract §5's, character for character.
- [ ] 3.3 `actionlint .github/workflows/deps-cascade.yml`, `task dev:fmt dev:vet dev:lint dev:test` green, then commit `ci(cascade): add the deps cascade receiver`

## 4. Gates caller and docs

- [ ] 4.1 Add `.github/workflows/cascade-gates.yml`, exactly as contract §8.3:
  - `pull_request_target` with types `opened`, `reopened` and `synchronize`;
  - `permissions: {}`;
  - concurrency `cascade-gates-${{ github.event.pull_request.number }}` with `cancel-in-progress: true`;
  - one job `gates`, named `Cascade gates`, granting `statuses: write` and `actions: write`, which calls `cascade-gates.yml@main` with `g2-mode` and `g3-mode` from the repo variables (default `warn`).

  Check that it has no `steps:` and no checkout.
- [ ] 4.2 Add one bullet to `AGENTS.md`, after the `task -x deps:cascade` bullet (`AGENTS.md:149`):
  - `release.yml`'s `notify-downstream` dispatches `upstream-released` to the cli once `publish-release` has published the draft; `CASCADE_NOTIFY=off` stops it;
  - `deps-cascade.yml` runs the shared receiver on dispatch, daily at 05:47 UTC and by hand (`dry_run`), and it pushes only when `CASCADE_DRY_RUN` is exactly `false`;
  - `cascade-gates.yml` posts `cascade/freshness` and `cascade/settled` on every PR, in `warn` mode until `CASCADE_G2_MODE` and `CASCADE_G3_MODE` say `enforce`;
  - all three call `open-platform-model/.github` at `@main` (workspace RELEASING.md, "The cascade", "Gates", "Stop switches").
- [ ] 4.3 `actionlint .github/workflows/*.yml`, `task dev:fmt dev:vet dev:lint dev:test` green, then commit `ci(cascade): post the cascade gate statuses on every pull request`

## 5. Archive

- [ ] 5.1 Once the supervisor says the change may be archived, run `openspec verify` for `join-release-cascade` and resolve its findings. Then archive the change on this branch (`openspec archive join-release-cascade`), so the archive rides the implementing PR. Never push to `main` (workspace RELEASING.md, "Rulesets on main"). After archiving, check that `openspec/specs/cascade-receiver/spec.md` carries the real Purpose (not "TBD") and that `openspec validate cascade-receiver --strict` passes. Commit `chore(openspec): archive join-release-cascade`
