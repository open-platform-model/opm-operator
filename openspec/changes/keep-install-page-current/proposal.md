## Why

`docs/site/start/install-the-operator.md` names operator `v1.0.0-beta.4`, catalog `4.4.4`, core `v2.0.0-beta.1` and "opm v1.0.0-beta.5 carries opm-operator v1.0.0-beta.4" in its commands and sample output. opmodel.dev's v1.0 now reads this page from the operator's `1.0.0-beta.5` docs bundle, so the published page tells readers to install a release older than the one it documents. A one-off edit goes stale at the next release; nothing today notices.

## What Changes

- The operator's own version on the page moves with every release: release-please rewrites it in the Release PR, as it already rewrites `internal/version/version.go`. The page wraps each passage that names the operator version in `x-release-please-start-version` / `x-release-please-end` comment markers, and `release-please-config.json` lists the page under `extra-files`. Each release's docs bundle is built from its tag, so it names its own release; `edge` names the newest release.
- The page stops naming versions it does not own: the catalog version in the Platform example becomes `4.Y.Z` with a link to the opm catalog's newest release, the core version in the sample log is elided, the seeded-Platform line leaves the sample `opm operator install` output (the prose already says the command creates the Platform), and the sentence pairing an opm release with an operator release loses its numbers.
- A test in `internal/version` keeps it true: the page holds at least one marker block, the blocks are balanced, and every version inside a block equals `Version`.
- One edit brings the page to `v1.0.0-beta.5` now.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `release-automation`: the Release PR also rewrites the operator version on the install page.

## Impact

- **Files.** `docs/site/start/install-the-operator.md`, `release-please-config.json`, `internal/version/version_test.go`, `AGENTS.md` (the rule for version mentions in `docs/site/`).
- **Site.** Only page text; no URL changes. v1.0 shows the fix after the next operator release, or after a docs revision of `1.0.0-beta.5` that applies this change's Markdown-only commit (the config and test land in a second commit, so the page commit stays revisable, docs-kit C3 "Docs revisions").
- **Other repositories.** None. The cli's install pages may carry the same kind of literal; that is the cli's to check (named in design.md as a follow-up).
- **Not docs-kit.** A docs-kit substitution (a version shortcode or a placeholder the build fills) was considered and rejected in design.md D1.
