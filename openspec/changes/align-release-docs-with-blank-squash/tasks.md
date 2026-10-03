## 1. State breaking changes and forced versions under the BLANK squash message

- [ ] 1.1 `AGENTS.md:151` ("Beta line"): replace "only as a `feat!` commit whose `BREAKING CHANGE:` footer is the migration note the CHANGELOG shows" with "only as a `!` in the PR title (`feat!:`), the only text the squash commit carries (`squash_merge_commit_message: BLANK`); the title is the CHANGELOG entry, and the migration note goes in the PR body, which that entry links". Replace "(such as `Release-As: 1.1.0-beta.1`)" with "(such as a `release-as` of `1.1.0-beta.1`)". Add one sentence: "A forced version is a `release-as` key in `release-please-config.json`, set by a normal PR and removed by the next PR once that release is cut; a `Release-As:` footer never reaches `main` (until the owner applies `BLANK`, merge with an empty body: workspace commit skill)." Cite workspace `RELEASING.md`, "Owner settings".
- [ ] 1.2 `CONSTITUTION.md:101` and the copy in `openspec/config.yaml:41-48` (`context`, Principle VI "Beta line"): the same replacement of the `feat!`/`BREAKING CHANGE:` sentence as 1.1, keeping the rest of each paragraph word for word.
- [ ] 1.3 `openspec/config.yaml:196` (apply guidance): keep the rule "no body line starting with word(" and replace its reason "because the squash body reaches release-please" with "because a squash merged without an empty body would carry it to release-please until the `BLANK` squash message is applied".
- [ ] 1.4 Verify: `grep -rnE 'BREAKING CHANGE:|Release-As' AGENTS.md CONSTITUTION.md openspec/config.yaml` matches only the new "never reaches `main`" sentence of 1.1. `python3 -c 'import yaml,sys; yaml.safe_load(open(sys.argv[1]))' openspec/config.yaml` parses. `openspec validate align-release-docs-with-blank-squash --strict` passes.
- [ ] 1.5 `task dev:fmt dev:vet dev:lint dev:test` and `task docs:bundle:check` green, then commit `docs: state breaking changes and forced versions under the blank squash message`

## 2. Correct the Dependabot gomod prefix comment

- [ ] 2.1 `.github/dependabot.yml:17-18`: replace the comment above `prefix: "build"` with the four lines in design.md D4. Keep `prefix: "build"`, `include: "scope"`, the `kubernetes` group and the `github.com/open-platform-model/*` ignore unchanged.
- [ ] 2.2 Verify: `python3 -c 'import yaml,sys; yaml.safe_load(open(sys.argv[1]))' .github/dependabot.yml` parses. `git diff origin/main -- .github/dependabot.yml` shows comment lines only.
- [ ] 2.3 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `ci: say what the build prefix does to Dependabot Go bumps`

## 3. Archive

- [ ] 3.1 `openspec verify` (via `/opsx:verify`) reports the three files of section 1 and the comment of section 2 match the change.
- [ ] 3.2 Archive the change on this branch (`openspec archive align-release-docs-with-blank-squash`), which syncs the delta into `openspec/specs/release-automation/spec.md`, so the archive rides the implementing PR; never push to `main` (owner decision 2026-10-01, workspace `RELEASING.md`, "Owner settings"). Confirm that `grep -n 'Release-As\|BREAKING CHANGE:' openspec/specs/release-automation/spec.md` matches only lines of the new requirement "Forced version via the release-as config key" (its footer sentence and the scenario "A footer forces nothing"), and that requirement "Manual version override via release-as" is gone; `openspec validate release-automation --type spec --strict` reports it valid. Commit `chore(openspec): archive align-release-docs-with-blank-squash`
