## Context

See proposal.md (Why). This change is S6 of the set in `orchestration.md`; the dialect contract it follows is section 4 there, and the lint that checks it is section 4.1.

State on `origin/main` (32d0932), read 2026-09-30:

- **Pages.** `docs/site/` holds five `.md` files and nothing else: `diagnostics/operator-conditions.md`, `operating/delete-an-instance-safely.md`, `operating/deletion-and-pruning.md`, `operating/install-the-operator.md` and `reference/operator-resources.md`. None is an overview: this repo has no `index.md` or `_index.md`.
- **Front matter.** Each page has `title`, `description` and `type` on lines 2-4, then `sidebar:` on line 5 and `  order: N` on line 6, then `---` on line 7. The types are `reference`, `how-to`, `explanation`, `how-to` and `reference`, all valid under 0018:D7:R1.
- **Lint.** The section 4.1 lint (sha256 `dae9717a...973c6b`) over `docs/site` prints exactly five findings, one per page, all `<file>:5: sidebar: is Starlight front matter; write weight: N`, and exits 1.
- **Bodies.** No asides, imports, component tags, images, Markdown links, reference definitions, code fences or raw `href=`/`src=`. Every "Check against" path in the planning comments names a file in `opm-operator`, `cli`, `core`, `library`, `enhancements` or `opm-kind-demo`, never `opmodel.dev/site/content/...`, so no comment path goes stale with the move.
- **Order.** The page-order helper (sha256 `edc62591...7b5f65`) prints five lines, each the page path, a tab, then the order: `diagnostics/operator-conditions.md<TAB>40`, `operating/delete-an-instance-safely.md<TAB>22`, `operating/deletion-and-pruning.md<TAB>30`, `operating/install-the-operator.md<TAB>20`, `reference/operator-resources.md<TAB>7`.
- **Shared sections.** The site assembles sections across repos (0018:D8). `diagnostics/` also holds cli and library pages, `operating/` an opm page, and `reference/` opm, catalog_opm and cli pages. A weight orders a page among all of them.
- **Rule files.** `AGENTS.md`, `CONSTITUTION.md`, `README.md`, `openspec/config.yaml` and `docs/RENDERING.md` never mention Starlight, Astro, MDX, `sidebar` or `opmodel.dev/site` (grep, case-insensitive; the only "astro" hits are the word "catastrophic"). The source-repo audit for this migration also found no opm-operator rule file to edit.

## Goals / Non-Goals

**Goals:**

- The lint prints `opm-dialect-lint: OK` over this repo's `docs/site`.
- Every page keeps its place: the page-order helper prints the same lines before and after.
- No byte outside the five front-matter blocks changes.

**Non-Goals:**

- Rendering the pages in Hugo. S workers never build the site; A's section 1 fixture build is the evidence that the dialect renders, and A renders these pages from section 2 on.
- Committing the lint or the page-order helper to this repo. A commits the lint in opmodel.dev; the helper is never committed.
- Fixing the dangling `docs/STYLE.md`, `docs/TESTING.md` and `docs/TENANCY.md` references in `AGENTS.md` (lines 109-111). They are real, but they are not about the site format. They are a follow-up.

## Decisions

### 1. `sidebar.order: N` becomes `weight: N`, with the same number

The pages MUST replace the two-line block with one `weight: N` line in the same place (line 5), keeping N. For example, in `operating/install-the-operator.md`:

```yaml
# before
type: how-to
sidebar:
  order: 20
---
# after
type: how-to
weight: 20
---
```

**Why the same number:** weights order pages across repos inside one section. Keeping every number keeps the order the authors chose among cli, library, opm and catalog_opm pages, and it lets the page-order diff prove that nothing moved. All five numbers are at least 1, as the contract requires; Hugo reads `weight: 0` as unset.

**Alternatives:** renumbering the operator pages (for example 10, 20, 30) was rejected: it would reorder pages against the other repos' weights and make the order diff meaningless.

### 2. No rule or doc file changes

O4 asks each S change to update the repo's rule and doc files that describe the Starlight/Astro format. This repo has none (Context, "Rule files"). Adding a repo-local copy of the page rules was rejected: the workspace `STYLE.md` "Site Pages" section already states them, and a copy here would be a second place to keep in sync. For the same reason, no S change adds repo-local callout style.

### 3. One section, no spike

The change is one front-matter edit in five files, and the lint runs on the whole tree, so all five edits MUST land together. design.md carries no unverified assumption: on 2026-09-30 the edit was applied to a scratch copy of `origin/main`'s `docs/site`, and the lint printed OK with an unchanged page order. Whether the dialect renders is A's section 1 claim, not this change's, and it gates the merge, not the work.

### 4. Gates follow `orchestration.md` section 10, not the full config gate list

`openspec/config.yaml` lists `task dev:manifests dev:generate`, `task dev:fmt dev:vet`, `task dev:lint` and `task dev:test` as the merge gates. For this docs-only change the section runs the lint, the order diff, `task -d <wt> dev:fmt dev:vet` (the same as `make -C <wt> fmt vet`), `git -C <wt> diff --check` and `openspec validate adopt-hugo-page-dialect --strict --no-interactive`. `dev:manifests dev:generate` are for API changes, and there are none. `dev:lint` and `dev:test` check Go code, which this change does not touch, and `dev:test` downloads envtest. `openspec validate` MUST name the change id: 19 main specs fail `--strict`, so `--all` fails for reasons outside this change.

### 5. Commit subject

`docs(site): adopt the hugo page dialect`, the one subject every S change in this set commits under (`orchestration.md` section 3 spells it out for S1). `docs(site)` is this repo's existing scope for these pages (bdc7d11).

## Interface

This change adds no interface name. From `orchestration.md` section 6 it relies on:

- the source lint (check 2), committed by A as `opmodel.dev/site/scripts/lint-sources.sh` and run by `task lint:sources`, byte-identical to the section 4.1 lint;
- `OPM_SRC_OPM_OPERATOR` and the read-only mount `/src/opm-operator`, fed from the supervisor's `site-src` worktree (`WS/opm-operator/.claude/worktrees/site-src`);
- ordering by `weight`, then title, in `_partials/sidebar.html` and `_partials/opm/section-children.html`;
- front-matter validation inside Hugo (check 3) and the expected-page check (check 6, Q2).

## Reconcile phase impact

None. No Source, Render, Apply, Prune or Status code changes.

## Research & Decisions

### Which findings the lint reports here

**Context**: the task is "turn every lint finding into a task", so the finding list had to be exact.
**Explored**: wrote the section 4.1 lint and page-order helper to scratch files, checked both hashes, ran them over the planning worktree's `docs/site` at 32d0932, then applied Decision 1 to a scratch copy from `git archive origin/main docs/site` and ran both again. Grepped the tree for `:::`, `import`, code fences, `](`, `index.md`, `site/content` and "Check against: opmodel.dev/".
**Decision**: five findings, all `sidebar:`; the dry-run copy passes the lint and the order diff is empty.
**Rationale**: `orchestration.md` 4.1 records opm-operator at 5 findings on `origin/main`, which matches.

## Risks / Trade-offs

- [The Astro build on opmodel.dev `main` renders degraded between this merge and A's merge] → Accepted by the owner (O4); the site is not live. The supervisor tells the owner.
- [A new page added to `docs/site` after this merge uses `sidebar:`] → A's lint fails the site build and names file and line (orchestration.md trap 27). Fix the page, never the lint. `add-cross-namespace-source-grants`, the only active change here, does not touch `docs/site`.
- [An invalid `type:` silently picks another Hugo layout] (trap 2) → This change keeps the five valid types; the lint and Hugo's front-matter check catch a bad one.
- [`openspec validate --all --strict` fails in this repo] (trap 31) → Validate by change id only. After the archive, skip the `--all` re-run that section 7 step 7 asks of other repos, as section 10 says.
- [The history-rewrite hook refuses rebase and lease-push] (trap 34) → Update the branch only by merging `origin/main`.
- [The `docs` commit opens or grows a release PR] → Expected. The owner merges it; this change cuts no release.

## Migration Plan

One PR. After it merges, the supervisor records the merge SHA as this repo's dialect floor for B and refreshes the `site-src` worktree. Rollback is a revert of the one commit; it would also make A's lint fail on this repo again.

## Open Questions

None.
