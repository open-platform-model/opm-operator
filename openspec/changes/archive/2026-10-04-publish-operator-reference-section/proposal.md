## Why

The operator's resource reference is one long page at `/docs/reference/operator-resources/`, titled "Operator resources". The site's other generated references are sections: "CLI Reference" at `/docs/reference/cli/` has one page per command under a collapsible sidebar entry. The owner asked for the operator reference to follow the same layout, be titled "Operator Reference", and move to the short URL `/docs/reference/operator/` while the line is in beta and a URL change is still cheap. docs-kit added a `section` layout to its `crd` source for this (docs-kit `docs/contracts.md` C18, released in docs-kit v0.7.0).

## What Changes

- `docs-kit.cue`: the `crd` source takes `section: "reference/operator/"` instead of `page:`, the bundle owns `reference/operator/`, and the title becomes "Operator Reference". The build writes `reference/operator/_index.md` (a `## Kinds` table) and one page per kind: `moduleinstance.md`, `modulepackage.md`, `platform.md`, `transformerregistration.md`.
- `docs/site/reference/operator-resources.md` moves to `docs/site/reference/operator/_index.md`, drops `type: reference` (a section overview declares none) and gets an intro for a section of one page per kind. It still completes the generated index.
- Every link and title mention of the old page under `docs/site/` and in `AGENTS.md` moves to the new section or the kind's page. The old URL gets no redirect.
- `.opm-docs-version` and every `publish.yml` ref move to docs-kit v0.7.0 together: an older `opm-docs` refuses `section`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `operator-resource-reference`: the reference is a section, one page per kind, at `/docs/reference/operator/`.
- `docs-bundle`: the bundle owns `reference/operator/` instead of `reference/operator-resources.md`.
- `deps-cascade`: the page it must not edit is the section index at its new path.

## Impact

- **API types and controllers.** None. Only docs config, authored pages, the docs-kit pin and specs change. No SemVer class: `docs` and `ci` commits only, nothing ships in the image, so nothing releases.
- **Site.** v1.0 shows the section once the next operator release (1.0.0-beta.6 or later, built from a tag that holds this change) publishes its bundle; `edge` shows it after merge. Links into the old page from other repositories are their owners' to move (opm's `docs/site/start/_index.md`, opmodel.dev pages); the supervisor handles them.
- **Sidebar cards.** The site appends a card per child page to a section overview, so the kinds appear twice (table and cards), as on the library section. Turning that off is the site's decision (docs-kit C18 "Section cards"), not this change's.
