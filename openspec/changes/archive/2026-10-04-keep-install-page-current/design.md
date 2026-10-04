## Context

The page shows the operator version in four places: the `opm operator install` sample output (`✔ opm-operator v1.0.0-beta.4 installed`), the "this page uses v1.0.0-beta.4" sentence with the `kubectl apply` URL, the image pin and the `cosign verify` command, and the `kubectl get platform` and controller-log sample output. It also names catalog `4.4.4` (sample output and the Platform YAML), core `v2.0.0-beta.1` (sample log) and opm `v1.0.0-beta.5`. Since opmodel.dev#38 (2026-10-04) the site reads this page from the operator's release docs bundles, built from each release's tag.

release-please already rewrites `internal/version/version.go` through `extra-files` (the `release-automation` spec requires it); its generic updater also rewrites every version inside a `x-release-please-start-version` ... `x-release-please-end` block, in any file.

## Goals / Non-Goals

**Goals:**

- Every released bundle's install page names the release it documents, with no hand edit per release.
- The smallest mechanism: no new tool, no docs-kit contract change.

**Non-Goals:**

- Naming the newest core or catalog release on the page (they release on their own schedules; the page links where to find them).
- Other pages or repositories (follow-up below).

## Decisions

### D1. release-please blocks, not a docs-kit substitution

```markdown
   <!-- x-release-please-start-version -->

   Without the CLI, apply the `install.yaml` asset of a release. ...; this page uses v1.0.0-beta.5:

   ```sh
   kubectl apply --server-side -f https://github.com/open-platform-model/opm-operator/releases/download/v1.0.0-beta.5/install.yaml
   ```
   ...

   <!-- x-release-please-end -->
```

The markers are HTML comments on their own lines, outside code fences (inside a fence they would print), each at the content indentation of the block that encloses it: three spaces inside step 1 of the ordered list, column 0 only for a block outside the list. A column-0 comment inside step 1 would end the list, push the rest of step 1 out of it and restart the numbering at step 2 (`<ol start="2">`), in an immutable release bundle. release-please matches the marker text anywhere in a line, so indentation does not affect it. docs-kit's markup check lets an authored page keep comments (C11), and the cli already publishes pages with comments. Four blocks: the install sample output, the kubectl/image/cosign passage, the `kubectl get platform` output, the controller log.

Inside a block release-please replaces every version, so a block holds only the operator's: the "Every v1.0.0 operator release is marked Pre-release" and "retired v0.7.5 manifest" sentence stays outside, and the core and catalog versions leave the blocks (D2).

**Alternatives.** A docs-kit substitution (a `{{< opm/version >}}`-style shortcode, or a placeholder `opm-docs build --release` fills from the tag) is the general answer, but it is a page-dialect change (C11 allows no other shortcode), a contract every producer and the site must adopt, and a second source of truth beside release-please, which already owns the version. A generated version block from `go generate` needs a new tool and a check task for one page. A one-off edit plus a follow-up issue is stale again at `1.0.0-beta.6`.

### D2. The page names only the operator's version

| Today | After |
|---|---|
| sample output: `INFO seeded cluster Platform subscribed to opmodel.dev/catalogs/opm@v4 4.4.4` | dropped from the shortened output; the paragraph below it already says the command creates the Platform |
| "opm v1.0.0-beta.5 carries opm-operator v1.0.0-beta.4." | "Each opm release carries one operator release, which the output names." |
| Platform YAML `version: "4.4.4"` | `version: "4.Y.Z"`, and the sentence after it: replace `4.Y.Z` with a published build of the opm catalog, such as the newest on the [opm catalog](/catalogs/opm/4/) page |
| log: `OPM core schema resolved {"version": "v2.0.0-beta.1"}` | `{"version": "..."}`, with "the core version it resolved" in the sentence that introduces the log |

`4.Y.Z` is not a version, so release-please never touches it, and the link follows the newest release of the major (STYLE: catalogs are linked by major).

### D3. A test keeps the blocks honest

`internal/version/version_test.go` gains `TestInstallPageNamesThisRelease`: it reads `../../docs/site/start/install-the-operator.md`, finds the marker blocks, and fails when there is none, when a start has no end, or when a version inside a block (`v?\d+\.\d+\.\d+(-[0-9A-Za-z.]+)?`) is not `Version`. On `main` the page and the constant name the last release; in a Release PR release-please rewrites both, so the test passes in both. It runs in `task dev:test`.

### D4. Two PRs, so the page stays revisable

The repositories squash-merge, so a commit reaches `main` alone only as its own PR. Section 1 (the page edit, Markdown-only) is PR A and merges first; sections 2 and the archive (config, test, `AGENTS.md`) are PR B. A docs revision of `1.0.0-beta.5` (dispatched by hand, opm-operator#188) can then apply PR A's squash commit, recorded here at 1.3 as the `fix=` to pass, so v1.0 shows the fix before the next release if the owner wants it. beta.5's tree carries its own `docs-kit.cue` with the operator-resources exclude, so this revision has no ordering constraint with `publish-crd-bundle`'s reduction commit, which only `1.0.0-beta.4` revisions need.

Delivered: PR A merged as opm-operator#200, squash `e268cbd2ed6e80916e6db8b2d58fbe0b094b6524`. The docs revision of `1.0.0-beta.5` with `fix=e268cbd` ran as [Actions run 37177763682](https://github.com/open-platform-model/opm-operator/actions/runs/37177763682) and published `docs/opm-operator:1.0.0-beta.5.1`, which `1.0.0-beta.5` now resolves to; the site rebuild (run 37177794431) shows v1.0.0-beta.5 on the live page.

The cascade rule. opm-operator#199 (`deps:cascade`), merged while this change was planned, added a warning when the install page's sample catalog or core version drifted from the tree, with test S13. PR A removes those versions, so the rule would have warned on `4.Y.Z`, and S13 failed PR A's Lint. opm-operator#201 removed the rule and S13 before PR A merged, with no operator-version check in their place, since release-please owns that version. PR A then took `main` by a merge commit, never by rewriting its history, so its squash stayed Markdown-only.

## Risks / Trade-offs

- [release-please's generic updater misses the Markdown file] -> The next Release PR shows it: the page must change in the PR's diff; whoever reviews that PR checks it. The test catches a page that release-please left behind once the PR's `Version` moves. Fallback: drop `extra-files` for the page and let the test fail the Release PR until someone edits it, which still never ships a stale page.
- [Someone adds a core or catalog version inside a block] -> The test fails, naming the version.
- [A version outside the blocks goes stale] -> Not guarded; the `AGENTS.md` rule says operator versions on pages go inside the blocks and other projects' versions are not named.

## Follow-ups

- cli: `docs/site/start/install-the-cli.md` names `v1.0.0-beta.5` in a page published from later releases; filed as cli#295 (release-please blocks cover its version literals, not its sample digests, SHAs and dates).
