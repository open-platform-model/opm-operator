## 1. State breaking changes and forced versions under the BLANK squash message

- [x] 1.1 `AGENTS.md:151` ("Beta line"): replace "only as a `feat!` commit whose `BREAKING CHANGE:` footer is the migration note the CHANGELOG shows" with "only as a `!` in the PR title (`feat!:`), the only text the squash commit carries (`squash_merge_commit_message: BLANK`); the title is the CHANGELOG entry, and the migration note goes in the PR body, which that entry links". Replace "(such as `Release-As: 1.1.0-beta.1`)" with "(such as a `release-as` of `1.1.0-beta.1`)". Add one sentence: "A forced version is a `release-as` key in `release-please-config.json`, set by a normal PR and removed by the next PR once that release is cut; a `Release-As:` footer never reaches `main` (until the owner applies the Merge settings of workspace `RELEASING.md` "Owner settings", merge by squash only, with `gh pr merge --squash --subject "<PR title> (#N)" --body ''`)." Cite workspace `RELEASING.md`, "Owner settings".
- [x] 1.2 `CONSTITUTION.md:101` and the copy in `openspec/config.yaml:41-48` (`context`, Principle VI "Beta line"): the same replacement of the `feat!`/`BREAKING CHANGE:` sentence as 1.1, keeping the rest of each paragraph word for word.
- [x] 1.3 `openspec/config.yaml:196` (apply guidance): keep the rule "no body line starting with word(" and replace its reason "because the squash body reaches release-please" with "because release-please drops a commit with such a body line, footer included, whenever the body reaches `main` (any merge not made under the `BLANK` squash message)".
- [x] 1.4 Verify: `grep -rnE 'BREAKING CHANGE:|Release-As' AGENTS.md CONSTITUTION.md openspec/config.yaml` matches only the new "never reaches `main`" sentence of 1.1. `python3 -c 'import yaml,sys; yaml.safe_load(open(sys.argv[1]))' openspec/config.yaml` parses. `openspec validate align-release-docs-with-blank-squash --strict` passes.
- [x] 1.5 `task dev:fmt dev:vet dev:lint dev:test` and `task docs:bundle:check` green, then commit `docs: state breaking changes and forced versions under the blank squash message`

## 2. Correct the Dependabot gomod prefix comment

- [x] 2.1 `.github/dependabot.yml:17-18`: replace the comment above `prefix: "build"` with the four lines in design.md D4. Keep `prefix: "build"`, `include: "scope"`, the `kubernetes` group and the `github.com/open-platform-model/*` ignore unchanged.
- [x] 2.2 Verify: `python3 -c 'import yaml,sys; yaml.safe_load(open(sys.argv[1]))' .github/dependabot.yml` parses. `git diff origin/main -- .github/dependabot.yml` shows comment lines only.
- [x] 2.3 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `ci: say what the build prefix does to Dependabot Go bumps`

## 3. Apply the implementation review

- [x] 3.1 `openspec/config.yaml` apply guidance "Delivery mode": the PR title carries the highest release class among the section commits (`!` over `feat` over `fix` over a hidden type), because only the title reaches `main`. Re-wrap the long "Beta line" line of the `context` copy without changing its words.
- [x] 3.2 `.github/dependabot.yml` and design.md D4: retitle a Dependabot Go bump to `fix(deps)` only for a security fix, and merge it with an explicit squash subject until the owner applies `PR_TITLE`. Record the settled prefix question in design.md.
- [x] 3.3 Fix the review's citations and wording in proposal.md and design.md (`AGENTS.md:147`, "decided" not "set", the empty-notes ordering, the commit count) and drop the repeated citation at the end of `AGENTS.md` "Beta line".
- [x] 3.4 `task dev:fmt dev:vet dev:lint dev:test` and `task docs:bundle:check` green, then commit `docs: apply the implementation review of the blank-squash alignment`

## 4. Archive

- [x] 4.1 `openspec verify` (via `/opsx:verify`) reports the three files of section 1 and the comment of section 2 match the change.
- [x] 4.2 Archive the change on this branch (`openspec archive align-release-docs-with-blank-squash`), which syncs the delta into `openspec/specs/release-automation/spec.md`, so the archive rides the implementing PR; never push to `main` (owner decision 2026-10-01, workspace `RELEASING.md`, "Owner settings"). Confirm that `grep -n 'Release-As\|BREAKING CHANGE:' openspec/specs/release-automation/spec.md` matches only lines of the new requirement "Forced version via the release-as config key" (its footer sentence and the scenario "A footer forces nothing"), and that requirement "Manual version override via release-as" is gone; `openspec validate release-automation --type spec --strict` reports it valid. Commit `chore(openspec): archive align-release-docs-with-blank-squash`
