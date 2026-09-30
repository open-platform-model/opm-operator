## Why

opmodel.dev moves from Astro + Starlight to Hugo + Hextra. The owner ruled that the source repos rewrite their `docs/site` pages to the Hugo dialect now, with no compatibility layer in the site (O4 in `orchestration.md`). The new site runs a source lint before every build, and that lint rejects Starlight front matter. All five pages this repo publishes carry a Starlight `sidebar:` block, so on `origin/main` (32d0932) the lint reports five violations. Once change A (`port-site-to-hugo-hextra`, opmodel.dev) builds the real sources, the site build fails on these pages. This change is S6 of the set in `orchestration.md`. It rewrites the pages now, so they merge before A's section 2 reads them.

## What Changes

- In each of the five pages under `docs/site/`, the two-line `sidebar:` / `  order: N` block becomes `weight: N`, with the same number:
  - `diagnostics/operator-conditions.md`: 40
  - `operating/delete-an-instance-safely.md`: 22
  - `operating/deletion-and-pruning.md`: 30
  - `operating/install-the-operator.md`: 20
  - `reference/operator-resources.md`: 7
- Nothing else in `docs/site/` changes. The lint (section 4.1 of `orchestration.md`) finds nothing here besides the five `sidebar:` blocks: no `.mdx`, `index.md`, aside, import, component tag, image, link or untagged code fence. A grep finds no "Check against" path under `opmodel.dev/site`, so no planning comment goes stale. Titles, descriptions, types and bodies stay as they are.
- No rule or doc file in this repo describes the Starlight or Astro page format, so none changes.

This brings the pages in line with the page contract of enhancement 0018: 0018:D7 (the `#Page` shape declares `weight`, not `sidebar`) and 0018:D7:R1 (all five `type` values are already valid). The pages stay in this repo, as 0018:D8 places them.

**Not in this change:** prose edits and new pages; a `docs/site` lint in this repo's CI (a 0018:D13 follow-up); the `AGENTS.md` references to `docs/STYLE.md`, `docs/TESTING.md` and `docs/TENANCY.md`, none of which exists; rendering the pages (A does that).

## Classification

**PATCH**, documentation only. No API type, CRD, controller, condition reason, generated file or dependency changes. The commit type is `docs`, which `release-please-config.json` shows (`hidden: false`), so the merge opens or grows a release PR. Only the owner merges that. No complexity is added (Principle VII).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None. `docs/site` pages only; `.openspec.yaml` sets `skip_specs: true`.

## Impact

- **Touches:** `docs/site/**` (the five pages above), and this change directory, which is archived in the same PR.
- **Depends on:** starts when this change is planned on `main`. Merges when verify is green, after A section 1 is green (its fixture build proves the dialect renders), and before A section 2. The supervisor merges it.
- **Downstream:** A section 2 reads these pages from the supervisor's `site-src` worktree. B (`version-site-from-tags`) takes this merge's SHA as this repo's dialect floor. Until A merges, the old Astro build on opmodel.dev `main` may render degraded; this is accepted, since the site is not live.
- **Delivery:** one PR from branch `docs/adopt-hugo-page-dialect`, one section, one commit, plus the archive commit.
