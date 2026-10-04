## Why

On 2026-10-02 the owner decided the squash merge message of the releasing repos is `BLANK` (not
yet applied, see Depends on; workspace `RELEASING.md`, "Owner settings", "Merge settings"): a
squash commit carries only the PR title. The PR body and the branch commit messages never reach
`main`, so no footer does either. Under that setting:

- a breaking change is `!` in the PR title (`feat!:`, `fix(deps)!:`); a `BREAKING CHANGE:` footer
  never reaches `main`;
- a forced version is a `release-as` key in `release-please-config.json`. A normal PR sets it, and
  the next PR removes it once that release is cut, because while it stays it pins every later
  release. A `Release-As:` footer never reaches `main`.

`RELEASING.md` ("Owner settings", "Merge settings", lines 460-467) and the workspace commit skill
(`.claude/skills/commit/SKILL.md:48-55`) already say this. This repo still says the opposite:

- The main spec `release-automation`, requirement "Manual version override via release-as"
  (`openspec/specs/release-automation/spec.md:9-22`), makes the footer the only mechanism. It says
  the `release-as` key "SHALL NOT appear in `release-please-config.json`" and has a scenario that
  checks the config carries no such key. Followed today, it forbids the only forced-version
  mechanism that still works.
- These places still describe a breaking change as a `feat!` commit whose `BREAKING CHANGE:`
  footer is the migration note in the CHANGELOG:
  - the same spec's "Version bump determination per release line" (`:69`, `:81`);
  - "Beta prerelease line" (`:85`, `:92`, `:97`, `:100-101`);
  - "Release PR opens for releasable commits on push to main" (`:156`, `:167`);
  - `AGENTS.md:151` ("Beta line");
  - `CONSTITUTION.md:101`;
  - the copy of the constitution in `openspec/config.yaml:41-48`.
- `openspec/config.yaml:196` justifies a commit-message rule with "the squash body reaches
  release-please", which is no longer true.
- The `gomod` comment in `.github/dependabot.yml:17-18` says `deps` "is not a Conventional Commit
  type, so release-please drops such merge commits from the changelog". That has been false since
  `release-please-config.json:23` listed `deps` as the visible "Dependencies" section: a `deps:`
  commit releases here. The real effect of the `build` prefix is the opposite of what the comment
  implies. `build` is hidden (`release-please-config.json:28`), so a Dependabot Go bump releases
  nothing unless the merger retitles it (`RELEASING.md`, "Runbook", "Dependabot PRs").

The archived `prepare-release-cascade` change listed this as a follow-up
(`openspec/changes/archive/2026-10-02-prepare-release-cascade/design.md:288-292`). The cli has the
same-named change for its own copies.

## What Changes

- **Spec `release-automation`:**
  - **Replaced.** "Manual version override via release-as" is replaced by a requirement that makes
    the `release-as` key the mechanism. A normal PR sets it, the next PR removes it once that
    release is cut, and a footer is not relied on.
  - **Rewritten.** The breaking-change wording in "Version bump determination per release line"
    and "Beta prerelease line" becomes `!` in the PR title. "Release PR opens for releasable
    commits on push to main" stops counting a `Release-As:` footer as releasable, and states that a
    `release-as` value alone opens no Release PR.
- **Prose:**
  - `AGENTS.md:151` ("Beta line"), `CONSTITUTION.md:101` and `openspec/config.yaml:41-48` now say:
    breaking is `!` in the PR title, and the migration note goes in the PR body, which the
    CHANGELOG entry links. "No minor or major hop (such as `Release-As: 1.1.0-beta.1`)" becomes
    "such as a `release-as` of `1.1.0-beta.1`".
  - `openspec/config.yaml:196` keeps its rule with a true reason.
  - `openspec/config.yaml` apply guidance "Delivery mode": the PR title carries the highest release
    class among the change's section commits, since only the title reaches `main`.
- **`.github/dependabot.yml`:** the `gomod` prefix comment states what `build(deps)` does here:
  hidden, so no release unless a human retitles it `fix(deps)` for a security fix, merged with an
  explicit squash subject until the owner applies `PR_TITLE`. The prefix itself does not change.
- **`AGENTS.md:147`:** "`go.mod` bumps are `deps`/`fix(deps)`" becomes: an OPM `go.mod` bump
  (library) is `fix(deps)`; a Dependabot third-party Go bump stays `build(deps)` and releases
  nothing unless a human retitles it `fix(deps)` for a security fix.

No workflow, task, script, Go code, CRD or `release-please-config.json` value changes.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `release-automation`:
  - a forced version is a `release-as` key in `release-please-config.json`, set by a normal PR and
    removed by the next PR after the release, instead of a commit footer;
  - a breaking change is `!` in the PR title;
  - a footer no longer counts as releasable.

## Impact

- **SemVer: none.** Every commit is `docs` or `ci`, both hidden in
  `release-please-config.json`. The change cuts no operator release and cascades nothing into the
  cli. No API type, CRD, controller or reconcile phase (Source, Render, Apply, Prune, Status)
  changes. Principle VII: nothing is added; text is corrected.
- **Files:** `openspec/specs/release-automation/spec.md` (on archive), `AGENTS.md`,
  `CONSTITUTION.md`, `openspec/config.yaml`, `.github/dependabot.yml`.
- **Behaviour:** none. Changing release-please's behaviour is out of scope: it already honours both
  the key and, under `COMMIT_MESSAGES`, the footer. The change aligns what the repo tells people
  and agents with the owner's merge settings.
- **Delivery:** one PR; the OpenSpec archive commit rides it, and nothing is pushed to `main`
  (owner decision 2026-10-01, `RELEASING.md`, "Owner settings").

## Depends on / gates

- **Depends on: none.** This change edits only prose, a spec and a YAML comment, and touches no
  task. It therefore does not wait for `.github` `add-cascade-resolver`, which gates the `task`
  changes of Phase 2 (`RELEASING.md`, "Rollout and changes", "Changes"). It can merge in any order
  with the cli's same-named change and with the other Phase 2 changes.
- **Owner merge settings (Phase 0) not required to merge, but not yet applied.** On 2026-10-04
  `gh api repos/open-platform-model/opm-operator` still reports three of the "Merge settings" of
  workspace `RELEASING.md` "Owner settings" as not applied:
  - `squash_merge_commit_message` is `COMMIT_MESSAGES`, not `BLANK`, so a footer in a branch commit
    reaches `main` unless the merge passes an empty body;
  - `squash_merge_commit_title` is `COMMIT_OR_PR_TITLE`, not `PR_TITLE`, so a one-commit PR squashes
    under its commit subject and a `!` that is only in the PR title can be lost;
  - `allow_merge_commit` and `allow_rebase_merge` are `true`, so a merge commit or a rebase merge
    carries every branch commit's body, footers included, to `main`.

  The commit skill's empty body (`gh pr merge --squash --body ''`,
  `.claude/skills/commit/SKILL.md:48-51`) covers only the first. The policy this change writes down
  is therefore the practice only when a PR is merged by squash with an explicit subject and an empty
  body (`gh pr merge --squash --subject "<PR title> (#N)" --body ''`). `AGENTS.md` says so in one
  clause until the owner applies the settings.
- **Not in scope:**
  - the cli's copies (`cli/AGENTS.md`, `cli/CONSTITUTION.md`, `cli/openspec`);
  - catalog_opm;
  - `.github` README and mention-guard comments;
  - workspace `RELEASING.md`;
  - the repo-local `.claude/skills/commit/SKILL.md`, which carries none of the release or
    empty-body rules D5 relies on, though a session started in this repo may load it instead of
    the workspace skill.
  Each is its own item in the follow-up sweep.
