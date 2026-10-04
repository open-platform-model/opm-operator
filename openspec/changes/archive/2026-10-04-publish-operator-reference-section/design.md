## Context

docs-kit's `crd` source has two layouts (docs-kit `docs/contracts.md` C18): `page`, one completable page holding every kind, and `section`, a completable `<section>_index.md` with a `## Kinds` table plus one generated page per kind at `<section><kind lower-cased>.md`. The operator uses `page` today.

## Goals / Non-Goals

**Goals:** the reference renders as a section titled "Operator Reference" at `/docs/reference/operator/`, one page per kind, with every link this repository owns pointing at the new URLs.

**Non-Goals:** a redirect from the old URL (the line is in beta; docs-kit writes none); links in other repositories; hiding the site's section cards (a site decision, docs-kit C18 "Section cards").

## Decisions

### D1. Section path `reference/operator/`

The owner chose `/docs/reference/operator/`, the same short-noun shape as its siblings `/docs/reference/cli/` and `/docs/reference/definitions/`. The bundle owns the directory, so a later kind adds a page without a config change.

### D2. The authored page completes the index

`docs/site/reference/operator-resources.md` moves (`git mv`, so history follows) to `docs/site/reference/operator/_index.md`. Completing the index, its front matter is what the site shows (C15): title "Operator Reference", the config's description, `weight: 7`, and no `type` (the build refuses a type on a section overview). The body is an intro only: no `## Kinds` heading, which the generated table owns (an authored copy exits `2`).

### D3. Links move to the kind's page when they meant a kind

A link with a `#<kind>` anchor becomes `/docs/reference/operator/<kind lower-cased>/`; a link to the whole page becomes `/docs/reference/operator/`. Kind pages lift every part one level, so their `## Spec` anchors carry no suffix.

### D4. The docs-kit pin moves in the same PR

`opm-docs` before v0.7.0 refuses `section` (`#CRD` is closed), so `.opm-docs-version` and the four `publish.yml@` refs move to v0.7.0 first, in their own section, and `task docs:pins:check` keeps them together.

## Risks / Trade-offs

- **Released versions keep the old URL.** v1.0 reads the newest release's bundle; until a release built after this merge publishes, `/docs/reference/operator-resources/` stays live and the new URL does not exist. Links in other repositories should move when that release publishes, or they break in the window.
- **Kinds listed twice** on the index (table and cards), as on the library section, until the site turns cards off for generated reference sections.
