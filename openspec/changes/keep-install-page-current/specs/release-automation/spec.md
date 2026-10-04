## ADDED Requirements

### Requirement: The Release PR rewrites the install page's operator version

`release-please-config.json` SHALL list `docs/site/start/install-the-operator.md` under the root package's `extra-files`, and every passage of that page that names the operator's version SHALL sit inside an `x-release-please-start-version` ... `x-release-please-end` block of HTML comment markers, each on its own line outside code fences and at the content indentation of its enclosing block (three spaces inside an ordered-list item), so the page renders as it would without them. A block SHALL name no version other than the operator's. A test in `internal/version` SHALL fail when the page has no block, when a block is not closed, or when a version inside a block differs from the `Version` constant.

#### Scenario: A Release PR moves the page with the constant

- **WHEN** release-please opens the Release PR for `1.0.0-beta.6`
- **THEN** the PR rewrites `Version` in `internal/version/version.go` and every version inside the install page's blocks to `1.0.0-beta.6`, and the release's docs bundle names `v1.0.0-beta.6` in its install commands

#### Scenario: Another project's version inside a block is refused

- **WHEN** a commit puts a core or catalog version inside one of the install page's blocks
- **THEN** `task dev:test` fails, naming the version and the expected operator version

#### Scenario: A detached marker is refused

- **WHEN** an edit removes every block from the install page, or leaves a start marker without its end
- **THEN** `task dev:test` fails, naming the page
