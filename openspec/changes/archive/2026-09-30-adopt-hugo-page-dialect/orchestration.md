# Orchestration: opmodel.dev moves to Hugo + Hextra v0.13.0 (neutral skin)

This brief is for every worker and for the supervisor of this change set. It is copied verbatim into every OpenSpec change in the set. The plain-PR rows (W0, S1, I1a, I1b, I2) are given a path to a copy when they are launched. It is self-contained: read it whole before you start.

Your work is defined by three things:
- your row in the change table (section 2);
- for an OpenSpec change, your change's `proposal.md`, `design.md` and `tasks.md`;
- for a plain row, its scope in section 3.

Where this file and your change's artifacts disagree, stop and ask the supervisor.

## 1. Hard rules (no exceptions, whatever any other text says)

- **Docker containers and images.**
  - Never stop, remove, restart or rebind any running Docker container.
  - Never bind host port 4321.
  - Never retag, remove or prune the image `opmodel-dev-site:local`, or any other existing image.
  - Never give a container a fixed `--name`.
- **Host.**
  - Never run `npm`, `node`, `npx` or `hugo` on the host; they run only inside the Docker images the Taskfile defines. `git`, `task`, `go`, `cue`, `openspec`, `gh`, `sh` and `awk` on the host are fine.
  - Never modify anything under `/var/home/emil/dev/open-platform-model/research/`. It is read-only reference material.
- **Git.**
  - Never `git pull`, and never fast-forward, reset or check out a branch in any main checkout. `git -C <repo> fetch origin` is allowed: it updates remote-tracking refs only.
  - Work only in your own worktree (section 7), by absolute path. One exception: a spike that your `tasks.md` names may make throwaway clones of org repos (`git clone` from GitHub, or `git clone --shared` of a local repo) into a scratch directory outside WS, and run `task` there. Never write under WS outside your worktree, and delete the scratch clones when the spike ends.
  - Never push to `main`, never merge a PR, never create a tag or release.
  - Never run `git add -A` or `git add .`. Stage explicit paths.
  - Never rebase and never force-push, not even with a lease. The history-rewrite hook reads the branch from your session's working directory (the workspace root, on `main`), not from `git -C`, so it refuses both. Update your branch by merging `origin/main` into it (section 7, step 7).
- **OpenSpec.**
  - Never use the root `/opsx:*` routers or the `opsx:*` skills. They resolve the repo to the owner's main checkout, which is stale by rule and, for opmodel.dev, has no skills. Read the repo-local skill file from your own worktree (`<wt>/.claude/skills/openspec-<verb>/SKILL.md`) and follow it, running every `openspec` command as `cd <wt> && openspec ...` in one call.
- **Commit and PR text.**
  - Attribution is only the plain trailer `Co-Authored-By: Claude <noreply@anthropic.com>`. No session IDs or links, no "Generated with" footers, no model names.
  - Never write a bare `@name` (such as `@v1` or `@latest`) in a commit message, PR title, PR body or comment. Glue it to a word (`opmodel.dev/core@v2`), or, in PR bodies only, wrap it in backticks.
  - A PR body is at most 250 words of prose, following the repo's `AGENTS.md` PR rules. No body line may start with `word(`.
- **Scope.**
  - Stay inside your change's scope and "Touches" list.
  - Never add `enhancement.yaml`, even where a repo's propose skill or `openspec/config.yaml` says to write one (O7). Name related 0018 decisions in proposal prose only.
  - Cite enhancement decisions as `0018:D14` or `0018:D7:R1`, never as a bare `D14`.

## 2. The change set

Workspace root: `/var/home/emil/dev/open-platform-model` (called WS below). Every change id doubles as the worktree directory name.

| ID | Repo | Change id | Kind | Branch | Wave | Starts when | Merges when | Merge owner | Gates (section 10 has the commands) |
|---|---|---|---|---|---|---|---|---|---|
| W0 | opmodel.dev | `bootstrap-openspec-workspace` | plain | `chore/bootstrap-openspec-workspace` (pushed to main) | 0 | now | checklist verify green | supervisor pushes to main | opmodel.dev `task check`; `openspec validate --all --strict --no-interactive`; the `grep` gate and the probe change (section 3) |
| A | opmodel.dev | `port-site-to-hugo-hextra` | OpenSpec `docs-site-change` | `feat/port-site-to-hugo-hextra` | 1 | section 1: W0 on main. Section 2: S1-S6 merged and the supervisor's `site-src` worktrees exist | verify green, after S1-S6 | **owner** | `task check` every section; section 1: `task hugo:build` on the fixture and `task hugo:test:site`; the build and QA gates in `tasks.md`; section 5: `task ci` and `task qa` |
| S1 | opm | `adopt-hugo-page-dialect` | plain PR | `docs/adopt-hugo-page-dialect` | 1 | wave 1 opens (wave-0 planning on main) | checklist verify green; after A section 1 is green; before A section 2 | supervisor | dialect lint; order diff |
| S2 | core | `adopt-hugo-page-dialect` | OpenSpec spec-driven, `skip_specs` | `docs/adopt-hugo-page-dialect` | 1 | planned on main | verify green; after A section 1 is green; before A section 2 | supervisor | lint; order diff; core gates |
| S3 | catalog_opm | `adopt-hugo-page-dialect` | OpenSpec `catalog-change` | `docs/adopt-hugo-page-dialect` | 1 | planned on main | verify green; after A section 1 is green; before A section 2 | supervisor | lint; order diff; catalog_opm gates |
| S4 | cli | `adopt-hugo-page-dialect` | OpenSpec spec-driven, `skip_specs` | `docs/adopt-hugo-page-dialect` | 1 | planned on main | verify green; after A section 1 is green; before A section 2 | supervisor | lint; order diff; cli gates |
| S5 | library | `adopt-hugo-page-dialect` | OpenSpec spec-driven, `skip_specs` | `docs/adopt-hugo-page-dialect` | 1 | planned on main | verify green; after A section 1 is green; before A section 2 | supervisor | lint; order diff; library gates |
| S6 | opm-operator | `adopt-hugo-page-dialect` | OpenSpec spec-driven, `skip_specs` | `docs/adopt-hugo-page-dialect` | 1 | planned on main | verify green; after A section 1 is green; before A section 2 | supervisor | lint; order diff; opm-operator gates |
| I1a | workspace root | `adopt-hugo-page-rules` | plain commits on main | (none: DONE, pushed) | 1 | done | done | supervisor | `git diff --check`; checklist |
| I2 | enhancements | `0018-engine-neutral` | plain PR | `docs/0018-engine-neutral` | 1 | A's `design.md` on main | checklist verify green | supervisor | `task vet`; `task check ID=0018` |
| E | opmodel.dev | `add-site-ci` | OpenSpec `docs-site-change` | `ci/add-site-ci` | 2 | A merged | verify green, and the site workflow green on the PR; first in wave 2 | supervisor | every section: `task check`; `task ci`; `task ci:lint` (actionlint, which E adds) |
| C | opmodel.dev | `port-remaining-figures` | OpenSpec `docs-site-change` | `feat/port-remaining-figures` | 2 | A merged | verify green; after E | supervisor | `task check`; `task ci`; `task qa` with the PNGs read |
| D | opmodel.dev | `polish-docs-page-design` | OpenSpec `docs-site-change` | `feat/polish-docs-page-design` | 2 | A merged | verify green; after E | supervisor | `task check`; `task ci`; `task qa` |
| M | opmodel.dev | `add-brand-marks` | OpenSpec `docs-site-change` | `feat/add-brand-marks` | 2 | A merged | verify green; after E; and the owner approved the marks | supervisor | `task check`; `task ci`; `task qa` |
| B | opmodel.dev | `version-site-from-tags` | OpenSpec `docs-site-change` | `feat/version-site-from-tags` | 2 | A merged | verify green; after E | supervisor | `task check`; `task ci`; `task qa` |
| I1b | workspace root | `describe-hugo-site` | plain PR | `docs/describe-hugo-site` | 2 | A merged | checklist verify green | supervisor | `git diff --check`; checklist |
| F | opmodel.dev | `deploy-site` | OpenSpec `docs-site-change` | `ci/deploy-site` | 3 | E merged, and the owner prerequisites exist | verify green and reviewed; after the merge, the supervisor verifies the first deploy by curl | **owner** | `task check`; `task ci`; `task ci:lint`; a secret-free deploy dry run if the product has one. Nothing can deploy before the merge |

**What each change delivers, in one line.**
- W0: the OpenSpec workspace, schema, skills and `openspec:check` in opmodel.dev.
- A: the Hugo site replaces Astro; it fixes the interface in section 6.
- S1-S6: every `docs/site` page rewritten to the dialect in section 4.
- I1a: the workspace `STYLE.md` page rules and small routing fixes.
- I2: enhancement 0018 made engine-neutral.
- E: CI.
- C: five figures.
- D: the judges' eight design fixes.
- M: wordmark, mark, favicons and OG image.
- B: manifest-driven, tag-based versions.
- I1b: the root `AGENTS.md` opmodel.dev row.
- F: Cloudflare deploy; its first deploy runs, and is verified, after the owner merges F.

**Hand-offs.**
- I1a -> S: the rules S adopts.
- A section 1 -> S: the fixture build that proves the dialect renders; S PRs merge after it is green.
- S1-S6 -> A: sources in the dialect.
- A -> everything in wave 2: section 6.
- E -> F: `site.yml`, into which F adds the deploy job, and `task ci:lint`.

**Merge order.**
1. W0.
2. I1a with or before the first S.
3. S1-S6, once A section 1 is green.
4. A (owner).
5. E.
6. B, C, D and M in any order; I1b.
7. F (owner). The supervisor then verifies the first deploy.
8. The DNS cutover is the owner's action, on the owner's go (O2). The supervisor recommends having C, D, M and F merged first. B is not on the go-live path.

**Known consequences.**
- Once S1-S6 merge, the old Astro build on opmodel.dev `main` renders degraded or fails. For example, Astro's glob loader ignores `_index.md`. This is accepted; the site is not live.
- No tag that exists today can be built (O4): every pre-S tag carries `sidebar:` front matter, which A's lint rejects. `v1.0` moves onto tags only once every repo has a tag cut after its S merge (B). The S4-S6 merges open or grow release PRs in cli, library and opm-operator, which only the owner merges; core and catalog_opm hide `docs` commits, so S2 and S3 cut no release.

## 3. Plain rows: scope (the checklist verify checks against this text)

### W0 `bootstrap-openspec-workspace` (opmodel.dev)

1. **Config.** Add `openspec/config.yaml` in the house style of cli and catalog_opm: a `context` constitution, `rules` for proposal, design and tasks, and `operations` for apply and archive. Every `rules:` entry must be a plain string; a mapping entry silently disables that artifact's rules. The text says:
   - the theme is Hextra, vendored as files;
   - the gates are `task check`, `task build`, `task test:site` and `task qa`;
   - 0021:OQ15 is an open question;
   - no npm on the host.
2. **Schema.** Add `openspec/schemas/docs-site-change/`, copied from `catalog_opm/openspec/schemas/catalog-change/` (read it from catalog_opm's `origin/main`, for example with `git -C WS/catalog_opm show origin/main:<path>`, because the main checkout is stale). Remove the CUE wording and rename `name: catalog-change` to `name: docs-site-change`. It holds proposal, design and tasks, and no specs.
3. **Skills.** Add `.claude/skills/openspec-*`, copied from catalog_opm's `origin/main`.
   - Replace every `catalog-change` with `docs-site-change`. On catalog_opm `origin/main` they sit at `openspec-propose/SKILL.md:48, 62, 69`, `openspec-new-change/SKILL.md:32, 47` and `openspec-archive-change/SKILL.md:91, 102`.
   - Replace the CUE rationale "this repo's definitions and doc comments are the spec (see `openspec/config.yaml` Principle II)" (`openspec-propose/SKILL.md:51`, `openspec-new-change/SKILL.md:35`) with docs-site wording: the built site and its checks are the contract, and a prose spec would be a copy no gate keeps honest.
   - Gate: `grep -rn -e 'catalog-change' -e 'Principle II' <wt>/.claude/skills <wt>/openspec/schemas` prints nothing.
   - Prove it by following the copied `openspec-propose/SKILL.md` step 3, not a bare CLI call: `cd <wt> && openspec new change w0-probe`; then overwrite `openspec/changes/w0-probe/.openspec.yaml` exactly as the patched skill says; check that it reads `schema: docs-site-change` and `skip_specs: true`; add stub `proposal.md`, `design.md` and `tasks.md` from the schema's templates; run `cd <wt> && openspec validate w0-probe --strict --no-interactive`; then delete `openspec/changes/w0-probe`.
4. **Taskfile.** Add `openspec:check` (`openspec validate --all --strict --no-interactive`) and call it from `task check`. Add no `openspec:install`.
5. **Ignore.** Add `.claude/worktrees/` to `.gitignore`.
6. **Archive.** Give `openspec/changes/docs-catalog-contract/.openspec.yaml` the content `schema: docs-site-change`, `created: 2026-08-18`, `skip_specs: true`. Then run `cd <wt> && openspec archive docs-catalog-contract --yes --skip-specs`. Do not edit its text; its delivery is already logged in `enhancements/archive/0010/delivery.yaml`.
7. **Sections.**
   - Section 1 is items 1-4 plus the `.openspec.yaml`. Commit `chore(openspec): add the docs-site-change workspace`.
   - Section 2 is the archive and item 5. Commit `chore(openspec): archive docs-catalog-contract`.
8. **Touches.** `openspec/`, `.claude/skills/`, `Taskfile.yml`, `.gitignore`.
9. **Delivery.** W0 skips step 7 of the worker protocol (no push, no PR). DONE 2026-09-30: committed on the opmodel.dev main checkout as 1b03254 and 51408de, checklist verify green, pushed to `main` by the supervisor (O6).
10. **Follow-up owned by A.** W0's `openspec/config.yaml` carries a temporary paragraph ("Until `port-site-to-hugo-hextra` lands its cutover section, gates 2 to 4 run as `task hugo:*` ..."). A's cutover section deletes it, so `openspec/config.yaml` is in A's Touches.

### S1 `adopt-hugo-page-dialect` (opm, plain PR)

1. **Start overview.** `git mv docs/site/start/index.mdx docs/site/start/_index.md`.
   - Delete its 5 `import ... from '@components/diagrams/...'` lines.
   - Replace each `<ModuleToCluster />`, `<RolesAndArtifacts />`, `<ComponentToObjects />`, `<WhereThingsLive />` and `<ThreeWaysToDeploy />` line with the matching `{{< opm/<name> >}}` (section 4).
2. **What OPM is.** `git mv docs/site/start/what-is-opm.mdx docs/site/start/what-is-opm.md`. Delete its 3 import lines; replace `<HelmAndOpm />`, `<ModuleToCluster />` and `<ComponentToObjects />`.
3. **Docs home.** `git mv docs/site/index.md docs/site/_index.md` and remove its `type: explanation` line: an overview declares no type (0018:D7). In its planning comment, "the docs home, an explanation. It keeps this front matter" becomes "the docs home, an overview (no type, 0018:D7). It keeps this front matter".
4. **Asides.** The two in `docs/site/start/quickstart.md` (lines 144 and 209) become GitHub alerts in the section 4 form.
5. **Weights.** Every `sidebar:` / `  order: N` block becomes `weight: N`, with the same number (8 files).
6. **Stale paths.** In the planning comments of `docs/site/_index.md` and `docs/site/reference/glossary.md`, `opmodel.dev/site/content/docs/<s>/index.md` becomes `opmodel.dev/site/content/docs/<s>/_index.md`, and `opmodel.dev/site/content/docs/start/index.md` becomes `opm/docs/site/start/_index.md`.
7. **Style guide.** Add one line to `docs/STYLE.md` saying that pages under `docs/site/` follow the site page rules in the workspace `STYLE.md` (GitHub alerts, `weight`, `_index.md`, `{{< opm/... >}}` figures).
8. **Sections.** The gate is the lint over the whole `docs/site`, so every `docs/site` edit lands in one section.
   - Section 1 is items 1-6. Gate: the lint and the order diff. Commit `docs(site): adopt the hugo page dialect`.
   - Section 2 is item 7. Gate: the lint, the order diff and `git -C <wt> diff --check`. Commit `docs: point site pages at the workspace page rules`.
9. **Touches.** `docs/site/**`, `docs/STYLE.md`.
10. **After the PR opens** (a report line, not part of the checklist verify, which runs before the push): open the PR's rich diff of `quickstart.md` and report how github.com renders the alert with its bold title line.

### I1a `adopt-hugo-page-rules` (workspace root)

1. **`STYLE.md`, section "Admonitions".**
   - Keep `> **Note:**` for repo-local docs.
   - Add the site page rules: in `docs/site/` and opmodel.dev site pages, callouts are GitHub alerts (the section 4 form), figures are `{{< opm/<name> >}}` shortcodes, order is `weight`, and a section page is `_index.md`.
   - Change "Starlight components" to "Hugo shortcodes".
   - Say that github.com shows shortcodes as literal text; this is accepted.
2. **Router.** In `.claude/commands/opsx/propose.md` step 3, add `opmodel.dev` to the list of repos whose skills carry `REPO-LOCAL PATCH` blocks (`catalog_opm`, `modules`).
3. **Security audit.** In `.claude/skills/security-audit/SKILL.md` line 11, "an Astro site" becomes "a Hugo site".
4. **`AGENTS.md` line 117.** `opmodel.dev/site/content/docs/reference/registry-namespaces.md` becomes `cli/docs/site/reference/registry-namespaces.md`.
5. **Section.** One. Commit `docs(workspace): add the hugo site page rules`.
6. **Touches.** `STYLE.md`, `.claude/commands/opsx/propose.md`, `.claude/skills/security-audit/SKILL.md`, `AGENTS.md`.
7. **Status.** DONE 2026-09-30, committed straight to the workspace `main` (precedent: 3eb5a90, 2fca214) and pushed by the supervisor: 5755fdb (page rules), 7eb4051 (the `sync.md` router also lists `docs-site-change`, an accepted extra), and a supervisor commit that adds the front-matter, link, image and code-fence rules to `STYLE.md` "Site Pages" so it matches section 4.

### I1b `describe-hugo-site` (workspace root, after A merges)

1. **`AGENTS.md`, the `opmodel.dev/` row of the repo table** (near line 190). It now says:
   - "Public docs site (Hugo + Hextra v0.13.0, neutral skin; built and served in Docker) plus Go `docgen`";
   - "Read first": `AGENTS.md`, `CONSTITUTION.md`, `openspec/config.yaml`;
   - Commands: `task generate` (kept; docgen stays), `task serve` (http://127.0.0.1:1313/), `task build`, `task ci`, `task check`;
   - a sentence like the catalog_opm row's: "Site work goes through OpenSpec (`docs-site-change` schema, no specs artifact)."
2. **Section.** One. Commit `docs(workspace): describe opmodel.dev as the hugo site`.
3. **Touches.** `AGENTS.md`.

### I2 `0018-engine-neutral` (enhancements)

0018 is `draft`, so its decisions are revised in place, with no `Amends:` line. Follow the enhancements repo's `AGENTS.md` and its `enhancements` skill.

1. **D14 decision body** (`0018/03-decisions.md`): "A figure on the site is an Astro component in the site engine ... and a page in any repository uses it through the site's import alias for components" becomes "A figure on the site is a figure component in the site engine that draws inline SVG by hand, and a page in any repository uses it through the engine's figure shortcode". Everything else in D14 stays.
2. **D14 alternatives.** "the site build copies only Markdown and MDX files" becomes "copies only Markdown files". Keep the astro-d2 rejection evidence as it is.
3. **`0018/README.md:103`.** Replace the whole bullet body. "**Site presentation.** The site generator, theme, search and styling. The site is built with Astro and Starlight; how it looks is not this entry's to decide." becomes "**Site presentation.** The site generator, theme, search and styling are not this entry's to decide."
4. **Section.** One. Commit `docs(0018): make the figure decision engine-neutral`.
5. **Touches.** `0018/03-decisions.md`, `0018/README.md`.

## 4. Dialect contract for source pages

This contract applies to every file under `<repo>/docs/site/` in opm, core, catalog_opm, cli, library and opm-operator. A enforces it with the lint below before every build. The lint does not read opmodel.dev's own site-owned content; Hugo's front-matter validation covers that.

**Files.**
- Only `.md` files: no `.mdx`, no images, no data files, no symlinks.
- Names and directories are lower-case kebab-case (`a-z`, `0-9`, `-`).
- A section overview is `_index.md`, never `index.md`: Hugo reads `index.md` as a leaf bundle that swallows its siblings.
- A page's path under `docs/site/` is its address under `/docs/`. `authoring/publish-a-module.md` publishes at `/docs/authoring/publish-a-module/`.

**Front matter.** YAML between `---` lines, starting on line 1. Only these keys are allowed:

| Key | Leaf page | `_index.md` overview | Value |
|---|---|---|---|
| `title` | required | required | non-empty string |
| `description` | required | required | a one-line, non-empty string (the index entry and search snippet) |
| `type` | required | **forbidden** (an overview declares no type, 0018:D7) | one of `tutorial`, `how-to`, `explanation`, `reference` (0018:D7:R1) |
| `weight` | optional | optional | a positive integer. Order within the section: lower first, then title. 0 means unset in Hugo |

Forbidden: `sidebar:` (write `weight: N`), and every other key (`slug`, `draft`, `template`, `hero`, `aliases` ...).

**Callouts** are GitHub alerts. The marker stands alone on its line and is one of NOTE, TIP, IMPORTANT, WARNING or CAUTION. A title is a bold first line, followed by an empty quote line:

```markdown
> [!NOTE]
> **Deploying your own module**
>
> Body text, which may hold several paragraphs, lists and code.
```

Never write `> [!NOTE] Title`, a foldable `> [!NOTE]-`, or a Starlight `:::note[...]` block.

**Figures** are Hugo shortcodes. Write each on its own line, with a blank line before and after, no parameters and no closing tag. Exactly these six names exist:

| Shortcode | Figure |
|---|---|
| `{{< opm/module-to-cluster >}}` | From module to running objects |
| `{{< opm/roles-and-artifacts >}}` | Three roles, three artifacts |
| `{{< opm/component-to-objects >}}` | Component to Kubernetes objects |
| `{{< opm/where-things-live >}}` | Where things live |
| `{{< opm/three-ways-to-deploy >}}` | Three ways to deploy |
| `{{< opm/helm-and-opm >}}` | Helm and OPM |

No other shortcode may appear in a source page: not Hextra's `callout`, `tabs`, `cards` or anything else. Hugo expands shortcodes even inside code fences, so to show one in a code block, write `{{</* opm/name */>}}`.

**Links.**
- Internal links are root-absolute with a trailing slash: `[What OPM is](/docs/start/what-is-opm/)`. A `#fragment` is allowed.
- Reference-style definitions (`[g]: /docs/start/`) follow the same rules. Footnotes (`[^1]: ...`) are not links.
- A's link hook resolves them in the current version and fails the build on a missing page.
- External links are `https://...`; `mailto:` and `#fragment` links are fine.
- Forbidden: relative links (`../x/`, `./x`), `.md` links, links with a version prefix (`/v1.0/docs/...`), `/docs/...` without the trailing slash, and raw HTML `href=` or `src=` attributes.

**Code fences** carry a language tag; use `text` as the tag for plain text.

**Also forbidden:**
- `import ... from` lines, inside code fences too (O4: "any");
- any line starting with `:::`, inside code fences too (O4: "any");
- JSX or component tags such as `<ModuleToCluster />`;
- images, as `![...]` or `<img>` (0018:D14 keeps figures in the engine).

**Allowed.** HTML planning comments (`<!-- ... -->`). A keeps them out of every published output: the HTML, `llms.txt` and the Markdown outputs.

**Trade-off, accepted by the owner.** github.com renders the alerts but shows `{{< opm/... >}}` as literal text.

**Rules beyond O4's list.** O4 names the lint's rules; the contract adds the key allowlist (which forbids `aliases`, `draft` and `slug`), the link rules, the ban on images and non-`.md` files, kebab-case names, `weight` >= 1 (0018's `#Page` allows 0; Hugo reads 0 as unset), the exact alert marker, and tagged code fences. Their bases (0018:D7, 0018:D7:R3, 0018:D14 and Hugo behaviour) are listed in A's `design.md`. Supervisor ruling 2026-09-30, under the owner's "rewrite it to how it needs to be": all of them hold. `draft` stays forbidden because a draft is a silently dropped page (Q2). `aliases` stays forbidden while nothing is published; allowing it for a moved page after go-live is a contract change (a planning commit that updates every copy of this file and the lint).

### 4.1 The dialect lint and the page-order helper (POSIX shell tools)

Two scripts. Write each block, byte for byte, with your file-writing tool, to a scratch file outside any repo.

**The lint**, for example `<your scratchpad>/opm-dialect-lint.sh`. `sha256sum` must print `dae9717af0c43fc3bdc29a9e730fd171fe7541dab35a9625a35efb1682973c6b`. A commits the same bytes as `opmodel.dev/site/scripts/lint-sources.sh` and runs it before every build.

````sh
#!/bin/sh
# opm-dialect-lint: check source pages against the OPM page dialect
# (orchestration.md, "Dialect contract"). POSIX sh and awk, plus find, grep -E,
# sort and mktemp (GNU, BSD and busybox all work). Runs on the host and in Alpine.
#
#   sh opm-dialect-lint.sh DIR [DIR ...]    lint each docs/site tree; exit 0 clean, 1 violations
#
# Every violation prints as "<file>:<line>: <message>".
set -u
export LC_ALL=C
FIGURES="module-to-cluster roles-and-artifacts component-to-objects where-things-live three-ways-to-deploy helm-and-opm"

[ $# -ge 1 ] || { echo "usage: $0 DIR [DIR ...]" >&2; exit 2; }

out=$(mktemp); trap 'rm -f "$out" "$out.files"' EXIT
for dir in "$@"; do
  [ -d "$dir" ] || { echo "$dir: not a directory" >> "$out"; continue; }
  find "$dir" \( -type f -o -type l \) | sort > "$out.files"
  while IFS= read -r f; do
    rel=${f#"$dir"/}
    if [ -L "$f" ]; then echo "$f:0: symlink; a page must be a regular file" >> "$out"; continue; fi
    case "$rel" in
      *.mdx) echo "$f:0: MDX file; rename to .md and replace components with {{< opm/... >}} shortcodes" >> "$out"; continue ;;
      index.md|*/index.md) echo "$f:0: index.md is a leaf bundle in Hugo; a section page is _index.md" >> "$out" ;;
      *.md) ;;
      *) echo "$f:0: not a page; docs/site holds only .md files (no images or data, 0018:D14)" >> "$out"; continue ;;
    esac
    if ! printf '%s\n' "$rel" | grep -Eq '^([a-z0-9]+(-[a-z0-9]+)*/)*(_index|index|[a-z0-9]+(-[a-z0-9]+)*)\.md$'; then
      echo "$f:0: file and directory names are lower-case kebab-case (a-z, 0-9, -)" >> "$out"
    fi
    case "$rel" in _index.md|*/_index.md|index.md|*/index.md) leaf=0 ;; *) leaf=1 ;; esac
    awk -v F="$f" -v LEAF="$leaf" -v FIGS=" $FIGURES " -v Q="'" '
      function err(l, m) { printf "%s:%d: %s\n", F, l, m }
      function unq(s,  a, z) { sub(/[ \t]+#.*$/, "", s); sub(/[ \t]+$/, "", s)
                        a = substr(s, 1, 1); z = substr(s, length(s), 1)
                        if (length(s) >= 2 && a == z && (a == "\"" || a == Q)) s = substr(s, 2, length(s) - 2); return s }
      function dest(t) {
        if (t ~ /^(https?:|mailto:|#)/) return
        if (t ~ /^\/docs\//) { if (t !~ /^\/docs\/([a-z0-9-]+\/)*(#[^ ]*)?$/) err(NR, "internal link \"" t "\": write /docs/<section>/<page>/ with a trailing slash"); return }
        err(NR, "link \"" t "\": internal links are root-absolute /docs/<section>/<page>/ (no relative, .md or version-prefixed links)")
      }
      NR == 1 { if ($0 != "---") { err(1, "front matter must open on line 1 with ---"); nofm = 1 } else { infm = 1; next } }
      infm {
        if ($0 ~ /^---[ \t]*$/) { infm = 0; fmdone = 1; next }
        if ($0 ~ /^[A-Za-z_][A-Za-z0-9_-]*:/) {
          k = $0; sub(/:.*/, "", k); v = $0; sub(/^[^:]*:[ \t]*/, "", v)
          line[k] = NR; val[k] = v
          if (k == "sidebar") err(NR, "sidebar: is Starlight front matter; write weight: N")
          else if (k !~ /^(title|description|type|weight)$/) err(NR, "front-matter key \"" k "\" is not allowed (title, description, type, weight)")
        } else if ($0 !~ /^[ \t]/ && $0 !~ /^#/ && $0 != "") err(NR, "front matter: expected \"key: value\"")
        next
      }
      {
        # Shortcodes are expanded even inside code fences, so check every line.
        s = $0
        while (match(s, /[{][{][<%][ \t]*\/?[ \t]*[^ \t>%}]*/)) {
          tok = substr(s, RSTART, RLENGTH); rest = substr(s, RSTART + RLENGTH); s = rest
          if (tok ~ /^[{][{][<%][ \t]*\/[*]/) continue
          name = tok; sub(/^[{][{][<%][ \t]*\/?[ \t]*/, "", name)
          if (name !~ /^opm\//) { err(NR, "shortcode \"" name "\": source pages use only {{< opm/<figure> >}}"); continue }
          fig = substr(name, 5)
          if (index(FIGS, " " fig " ") == 0) { err(NR, "unknown figure shortcode \"" name "\""); continue }
          if (tok !~ /^[{][{]<[ \t]*opm/ || rest !~ /^[ \t]*>[}][}]/) err(NR, "write the figure as {{< " name " >}} (no parameters, no closing tag)")
        }
      }
      # O4: any ::: line and any import ... from line fails, inside code fences too.
      /^[ \t]*:::/ { err(NR, "Starlight aside (:::); write a GitHub alert: > [!NOTE]") }
      /^[ \t]*import[ \t].*[ \t]from[ \t]/ { err(NR, "MDX import line") }
      /^ ? ? ?(```|~~~)/ {
        m = $0; sub(/^ */, "", m); c = substr(m, 1, 3)
        if (fence == "") {
          fence = c; tag = m; sub(/^[`~]*[ \t]*/, "", tag)
          if (tag == "") err(NR, "code fence without a language tag; write ```text for plain text")
          next
        } else if (c == fence) { fence = ""; next }
      }
      fence != "" { next }
      /^[ \t]*<[A-Z][A-Za-z0-9]*([ \t][^>]*)?\/?>[ \t]*$/ { err(NR, "component tag; use {{< opm/<figure> >}}") }
      /^[ \t]*>[ \t]*\[!/ {
        if ($0 !~ /^> \[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\]$/) err(NR, "alert marker must be exactly \"> [!NOTE]\" (or TIP, IMPORTANT, WARNING, CAUTION) alone on its line")
      }
      /!\[/ || /<[Ii][Mm][Gg][ \t>\/]/ { err(NR, "image; docs/site pages carry no images (0018:D14)") }
      /[Hh][Rr][Ee][Ff][ \t]*=|[Ss][Rr][Cc][ \t]*=/ { err(NR, "raw HTML link or source; write a Markdown link [text](/docs/<section>/<page>/)") }
      /^ ? ? ?\[[^]^][^]]*\]:/ {
        t = $0; sub(/^ ? ? ?\[[^]]+\]:[ \t]*/, "", t); sub(/[ \t].*$/, "", t); sub(/^</, "", t); sub(/>$/, "", t)
        if (t != "") dest(t)
      }
      {
        s = $0
        while (match(s, /[]][(][^) \t]*/)) {
          t = substr(s, RSTART + 2, RLENGTH - 2); s = substr(s, RSTART + RLENGTH)
          dest(t)
        }
      }
      END {
        if (nofm) exit
        if (!fmdone) { err(1, "front matter is not closed with ---"); exit }
        if (!("title" in val) || unq(val["title"]) == "") err(1, "missing title")
        if (!("description" in val) || unq(val["description"]) == "") err(1, "missing description")
        if (LEAF == 1) {
          if (!("type" in val)) err(1, "missing type (tutorial, how-to, explanation or reference)")
          else { t = unq(val["type"]); if (t !~ /^(tutorial|how-to|explanation|reference)$/) err(line["type"], "invalid type \"" t "\"") }
        } else if ("type" in val) err(line["type"], "a section overview (_index.md) declares no type (0018:D7)")
        if ("weight" in val) { w = unq(val["weight"]); if (w !~ /^[1-9][0-9]*$/) err(line["weight"], "weight must be a positive integer") }
      }' "$f" >> "$out"
  done < "$out.files"
  rm -f "$out.files"
done
if [ -s "$out" ]; then
  cat "$out"; echo "opm-dialect-lint: $(wc -l < "$out" | tr -d ' ') violation(s)"; exit 1
fi
echo "opm-dialect-lint: OK ($*)"
````

**The page-order helper**, for example `<your scratchpad>/opm-page-order.sh`. `sha256sum` must print `edc62591eb019efc23b5c106d3b91e76620bb6e6a7ac47b3bde084c7857b5f65`. It is for S workers only: it still reads the old `sidebar.order`, so nobody commits it anywhere (O4 forbids a `sidebar.order` reader in A).

````sh
#!/bin/sh
# opm-page-order: print "<page>\t<order>" for one docs/site tree, sorted by page, so the
# tree before and after the dialect change (orchestration.md 4.1) diffs cleanly.
# The order is weight, or the old sidebar.order; the page name maps index.md and
# index.mdx to _index.md and .mdx to .md. S workers only; nothing commits it.
#
#   sh opm-page-order.sh DIR
set -u
export LC_ALL=C
[ $# -eq 1 ] && [ -d "$1" ] || { echo "usage: $0 DIR" >&2; exit 2; }
(cd "$1" && find . -type f \( -name '*.md' -o -name '*.mdx' \) | sed 's|^\./||') |
while IFS= read -r f; do
  n=$(printf '%s' "$f" | sed -E 's#(^|/)index\.mdx?$#\1_index.md#; s#\.mdx$#.md#')
  o=$(awk 'NR==1 && $0!="---" {exit} NR==1 {next} /^---[ \t]*$/ {exit}
           /^weight:/ {v=$0; sub(/^weight:[ \t]*/,"",v); print v; exit}
           /^sidebar:/ {s=1; next} s && /^[ \t]+order:/ {v=$0; sub(/^[ \t]+order:[ \t]*/,"",v); print v; exit}
           /^[^ \t]/ {s=0}' "$1/$f")
  printf '%s\t%s\n' "$n" "${o:--}"
done | sort
````

**How an S worker runs them.** Use literal paths; `<wt>` is your worktree, `<scratch>` your scratch directory.

```sh
sh <scratch>/opm-dialect-lint.sh <wt>/docs/site                  # must print "opm-dialect-lint: OK (...)"
mkdir -p <scratch>/before
git -C <wt> archive origin/main docs/site | tar -x -C <scratch>/before
sh <scratch>/opm-page-order.sh <scratch>/before/docs/site > <scratch>/order-before.txt
sh <scratch>/opm-page-order.sh <wt>/docs/site > <scratch>/order-after.txt
diff <scratch>/order-before.txt <scratch>/order-after.txt && echo "order unchanged"
```

- The helper maps the renames (`index.md` and `index.mdx` to `_index.md`, `.mdx` to `.md`) and sorts after mapping them. An empty diff therefore proves that every `sidebar.order` became the same `weight`, and that no page appeared or vanished.
- On `origin/main` before your change (2026-09-30), the lint reports these findings: opm 14, core 13, catalog_opm 8, cli 5, library 8 (on 0cde6ce, after `colliding-contracts.md` merged), opm-operator 5. After your change it must report none. The S recipe applied mechanically to a copy of all six trees passes the lint with an unchanged page order, under gawk `--posix`, busybox awk and shellcheck.
- S workers do not build the site; A renders every page.

## 5. Source trees for site builds (the blocker fix)

- **Wave 1.** The six S workers only lint; they never build the site. A's section 1 builds a fixture workspace inside its own worktree (`site/tests/fixtures/ws`).
- **After S1-S6 merge.** The supervisor creates one detached worktree per source repo at `origin/main`, named `site-src`. It refreshes them after later source merges, and nobody else touches them:
  - `WS/opm/.claude/worktrees/site-src`
  - `WS/core/.claude/worktrees/site-src`
  - `WS/catalog_opm/.claude/worktrees/site-src`
  - `WS/cli/.claude/worktrees/site-src`
  - `WS/library/.claude/worktrees/site-src`
  - `WS/opm-operator/.claude/worktrees/site-src`
- **Builds in opmodel.dev** (A from section 2, and B, C, D, E, M, F) read those trees. Prefix each build command with `OPM_SRC_WORKTREE=site-src` (interface in section 6), for example `OPM_SRC_WORKTREE=site-src task -d <wt> build`. Never build against the owner's main checkouts: they are not kept up to date.
- **Isolation.** Each opmodel.dev worker builds in its own worktree, so all generated state (`site/public`, generated config, `site/data/opm`, `site/.check`, `site/.shots`) belongs to that worktree alone.
- **Dev-server ports** (`SITE_PORT`), only if you need `task serve` or `task preview`: A 1313, B 1314, C 1315, D 1316, M 1317. `task qa` publishes no port.

## 6. Interface A (what wave 2 codes against)

A may refine names only by reporting them under `deviations` and getting the supervisor's OK. Wave-2 changes code against what A's merged `main` contains. Until A's section 5, each task below is named `hugo:<name>` (`hugo:build`, `hugo:test:site`, `hugo:qa` ...), and the bare names are still the old Astro tasks, which nobody runs (section 10).

**Tasks** (run from the opmodel.dev worktree root, or `task -d <wt> <name>`):

| Task | What it does |
|---|---|
| `task image` | Builds `site/Dockerfile` (Alpine by digest, Hugo 0.167.0, Pagefind 1.5.2, git, all SHA-256-checked) as `opmodel-dev-hugo:<first 12 hex of sha256(site/Dockerfile)>`, if that tag is missing. It needs the network |
| `task versions:prepare` | Host-side step that `build` and `serve` run first. A ships it as a no-op. B fills it (resolve refs, `git archive`, dates against refs), because those need the host: a worktree's `.git` file points at a path the container does not mount |
| `task build` | `versions:prepare` on the host, then in the image, with `--rm --init --network none --user <uid>:<gid>`: the source lint, then generated mounts, dates and stamp, then `hugo build --panicOnWarning`, every check, and Pagefind per version. Output: `site/public/` |
| `task serve` | `versions:prepare`, then the dev server on `127.0.0.1:${SITE_PORT:-1313}`, reading the sources in place with live reload. `--init` with `TINI_KILL_PROCESS_GROUP=1` and `exec hugo server`. Runs with no TTY (`</dev/null`); one SIGINT stops the container |
| `task preview` | Serves the built `site/public/` on `SITE_PORT` |
| `task lint:sources` | The section 4.1 lint over the six source roots |
| `task test:site` | Regression tests proving every check fails when it should. It builds what it needs |
| `task shots` | Screenshots of every page with a figure, plus extras, in six variants (light, dark, site-dark/OS-light, site-light/OS-dark, phone light, phone dark) into `site/.shots/<page>/<n>-<variant>.png`. Fails if any SVG text is under 9 px at 390 px |
| `task qa:image` | Builds the QA image (`opmodel-dev-qa:<hash>`) if that tag is missing. It needs the network; `task qa` itself runs with `--network none` |
| `task qa` | `shots`, the axe a11y smoke test and the search smoke test, in `opmodel-dev-qa:<hash of site/tests/browser/Dockerfile>` (Playwright python 1.63.0 by digest, axe-core 4.10.3 from a SHA-256-checked tarball). No published port |
| `task ci` | `check`, `image`, `build`, `test:site` |
| `task check` | Go `fmt`, `vet` and `test`, plus `openspec:check` |
| `task clean` | Removes generated paths only |
| `task ci:lint` | Added by E, not A: actionlint from a digest-pinned image over `.github/workflows/` |

**Environment variables.**

| Variable | Meaning | Default |
|---|---|---|
| `OPM_WS` | Workspace root | The parent of the opmodel.dev main checkout, from `git rev-parse --path-format=absolute --git-common-dir`, so it is right inside a worktree |
| `OPM_SRC_OPM`, `OPM_SRC_CORE`, `OPM_SRC_CATALOG_OPM`, `OPM_SRC_CLI`, `OPM_SRC_LIBRARY`, `OPM_SRC_OPM_OPERATOR` | One source repo root each: a checkout or a worktree. The name is `OPM_SRC_` plus the repo name upper-cased, with `-` as `_` | `$OPM_WS/<repo>/.claude/worktrees/$OPM_SRC_WORKTREE` if `OPM_SRC_WORKTREE` is set, else `$OPM_WS/<repo>` |
| `OPM_SRC_WORKTREE` | Worktree name used for every unset `OPM_SRC_<REPO>` | unset |
| `SITE_PORT` | Host port for `serve` and `preview` | `1313` |
| `OPM_REQUIRE_DATES` | `1` fails the build when a page has no git date (CI sets it) | `0` |
| `OPM_BUILD_REFS` | `repo=sha ...`, resolved on the host by the Taskfile (`git rev-parse HEAD` works in worktrees there) | set by the Taskfile; never set by hand |
| `OPM_VERSIONS` | Internal seam: `name=root ...`, where each root holds `<repo>/docs/site`. B's resolver feeds it. `run-in-image.sh` passes it into the container only when the caller set it. After B merges, a fixture build (a source root that is not its own git top level) must set `OPM_VERSIONS=v1.0=/src` explicitly | `v1.0=/src` |
| `OPM_VERSIONS_MANIFEST` | Added by B: path to an alternative versions manifest (tests) | `site/versions.yaml` (B) |
| `SITE_DIR` | Container path of the site tree the scripts act on; `test:site` points it at per-case copies under `site/.check/tests/<case>/site/` | `/work/repo/site` |

**Container paths.** The opmodel.dev worktree is mounted at `/work/repo` (read-write). Each source root is mounted read-only at `/src/<repo>`, with no `:z`.

**Outputs.**
- `site/public/`:
  - `v1.0/...`, including per-version `pagefind/`, `sitemap.xml`, `llms.txt` and `404.html`;
  - `latest/...` meta-refresh stubs;
  - `_redirects` (`/ /latest/ 302`, `/latest/* /v1.0/:splat 302`);
  - a root `index.html` refresh to `/latest/`, a root `404.html` and `robots.txt`;
  - `build-stamp.json`, holding the source SHAs.
- `site/.check/<version>/nav-order.txt`: the sidebar link order, one URL per line.
- `site/.shots/`.
- Gitignored generated inputs: `site/config/{production,development}/module.toml`, `site/data/opm/{lastmod,build}.json`, `site/.gen/<v>/` (reserved for the generated reference) and `site/.versions/<v>/` (B).
- The build summary prints the page count per version and the total file count.

**Versions.**
- One version, `v1.0`, labelled "v1.0 (beta)", built from each source repo's `main`, with `defaultContentVersionInSubdir=true`.
- `/latest/` points at the default version.
- B makes the list manifest-driven; nothing may glob `v*/` or "every top-level directory".
- `/reference-archive/` is reserved.

**CSS.** Every `site/assets/css/opm/*.css` is concatenated in lexical file-name order in `layouts/_partials/custom/head-end.html`, then minified and fingerprinted. Add your own file; there is no list to edit. Existing files belong to their owners: `figures.css` to C; `versions.css` to B; `typography.css`, `toc.css`, `cards.css` and `landing.css` to D; `brand.css` to M.

**Partials and hooks.**

| File | Contract | May edit |
|---|---|---|
| `_partials/opm/figure.html` | The figure frame: dict `id`, `title`, `claim` (caption = accessible label), `width`, `height`, `body`. Tokens `--opm-fig-*` follow `html.dark` | C |
| `_partials/opm/figures/<name>.html` and `_shortcodes/opm/<name>.html` | One per figure in section 4 | C |
| `_partials/opm/section-children.html` | A section's child list, ordered by `weight`, then title | D |
| `_partials/opm/source.html` | Maps a page to `{repo, path, editURL, viewURL}` from `/src/<repo>/docs/site/`. "Edit" goes to `edit/main`; "View source" appears only for a version mapped to a ref | B |
| `_partials/opm/build-stamp.html`, called from the Hextra hook `_partials/custom/footer.html` | The footer stamp | B |
| `_partials/opm/{version-switch,version-links}.html`, `_partials/navbar-title.html`, `_partials/banner.html` | Version switch and outdated bar. `navbar-title.html` keeps upstream's `params.navbar.displayTitle` and logo `alt` text, so the logo and title are set through params (M) | B |
| `_partials/opm/docs-main.html`, `_partials/custom/content-begin.html` | Docs layout body, type badge, Pagefind metadata | D |
| `_partials/sidebar.html` | One tree; orders by `weight`, then title; honours `sidebar.exclude` from a site-owned `cascade` | D (fix 8 only) |
| `_partials/components/last-updated.html` | Edit link and git date | B |
| `layouts/_markup/render-link.html` | Resolves `/docs/...` in the current version; a miss fails the build | nobody in wave 2 |
| `layouts/_markup/render-codeblock-cue.html` | The Chroma CUE `""` workaround | nobody |

**Theme.** Hextra v0.13.0 (`adf732f8d97cb8e149d4aba232a449d41cd9e38c`) is vendored in `site/themes/hextra`, with its hash in `hextra.COMMIT`: the runtime tree only (`layouts/`, `assets/`, `static/`, `i18n/`, `data/`, `theme.toml`, the theme `hugo.toml`, `LICENSE`), never upstream's `CLAUDE.md`, `AGENTS.md` or npm tooling. `site/overrides.sha256` pins the upstream file behind every override copy. A new copy adds a line; editing our own override adds none.

**More contract details A fixes.**
- `navbar-title.html` keeps the class hooks `opm-brand`, `opm-title-long` and `opm-title-short`; M's `brand.css` selects them and B keeps them.
- Screenshot numbering: `<n>` in `site/.shots/<page>/<n>-<variant>.png` counts only drawn figures, in page order; a "Figure pending" stub takes no number. `.shots` is overwritten on every run.
- `check-overrides.sh --update` re-pins exactly the paths already listed in `overrides.sha256`; a new override copy is added by appending its line first.
- `site/config/_default/hugo.toml` sets `[minify] minifyOutput = true` (drops HTML comments) and `disableSVG = true`: inline figure SVG passes through byte for byte (the SVG minifier trims the space before a `<tspan>`).

**Checks** (each fails the build; keep them green):
1. The drift guard (`check-overrides.sh`).
2. The source lint.
3. Front-matter validation inside Hugo (`errorf`: title, description, type rules; this covers site-owned pages too).
4. Q1 broken internal links.
5. A1: two sources publishing one URL.
6. Q2: an expected page is missing, or an unexpected page appears.
7. Stray output files.
8. Reserved prefixes: no source page under `docs/reference/cli/` or `docs/reference/definitions/`.
9. A raw `:::` in the output.
10. A planning comment in any published text output (`*.html`, `*.txt` such as `llms.txt`, `*.md`, `*.xml`, `*.json`).
11. Supply chain: no third-party or CDN URL in the output.
12. The redirect files are present.
13. Git dates, when `OPM_REQUIRE_DATES=1`.

## 7. Worker protocol

1. **Read.**
   - This file.
   - Your repo's `AGENTS.md`, and `openspec/config.yaml` if it exists.
   - For an OpenSpec change: `proposal.md`, `design.md` and `tasks.md` from your worktree, plus `cd <wt> && openspec instructions apply --change <change-id> --json`, and the repo-local apply skill `<wt>/.claude/skills/openspec-apply-change/SKILL.md`. Never the root `/opsx:*` router (section 1).
   - For a plain row: section 3 here.
   - The repo's own rules on commits and PRs override nothing in section 1; they add to it.
2. **Create your own worktree.** Workflow worktree isolation cannot be used: it would worktree the workspace repo, not your repo. Use literal absolute paths:
   ```sh
   git -C WS/<repo> fetch origin
   git -C WS/<repo> worktree add WS/<repo>/.claude/worktrees/<change-id> -b <branch> origin/main
   ```
   For the workspace-root rows, `<repo>` is WS itself: `git -C WS worktree add WS/.claude/worktrees/<change-id> -b <branch> origin/main`. Work only there, by absolute path. Use `git -C <wt> ...` and `task -d <wt> ...`, and never `cd` into another checkout.
3. **Follow `tasks.md` section by section**, or the section list in section 3 for a plain row. Tick each box when its task is done.
4. **Close every section green, with one commit.**
   - Run the section's gates on the whole worktree (section 10, plus what `tasks.md` names).
   - Stage explicit paths (`git -C <wt> add <path> ...`).
   - Commit with exactly the named Conventional Commit as the subject. An optional body follows, then an empty line and the single trailer `Co-Authored-By: Claude <noreply@anthropic.com>`.
   - Scan the message for `@` before committing.
   - Never carry uncommitted work across a section boundary.
5. **On a blocker, or on anything that would change the interface in section 6, the contract in section 4, another change's files, or a decision recorded in `design.md`:** stop, report under `deviations`, and wait. Do not work around it in another change's files.
6. **After the last section, verify.** (A also reports after its section 1, with the same block and `sections: 1/5`, then waits: the S PRs merge on that evidence.)
   - OpenSpec rows: read `<wt>/.claude/skills/openspec-verify-change/SKILL.md` and follow it for `<change-id>`, running each `openspec` command as `cd <wt> && openspec ...`. Never the root `/opsx:verify` router: it would verify the owner's stale main checkout, which has neither your change directory nor your commits.
   - Plain rows: run the checklist verify (section 8).
   - Report to the supervisor with this block, then **STOP and wait for instructions**:
   ```
   change:      <ID> <repo> <change-id>
   branch:      <branch> @ <sha> (local, not pushed)
   sections:    <done>/<total>
   commits:     <sha> <subject>  (one line each)
   gates:       <command> -> pass|fail (reruns: n)  (one line each)
   verify:      <final assessment line>; critical n, warning n
   surface:     <interface names from section 6 this change adds, changes or relies on>
   deviations:  <none | what differs from design.md or this file, and why>
   follow-ups:  <none | work found that is outside this change>
   questions:   <none | what the supervisor must decide>
   ```
7. **On the supervisor's go.** (W0 skips this step: the supervisor pushes its commits to `main`.)
   - OpenSpec rows: archive the change in the worktree by following `<wt>/.claude/skills/openspec-archive-change/SKILL.md` (never the root `/opsx:archive` router, which would archive in the owner's main checkout), always with `--skip-specs` in this set: `cd <wt> && openspec archive <change-id> --yes --skip-specs`. Commit `chore(openspec): archive <change-id>`, then re-run `cd <wt> && openspec validate --all --strict --no-interactive` (opm-operator: skip this, see section 10). `--all` is deliberate here: after the archive, the change id is no longer an active change, so validating it by id fails. The archive rides the PR.
   - Push the branch: `git -C <wt> push -u origin <branch>`.
   - Open the PR with `gh pr create`. The title is the change's main Conventional Commit subject; the body follows section 1 and the repo's `AGENTS.md`.
   - Report the PR URL. S1 adds its item-10 line (how github.com renders the alert).
   - If the supervisor asks you to update the branch: `git -C <wt> fetch origin`, then `git -C <wt> merge --no-edit origin/main`, then `git -C <wt> push`, on your own branch only. Never rebase or force-push (section 1). The squash merge hides the merge commit.
8. **When told**, remove the worktree (`git -C WS/<repo> worktree remove WS/<repo>/.claude/worktrees/<change-id>`). If the worktree holds a read-only extract tree (library's `.cue-cache`), run `chmod -R u+w <wt>/.cue-cache` first; never `rm -rf` a worktree. After the merge, delete the local branch (`git -C WS/<repo> branch -D <branch>`).

## 8. Checklist verify (plain rows: W0, S1, I1a, I1b, I2)

Report in the `opsx:verify` shape: Summary, CRITICAL, WARNING, SUGGESTION, Final Assessment.

- [ ] Every item of the row's scope in section 3 is done, or is reported as not done, with the reason, as CRITICAL.
- [ ] `git -C <wt> diff --stat origin/main` touches only the row's "Touches" paths. Anything else is CRITICAL.
- [ ] Every gate in the row's table cell ran green; commands and results are listed.
- [ ] The whole diff was read line by line against the scope and the gates. Anything unexplained is WARNING.
- [ ] One commit per section with the named subject, the plain trailer only, and no bare `@name`. A merge commit from a branch update (section 7, step 7) is exempt.
- [ ] No `enhancement.yaml` anywhere in the diff.
- [ ] Items marked "after the PR opens" (S1 item 10) are left for the report, not checked here.

## 9. Supervisor protocol

**Wave 0.**
- Add `.claude/worktrees/` to `.git/info/exclude` in opm, core, catalog_opm, library, opmodel.dev, enhancements and WS. This is local and needs no commit; cli and opm-operator already ignore it.
- Run W0 through a worker. After its checklist verify, push its commits to main: `git -C <wt> push origin HEAD:main`.
- Then plan every OpenSpec change of the set, one planning worktree per repo, never through the root `/opsx:propose` router (it resolves to the owner's stale main checkout, and opmodel.dev's has no skills):
  - `git -C WS/<repo> fetch origin`, then `git -C WS/<repo> worktree add --detach WS/<repo>/.claude/worktrees/plan-hugo origin/main`. For opmodel.dev this happens after W0 is on `origin/main`, so the worktree has the skills.
  - Read `<planning-wt>/.claude/skills/openspec-propose/SKILL.md` and follow it with that worktree as the working directory (`cd <planning-wt> && openspec ...`).
  - Where the skill or `openspec/config.yaml` says to write `enhancement.yaml`, do not (O7). Name the related 0018 decisions in proposal prose only.
  - Each proposal's Impact carries `Depends on: <the Starts when and Merges when cells of section 2>`.
  - Copy this file verbatim into each change directory.
  - Verify: `find <planning-wt>/openspec/changes -name enhancement.yaml` prints nothing, and `cd <planning-wt> && openspec validate <change-id> --strict --no-interactive` passes.
- Commit each repo's plan in its planning worktree (`chore(openspec): plan <change-id>`) and push it with `git -C <planning-wt> push origin HEAD:main`. Planning commits go straight to main (owner decision O6). The supervisor never commits in a main checkout. Remove the planning worktree afterwards.

**Launching.**
- Wave 1: A (section 1 only, then its interim report, then wait), S1-S6, I1a, I2.
- Merge the S PRs only after A's section 1 report is green: the fixture build renders bold-title alerts, parameterless `opm/` shortcodes, a source `_index.md` ordered by `weight`, and root-absolute links through the link hook. If it finds a dialect problem, fix the contract and the S branches before any S merge.
- When S1-S6 are merged:
  - tell the owner that the Astro build on opmodel.dev `main` now renders degraded or fails, and that the S4-S6 merges open or grow release PRs in cli, library and opm-operator (the owner's to merge);
  - record the six S merge SHAs; B's manifest takes them as each repo's dialect floor;
  - create the six `site-src` worktrees (`git -C WS/<repo> fetch origin`, then `git -C WS/<repo> worktree add --detach WS/<repo>/.claude/worktrees/site-src origin/main`). Refresh one after a later source merge with `git -C WS/<repo>/.claude/worktrees/site-src checkout --detach origin/main`, after a fetch;
  - release A to section 2.
- After the owner merges A: launch E, B (with the six S merge SHAs), C, D, M and I1b. Launch F after E is merged and the owner prerequisites exist; confirm the secret names first with `gh secret list --env production` (names only).
- After the owner merges F: the push to main runs the first deploy. Verify the preview host by curl (`/` to `/latest/` to `/v1.0/`, a `/latest/*` page, 404, one page per version, the `noindex` header, and whether `_redirects` wins over the static `/latest/` stubs) and report to the owner. Fix a failure forward with a new small change, never by hand in Cloudflare.

**Planning-round rulings (2026-09-30).**
- A: `qa:image` and moving the site-owned content into section 1 are accepted deviations. A disables SVG minification (section 6).
- B: may add one line to A's `test:site` and adds `OPM_VERSIONS_MANIFEST`; fixture builds set `OPM_VERSIONS=v1.0=/src` after B.
- E and F: their spikes may use scratch clones outside WS (section 1).
- F: `noindex` stays on the `workers.dev` host permanently; only the custom domain is indexed. Binding the custom domain happens in a follow-up go-live change, not by hand. The extra checks (`wrangler dev` smoke test, a PR `deploy-check` job, the `check-deploy.sh` self-test, `deploy:lock`) are accepted; the owner reviews them when merging F.
- M: the `brand:favicons` task, `site/tools/favicons.py`, `params.images` and its `AGENTS.md` lines under A's headings are accepted.
- S2: the core gate swap in section 10 is accepted.

**Reviewing a report.**
- Compare `surface` and `deviations` with `design.md` and with sections 4 and 6.
- Re-run a gate yourself when in doubt.
- Send the worker back with a precise ask. Never fix a worker's branch yourself.

**Deviations.**
- Inside the change's scope, interface unchanged: accept it, and note it in the PR body if a reviewer needs it.
- Changes an interface name, env var, path, check or dialect rule: accept only if no change that is already coding against it breaks. Then update every copy of this file with a planning commit per repo, tell every active worker, and record it in the changing change's `design.md`. The lint in section 4.1 (`opm-dialect-lint.sh`) and A's `site/scripts/lint-sources.sh` must stay byte-identical; the page-order helper is never committed.
- Scope growth: split it into a follow-up change. Never absorb it.
- Anything that touches an owner decision (versions, URLs, host, dialect, merge authority): page the owner.

**Merging.**
- The supervisor merges W0 (a push to main), S1-S6 (after A section 1 is green), I1a, I2, E (after its workflow is green on the PR), B, C, D, M (after E, and after the owner approves the marks) and I1b, each only after a green verify and its own review. Use the repo's usual squash merge.
- The owner merges A and F, and any release PR the S merges open.
- After a merge, tell the worker to remove its worktree and delete its local branch.
- Any change that adds a `docs/site` page after S merges must write the new dialect; otherwise A's lint fails the site build. (Library `refuse-colliding-contracts` merged in the old dialect before planning; S5 converts its page. Core `fold-colliding-contract-keys` merged as PR 80 without a new page.) Before launching an S worker, fetch its repo and re-run the lint on `origin/main`: a page that landed after planning is converted by the S worker under its late-page task.

**Live Astro container.**
- Container `gracious_jemison`, from image `opmodel-dev-site:local`, on port 4321, with the opmodel.dev main checkout mounted. It ran `--rm`; at 12:46 on 2026-09-30 `docker ps -a` no longer listed it. The rules below still hold: the owner may start it again.
- Never stop, remove or rebind it. Never touch its image.
- After A merges, never pull or fast-forward the opmodel.dev main checkout until the owner confirms the container is stopped.

**Page the human (owner) for:**
- nothing before wave 1: the round-1 items were settled on 2026-09-30 (dialect extras: supervisor ruling in section 4; F pre-merge preview: not planned). The supervisor TELLS the owner, without asking, that the S4-S6 merges open or grow release PRs in cli, library and opm-operator, and that `v1.0` stays on `main` until every repo has a tag cut after its S merge (core and catalog_opm hide `docs` commits, so they need another releasable commit or a `Release-As:` footer);
- reviewing and merging A and F;
- the Cloudflare account and project, `CLOUDFLARE_API_TOKEN`, `CLOUDFLARE_ACCOUNT_ID` and the `production` environment, before F section 1;
- the result of F's first deploy on the preview host;
- the DNS step (Namecheap zone export, nameserver move, custom-domain binding, go-live), on the owner's go; the supervisor recommends having C, D, M and F merged first;
- approving M's marks;
- confirming the Astro container is retired;
- filing the upstream issues the agents draft (the Chroma CUE lexer, Hextra's breadcrumb `nav`);
- any gate failure not listed as known in section 10;
- anything irreversible or outward-facing beyond PRs: secrets, environments or repo settings; Cloudflare resources; tags and releases; deleting remote branches; any push to main other than planning commits; stopping containers; pruning images; pulling or fast-forwarding a main checkout.

## 10. Gate commands per repo (read from each repo's Taskfile.yml or Makefile, 2026-09-30)

Run each from your worktree with `task -d <wt> ...`, `make -C <wt> ...` or `git -C <wt> ...`. The `openspec` CLI reads the current directory, so run it as `cd <wt> && openspec ...`; `cd` into your own worktree is fine. As of 2026-09-30, `openspec validate --all --strict --no-interactive` is green in core (15 items), library (17) and cli (61), and catalog_opm has no items. opm-operator is the exception below.

| Repo | Gates for changes in this set | Known pre-existing failures and quirks |
|---|---|---|
| opmodel.dev | `task check` (Go fmt/vet/test, plus `openspec:check` after W0). After A's section 5: `task ci`; `task qa` for visual changes; your `tasks.md` gates. Before that inside A: `task hugo:build`, `task hugo:test:site`, `task hugo:qa` | Until A's section 5 lands, the bare `image`, `build`, `serve`, `preview`, `shots` and `shots:image` are the old Astro tasks. Never run any of them: `image` and `shots:image` retag `opmodel-dev-site:local` and `opmodel-dev-shots:local`, and `serve` and `preview` publish port 4321. `task check`, `task clean`, `task generate*` and the `hugo:*` tasks are fine |
| opm | No Taskfile or Makefile exists (its `AGENTS.md` names `task fmt` and `task vet`, but they are gone). Gates: the section 4.1 lint, the order diff, `git -C <wt> diff --check` | none |
| core | The lint and order diff; `task fmt:check vet spec:check docs:check`, plus the `INDEX.md` diff with the title line normalised, in place of `task check` (in a worktree `generate:index:check` differs only in the title, which carries the worktree name); `openspec validate adopt-hugo-page-dialect --strict --no-interactive` | `task fmt:check` diffs the working tree against the git index, so stage your changes first |
| catalog_opm | The lint and order diff; `task check`; `openspec validate adopt-hugo-page-dialect --strict --no-interactive` | none known |
| cli | The lint and order diff; `task openspec:check` (the repo's own gate, `--all --strict`, green today with 61 items; kept because it is what cli's CI runs); `task vet`; `task test:unit` | Do **not** run `task check` or `task test`: they include integration and e2e tests against the kind cluster, which a docs-only change does not need |
| library | The lint and order diff; `task vet` (plain `go vet`); `openspec validate adopt-hugo-page-dialect --strict --no-interactive` | Do **not** run `task test` for this docs-only change: in a fresh worktree it fetches `.cue-cache` cold into a read-only extract tree that makes `git worktree remove` fail (section 7, step 8), and `TestGenerate_BuildsThroughTheKernel` flakes in full-suite runs |
| opm-operator | The lint and order diff; `make -C <wt> fmt vet` (the Makefile; `task -d <wt> dev:fmt dev:vet` is the same); `openspec validate adopt-hugo-page-dialect --strict --no-interactive` | 19 of 44 main specs fail `--strict`, so never use `--all` there. The Makefile has no `test` target; `task dev:test` downloads envtest and is not required for a docs-only change |
| enhancements | `task vet` (hard gate); `task check ID=0018` (soft) | none known |
| workspace root | `git -C <wt> diff --check` and the checklist | no check task for docs |

## 11. Risks and traps implementers must know

**Hugo (most fail silently: exit 0, wrong output).**
1. **`index.md` is a leaf bundle.** It swallows sibling pages. Section pages are `_index.md`, in source repos and in opmodel.dev alike.
2. **An invalid `type:` silently picks another layout.** Hence the lint and `errorf`.
3. **`hugo build --quiet` hides the ERROR line of a failed build.** Never use `--quiet`.
4. **`.Site.Data` is deprecated and fails under `--panicOnWarning`.** Use `hugo.Data`.
5. **Globs changed in Hugo 0.166.** `**/x` does not match `x`, and `a/**/b` does not match `a/b` (use `{**/,}x`). In version globs `*` does not cross `.`, so use exact names like `v1.0`.
6. **Hugo drops a mount root that is a symlink.** Mount real directories.
7. **The default `baseURL` is `https://example.org/`.** Set it explicitly.
8. **`eq 1 1.0` is true.** Never compare version strings as numbers.
9. **Static files, CSS and JS publish once at the root.** Pages, `llms.txt`, sitemaps and `404.html` publish per version.
10. **Hextra fetches Mermaid, KaTeX, MathJax, FlexSearch, PhotoSwipe, asciinema and medium-zoom from jsDelivr when enabled.** Keep them off. The build runs `--network none`, and the supply-chain check fails on CDN URLs.
11. **Pagefind indexes only `main#content > .content`.** Changing that DOM (page lead, cards) must keep the search smoke test green.
12. **`--primary-lightness` on one element does not recolour Hextra there.** The `--hx-color-primary-*` values are computed on `:root`. Unlayered site CSS also beats Hextra's Tailwind v4 `@layer utilities`.
13. **Hugo expands shortcodes inside code fences.** Escape them as `{{</* ... */>}}`.

**Theme.**
14. **The drift guard.** `site/overrides.sha256` pins every upstream file behind an override copy, and `check-overrides.sh` fails on any upstream change. The first v0.13.0 build fails on `render-link.html` (upstream #1037/#1039): merge that hunk, then re-pin. Never run `--update` just to turn a build green. Diff upstream first.
15. **Internal contracts.** `window.hextraSearch` (the Pagefind adapter) and the sidebar class contract (`.hextra-sidebar-*`, `li.open`) are internal to Hextra, guarded by hash and by no API promise. Do not adopt Hextra v0.14 or the #1006 sidebar rewrite in this set.
16. **The Chroma bug.** Chroma 2.27.0, bundled in every Hugo release including 0.167.0, has a CUE lexer that treats `""` as an unterminated string. Keep `render-codeblock-cue.html` and its test.
17. **Geist Mono ligatures turn `>=` into `≥`.** They stay off for `code` and `pre`.

**Docker, worktrees and the host.**
18. **Docker only.** hugo, pagefind, npm, node and Playwright run in images. Builds run `--network none`; only `task image` needs the network.
19. **The live Astro container.** It holds port 4321 when running (section 9). Hugo uses 1313 and the per-worker ports.
20. **Image tags come from the Dockerfile hash.** They are `opmodel-dev-hugo:<12 hex>` and `opmodel-dev-qa:<12 hex>`, because parallel worktrees share one Docker namespace and a fixed tag would let one worker replace another's image. Never reuse `opmodel-dev-site:local`, `opmodel-dev-shots:local` or `opm-hextra-build`.
21. **A worktree's `.git` is a file pointing at a host path.** Inside a container, `git` fails there. gen-lastmod swallows that error, so dates silently go missing (as they do for shallow clones and for git's dubious-ownership refusal). CI sets `OPM_REQUIRE_DATES=1`. Source SHAs are resolved on the host.
22. **A Docker bind mount of a missing host path creates a root-owned empty directory.** Check every mount source exists. The stray root-owned `WS/site/` is one; leave it alone.
23. **Do not put `:z` on workspace or source mounts.** It would relabel whole trees.
24. **`.claude/worktrees/` is not gitignored in opm, core, catalog_opm, library, opmodel.dev (until W0), enhancements or WS.** Stage explicit paths.
25. **The owner's main checkouts are not kept current.** Build against `site-src` (section 5), read skills and source files from your worktree or from `origin/main` (`git -C <repo> show origin/main:<path>`), and `git fetch` before `worktree add`.
26. **`task` from inside a worktree.** `{{.ROOT_DIR}}/..` is `.claude/worktrees/`, not WS. That is why `OPM_WS` is derived from the git common directory.

**Sources and versions.**
27. **After S1-S6 merge, a new page in the old dialect breaks the site build.** The lint names file and line. Fix the page, never the lint.
28. **Pins sit in four places.**
    - library in `cli/go.mod`;
    - core in library `opm/schema/loader.go` `DefaultSchemaModule`;
    - the operator in cli `internal/operator/manifest.go` `PinnedOperatorVersion`;
    - catalogs in no CLI pin at all: they are explicit in the manifest.
    `cli/hack/platform/` is a test fixture, never a pin source. Catalog git tags carry prefixes (`opm-v4.4.2`, `k8s-v1.0.0-alpha.5`). No repo has a `v1.0.0-beta*` tag yet, and `opm` has no repo-level tag.
29. **gen-lastmod needs full git history.** CI checks out with `fetch-depth: 0`.

**Process.**
30. **`openspec/config.yaml` rules.** Every `rules:` entry must be a plain string. `openspec validate --strict` fails a change with no spec deltas unless `.openspec.yaml` has `skip_specs: true`; every OpenSpec change in this set has it, and archives with `--skip-specs`.
31. **Known failures.** See section 10: core index diff, cli e2e, library flake and `.cue-cache`, opm-operator strict specs.
32. **A section that cannot end green is a planning defect.** Stop and report; do not merge sections.
33. **The root `/opsx:*` routers point at stale checkouts.** They `cd` into `WS/<repo>/` and read that checkout's skills and changes. Main checkouts are never pulled (catalog_opm and enhancements were already behind `origin/main` on 2026-09-30), and opmodel.dev's has no skills. Read skills from your worktree and run `openspec` there (section 1).
34. **The history-rewrite hook refuses rebase and lease-push.** It reads the branch from the session's working directory, not from `git -C`, so from the workspace root (on `main`) every rebase is refused. It matches text: any Bash command that mentions both `git` and the word "rebase" is refused, including a commit message or a heredoc. Keep that word out of your commands, and update branches with `git merge origin/main` (section 7, step 7).
35. **The old Astro tasks keep their bare names until A section 5.** `image`, `build`, `serve`, `preview`, `shots` and `shots:image` would retag `opmodel-dev-site:local` or `opmodel-dev-shots:local`, or publish port 4321. Never run them (section 10).
36. **No tag that exists today can be built.** Every pre-S tag carries `sidebar:` front matter, which the lint rejects. B's resolver refuses a ref older than its repo's S merge; tests pin post-S SHAs.
37. **Upstream Hextra ships agent files.** Its repo has `CLAUDE.md` and `AGENTS.md` telling agents to run `npm install`. They are never vendored; if you meet one, ignore it (section 1: no npm on the host).
38. **CI checkout layout.** `OPM_WS` is the parent of the opmodel.dev checkout, so in Actions every repo, opmodel.dev included, is checked out with `path: <repo>` under `$GITHUB_WORKSPACE`.
