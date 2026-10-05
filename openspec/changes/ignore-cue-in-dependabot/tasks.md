Gates for section 1: the YAML parses, `git diff` shows only the intended lines, `task dev:lint` (no Go code changes, so `task dev:test` is not affected), and `openspec validate ignore-cue-in-dependabot --strict`. Every commit is `ci` or `chore` typed, so nothing releases.

## 1. Ignore cuelang.org/go and ociregistry in Dependabot

- [ ] 1.1 In `.github/dependabot.yml`, add `- dependency-name: "cuelang.org/go"` and `- dependency-name: "cuelabs.dev/go/oci/ociregistry"` to the `ignore:` list of the `gomod` update, after the `github.com/open-platform-model/*` entry, under one comment in the style of the comment above it: CUE moves only through a library release (the library's pull request runs the CUE check) and reaches the operator through the cascade's library bump, whose `go get` of the library raises it by minimal version selection (kept by `go mod tidy`); the library's `go.mod` sets the `ociregistry` version (`cuelang.org/go` sets only a floor), so it moves with library releases too; cite owner decision j4 (2026-10-03) and workspace RELEASING.md, section "Pin classes". Leave the `github-actions` block, the `kubernetes` group, `commit-message` and every other key unchanged.
- [ ] 1.2 Check the file: it parses as YAML (`python3 -c 'import yaml,sys; yaml.safe_load(open(sys.argv[1]))' .github/dependabot.yml`), the parsed `gomod` entry's `ignore` list holds exactly the three dependency names, and `git diff` shows only the added comment and the two ignore entries.
- [ ] 1.3 For each open Dependabot `gomod` pull request (`gh pr list --author app/dependabot`), check whether its `go.mod` diff moves `cuelang.org/go` or `cuelabs.dev/go/oci/ociregistry`; record any hit in the change's report for the owner to close or hold (workspace RELEASING.md, "Dependabot PRs"). Do not close it here.
- [ ] 1.4 `task dev:lint` and `openspec validate ignore-cue-in-dependabot --strict` green, then commit `ci(dependabot): leave cuelang.org/go to library releases`.

## 2. Archive (rides this PR)

The archive rides the implementing PR, never a push to main. This section runs only after the review of section 1, never in the planning or implementation run.

- [ ] 2.1 `openspec archive ignore-cue-in-dependabot --yes`. This adds "Dependabot leaves cuelang.org/go to library releases" to `openspec/specs/release-automation/spec.md` and applies the MODIFIED "Dependabot leaves OPM Go modules to the release cascade". The spec's Purpose line does not mention Dependabot and stays as it is.
- [ ] 2.2 `openspec validate --specs --strict` shows no new failure in `release-automation`, then commit `chore(openspec): archive ignore-cue-in-dependabot`. The commit touches only `openspec/`.
