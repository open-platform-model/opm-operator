## Context

The owner set `squash_merge_commit_message: BLANK` on 2026-10-02, reversing `PR_BODY` (workspace
`RELEASING.md`, "Owner settings", "Merge settings"). The reason: a ruleset-required mention-guard
ignores the `edited` event, so a PR body edited after a green run could reach `main` unchecked.
Under `BLANK` a squash commit is the PR title alone. That rules out both commit footers that
release-please reads:

| Intent | Before | Now (`RELEASING.md`, "Merge settings") |
| --- | --- | --- |
| Breaking change | `BREAKING CHANGE:` footer, often with `feat!:` | `!` in the PR title |
| Forced version | `Release-As: X` footer, config key forbidden | `release-as` in `release-please-config.json`, set by a normal PR, removed by the next PR after that release |

Where this repo still teaches the old practice (line numbers at `origin/main` `d2dda57`):

| Place | Lines | Stale text |
| --- | --- | --- |
| `openspec/specs/release-automation/spec.md` | 9-22 | requirement "Manual version override via release-as": the footer is the only mechanism; the key "SHALL NOT appear"; the scenario "Config carries no release-as" |
| same | 69, 81 | `BREAKING CHANGE:` footer in the two breaking-change bump scenarios |
| same | 85, 92, 97, 100-101 | "Beta prerelease line": footer as migration note, `Release-As:` hop and label change |
| same | 156, 167 | "any commit carrying a `Release-As:` footer" counts as releasable |
| `AGENTS.md` | 151 | "Beta line": `feat!` plus `BREAKING CHANGE:` footer; "(such as `Release-As: 1.1.0-beta.1`)" |
| `CONSTITUTION.md` | 101 | same beta-line sentence |
| `openspec/config.yaml` | 41-48 | constitution copy in `context`, same sentence |
| `openspec/config.yaml` | 196 | "because the squash body reaches release-please" |
| `.github/dependabot.yml` | 17-18 | "`deps` … release-please drops such merge commits from the changelog" |

The archived `prepare-release-cascade` design named this follow-up
(`openspec/changes/archive/2026-10-02-prepare-release-cascade/design.md:288-292`).

## Goals / Non-Goals

**Goals:**

- The main spec, `AGENTS.md`, `CONSTITUTION.md` and the `openspec/config.yaml` context state the
  `BLANK` practice in the same words as `RELEASING.md` and the workspace commit skill.
- The Dependabot comment states what its prefix actually does in this repo.

**Non-Goals:**

- No change to `release-please-config.json`, any workflow, task, script, Go code or CRD. There is
  no reconcile-phase impact (Source, Render, Apply, Prune and Status are untouched).
- No change to the Dependabot `gomod` prefix (see the decision below).
- The cli, catalog_opm, `.github` and workspace copies of the same text: each is its own sweep item.

## Decisions

### D1 Replace the requirement instead of modifying it

OpenSpec 1.12 refuses a MODIFIED requirement that drops a main-spec scenario (matched by name).
Two of the three scenarios of "Manual version override via release-as" ("Beta line entered via
footer", "Config carries no release-as") state the opposite of the new rule, so their names
cannot stay. The delta therefore REMOVEs the requirement (with Reason and Migration) and ADDs
"Forced version via the release-as config key". The three requirements whose scenario names
still fit are MODIFIED: every scenario keeps its name and only the body is rewritten. They are
"Version bump determination per release line", "Beta prerelease line" and "Release PR opens for
releasable commits on push to main".

### D2 What the new requirement says, grounded in release-please

- `release-as` in config overrides the computed version before the footer or the versioning
  strategy is consulted (`buildNewVersion`, release-please `src/strategies/base.ts:547-551`). The
  override also applies under `versioning: prerelease`, so the key can change a label (alpha to
  beta), which a `prerelease-type` flip cannot do.
- The value stays in force on every run until it is removed, and the next Release PR would
  re-propose an already-tagged version. So the next PR after the cut removes it
  (`RELEASING.md:465-467`).
- A Release PR still needs a commit with a visible type: release-please skips the path when the
  release notes are empty (`base.ts:286-289` and `:331-337`, "No user facing commits found"). A
  carrier PR typed `ci:` therefore waits for the next visible commit. The spec states this in a
  scenario, so nobody expects the config edit alone to cut the release.

```jsonc
// release-please-config.json, package "." — only between the carrier PR and the cut release
{
  "release-type": "go",
  "versioning": "prerelease",
  "prerelease": true,
  "prerelease-type": "beta",
  "release-as": "1.0.0-beta.1"
}
```

### D3 Breaking changes: `!` in the title, the migration note in the PR body

Under `BLANK` the title is the CHANGELOG entry. release-please lists a `!` subject under its
breaking-changes heading and links the PR number that the squash title carries, so the migration
note is one click away in the PR body. The scenario "Breaking change during beta carries its
migration note" keeps its name and now asserts that link instead of footer text in the CHANGELOG.

### D4 Dependabot: correct the comment, keep the prefix

`build` is hidden (`release-please-config.json:28`), so a Dependabot Go bump releases nothing as
titled. `deps` is a visible section here (`:23`), so the old reason ("release-please drops
`deps`") is false. `RELEASING.md` ("Runbook", "Dependabot PRs") tells the merger to check the
title's type before merging, and `AGENTS.md:146` says a `go.mod` bump that changes the image is
`deps`/`fix(deps)`. So the merger retitles a bump that should release. Keeping `build` keeps
today's behaviour, and the comment now says so:

```yaml
    commit-message:
      # build is a hidden type here, so a Dependabot Go bump releases nothing as
      # titled; retitle it fix(deps) before merging when the image should ship
      # (workspace RELEASING.md, "Dependabot PRs"). deps would release: it is a
      # visible section in release-please-config.json.
      prefix: "build"
```

Switching the prefix to `fix` would make every third-party Go bump release the operator and
cascade into the cli. That policy belongs to the owner, so it is raised as an open question
rather than decided here.

### D5 Interim before the owner applies `BLANK`

Until the setting is applied, the repo squashes with `COMMIT_MESSAGES` and a branch-commit footer
would reach `main`. The workspace commit skill already mandates merging with an empty body
(`gh pr merge --squash --body ''`, `.claude/skills/commit/SKILL.md:48-51`), so the documented
practice holds today. `AGENTS.md` carries one clause saying so. That clause is dropped once the
setting is verified, which is outside this change.

## Research & Decisions

### Does a release-as config value alone open a Release PR?
**Context**: The new requirement must not promise that the carrier PR cuts the release.
**Explored**: release-please `src/strategies/base.ts` on `main` (package version 17.11.2):
`buildReleasePullRequest` returns early with "No user facing commits found" when the release notes
are empty (`:331-337`), before any version logic matters; `buildNewVersion` takes `releaseAs` first
(`:547-551`).
**Decision**: The spec says a `release-as` value opens no Release PR by itself. A scenario covers
a hidden-type carrier.
**Rationale**: It matches the source. The repo pins `googleapis/release-please-action` v5.0.0
(`.github/workflows/release.yml:49`). That this action bundles a release-please with the same
two code paths is assumed, not checked against the action's lockfile. Both paths predate v17, so
the risk is low, and the change ships no behaviour that depends on it.

### Keep or flip the Dependabot prefix?
**Context**: The comment is wrong; the prefix it defends has an effect the comment does not state.
**Explored**: `release-please-config.json:19-30`, `RELEASING.md` "Dependabot PRs", `AGENTS.md:146`,
and the commit that introduced the prefix (`641f713`, opm-operator#101: "deps is not a
Conventional Commit type").
**Decision**: Keep `build`; rewrite the comment (D4).
**Rationale**: This is a docs-alignment change. Changing which third-party bumps release is a
release-policy decision for the owner.

## Risks / Trade-offs

- [Spec says "SHALL NOT be relied on" for a footer that still works under `COMMIT_MESSAGES`] →
  Intended. The owner decision of 2026-10-02 makes the config key the mechanism, and the empty-body merge rule
  (D5) keeps footers off `main` before the setting lands.
- [A `release-as` value left in config re-proposes a published version] → The new requirement and
  its scenario make removal by the next PR part of the contract. The draft-first release flow
  ("Release tags are never moved, deleted or re-created") already refuses to re-tag.
- [Unverified bundled release-please version] → See Research. No behaviour in this change depends
  on it.

## Migration Plan

One PR with three commits: prose, Dependabot comment, then the archive commit, which syncs the
delta into the main spec. Nothing is published and nothing releases. Rollback is reverting the PR.

## Open Questions

- Owner: should Dependabot `gomod` bumps release by default, with prefix `fix` and scope `deps`,
  instead of relying on a retitle (D4)? The answer does not block this change.
