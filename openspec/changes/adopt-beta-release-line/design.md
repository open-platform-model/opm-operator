## Context

See proposal.md (Why). State on `main` at planning time (2026-09-30): manifest and `internal/version/version.go` at `1.0.0-alpha.22`; `go.mod` requires library `v1.0.0-alpha.36`; `release-please-config.json` has `versioning: prerelease`, `prerelease: true`, `prerelease-type: alpha`, `bump-minor-pre-major: false`, and a visible Documentation section. Release PR #161 (`chore(main): release 1.0.0-alpha.23`, docs entries only) is open on `release-please--branches--main--components--opm-operator` and is held: nobody but the supervisor merges it, and never as an alpha. GitHub's Latest release for this repo is v0.7.5, the newest release not flagged Pre-release, so `releases/latest/download/install.yaml` installs v0.7.5.

The cutover sequence is fixed outside this repo: G1 core `v2.0.0-beta.1`, G2 library `v1.0.0-beta.1`, G3 catalogs `k8s-v1.0.0-beta.1` and `opm-v4.4.4`, G4 cli `v1.0.0-beta.1`, G5 this operator's `v1.0.0-beta.1`, G6 the first cli release embedding it.

## Goals / Non-Goals

**Goals:**

- One squash commit on `main` that bumps the library and carries `Release-As: 1.0.0-beta.1`, producing exactly one Release PR titled `chore(main): release 1.0.0-beta.1`.
- After beta.1, every releasable commit proposes the next `-beta.N` with no further config edit.
- The spec states the mechanism the repo actually uses, so the next line crossing (GA) is not re-derived.

**Non-Goals:**

- No controller, API or CRD change. The CRD API stays `v1alpha1` until GA.
- No change to the `:latest` image tag policy: it keeps tracking the newest release, betas included.
- No fixture republish, no `test/fixtures/catalog.go` edit, no workflow opm CLI pin: those are supervisor-run tracks after G3 and G6.

## Research & Decisions

### Crossing the line with a footer, not config `release-as`

**Context**: release-please's prerelease strategy only applies `prerelease-type` when the current version has no suffix. On `1.0.0-alpha.22` a flip to `beta` still proposes `1.0.0-alpha.23`.
**Explored**: release-please 17.6.0 `src/versioning-strategies/prerelease.ts` (`bumpPrerelease`), executed against 17.3.0 and 17.6.0 with identical results (the operator's action v5.0.0 bundles 17.6.0); upstream issue 2447; workspace precedents cli ed9774e and catalog_opm bc778ca (config `release-as` re-proposed a published version and had to be removed); footer precedents core d8db7fe, library c8e2f8d, catalog_opm 37d2771, operator 2d96a46.
**Decision**: The squash commit of the work PR MUST end with the footer block `Release-As: 1.0.0-beta.1` followed by the plain co-author trailer. `release-please-config.json` MUST NOT gain a `release-as` key. The manifest and the version constant MUST NOT be hand-edited.
**Rationale**: The footer is self-clearing (the commit leaves the window once `v1.0.0-beta.1` is tagged) and idempotent (the open Release PR is recomputed on every push). The config key is sticky and has already broken two repos.

### Beta numbering after beta.1

**Context**: The owner wants every later release on the same 1.0.0 line.
**Explored**: release-please 17.6.0 `src/versioning-strategies/prerelease.ts` (`bumpPrerelease`), executed: from `X.0.0-beta.N` with `prerelease: true`, fix, feat and breaking commits all propose `X.0.0-beta.(N+1)`; with `prerelease: false` the same base proposes `X.0.0`.
**Decision**: Keep `versioning: prerelease` and `prerelease: true`; set `prerelease-type: beta` so the config reads what the line is. GA is a later change: `prerelease: false` plus a visible carrier commit.
**Rationale**: No further config edit is needed during beta, and the GA path is already verified.

### The library bump PR is the carrier

**Context**: The footer is honoured only in the final commit message on `main`. The supervisor writes the squash message at merge time; inner branch commits are discarded by the squash.
**Decision**: The single work PR (library bump, config flip, docs, archived change) is squash-merged as `fix(deps): adopt the beta release line on library v1.0.0-beta.1` with the footer. No inner commit carries a `Release-As:` footer, and nothing in the plan relies on one.
**Rationale**: `fix(deps)` releases on its own, so the carrier is a real change, not an empty commit. Folding the docs edits into the same squash keeps them from cutting a separate docs-only release.

### Release PR held until G4

**Context**: Once the carrier merges, #161 is recomputed to `chore(main): release 1.0.0-beta.1`. Operator CI installs the cli with `go install`, which reports version `dev` and skips the cli's operator-version ceiling, so the work PR can merge at G2 and G3.
**Decision**: The work PR MAY merge once G2 and G3 are confirmed. #161 MUST NOT be merged before G4 (cli `v1.0.0-beta.1` released); the supervisor merges it, which makes G5.
**Rationale**: The cli ships its beta.1 before the operator ships one, matching the cutover order.

### Retiring the bump-determination requirement

**Context**: `release-automation` scenario "Breaking-change commit pre-1.0 is demoted to MINOR" names behavior that `bump-minor-pre-major: false` disables, and the MINOR/PATCH/MAJOR scenarios do not hold on the prerelease line. A MODIFIED delta may not drop or rename a live scenario.
**Decision**: REMOVE "Version bump determination from Conventional Commits" and "Initial version baseline" (the manifest is past 0.x, and "pre-v1 maturity" contradicts the beta promise); ADD "Version bump determination per release line" plus "Beta prerelease line". MODIFY "Manual version override via release-as", "Release PR creation on push to main", "Changelog generation" (sections as `release-please-config.json` declares them, hidden types excluded) and "Release PR bumps the annotated version constant" with every live scenario heading kept verbatim.
**Rationale**: The spec matches the config and the executed release-please behavior; heading names stay stable where the behavior they name still holds.

### README and install page

**Context**: `releases/latest/download/install.yaml` installs v0.7.5, a retired major, while every current release is a Pre-release.
**Decision**: README shows `opm operator install` as the primary path and the tagged form `https://github.com/open-platform-model/opm-operator/releases/download/<tag>/install.yaml` with a link to the Releases page to pick `<tag>`, and says why `releases/latest` must not be used. No concrete tag is written: `v1.0.0-beta.1` does not exist until G5, and a burned version would move the target to beta.2. The `:latest` row notes that it tracks beta builds. The docs page's authoring comments move their pairing and version examples to the beta line.
**Rationale**: A tagged URL is the only kubectl form that cannot silently resolve an old major, and a placeholder never 404s or goes stale.

### Version doc examples stay

**Context**: `internal/version/version.go` and `version_test.go` carry `v1.0.0-alpha.2` as format examples, as does `openspec/specs/operator-version-identity/spec.md`.
**Decision**: Not touched here. `version.go` is release-tooling-owned, and the examples are format illustrations that remain correct on the beta line. A later non-release docs pass may move all three together.
**Rationale**: Keeps the carrier to release-relevant files.

### Fixture pins travel apart

**Context**: The workspace commit rule forbids mixing a shipped bump (`go.mod`) with a fixture bump (`config/samples/*`) in one PR; a squash collapses the PR into one commit.
**Decision**: `config/samples/opmodel.dev_v1alpha1_platform.yaml` moves in its own `test(fixtures)` PR on branch `beta/sample-platform-beta-pins`, applied from a supervisor patch produced by the root `platform-pins` run after G3, merged with no footer.
**Rationale**: The sample is not in `dist/install.yaml`; its bump must not release the operator.

## Reconcile phase impact

No reconciler code changes. Through the library bump, the Render phase resolves core `v2.0.0-beta.1`: the generated platform module pins core from the library's default schema module (spec `platform-module-generation`), and `Kernel.Render` reads core from each module's own `cue.mod`. Source, Apply, Prune and Status are unaffected. `Platform.status.operatorVersion` reports `v1.0.0-beta.1` only after #161 merges.

```yaml
# Squash commit message the supervisor writes for the carrier (subject gets " (#<PR>)")
fix(deps): adopt the beta release line on library v1.0.0-beta.1

Moves github.com/open-platform-model/library to v1.0.0-beta.1, which renders
against core v2.0.0-beta.1, and switches release-please to the beta label.
A label flip alone keeps counting alpha, so this commit crosses the line once.
The README no longer installs through the Latest alias, which serves v0.7.5.

Release-As: 1.0.0-beta.1
Co-Authored-By: Claude <noreply@anthropic.com>
```

## Risks / Trade-offs

- [Library beta.1 plus the stale fixtures (core `v2.0.0-alpha.6`, catalogs opm `v4.0.1`) may fail a registry-backed spec, an unverified assumption] → Section 1 runs `task dev:test` with the registry exported and no registry-backed spec skipped before anything else lands; a red run stops the change and is reported, since the fixture republish track may have to merge first.
- [The squash drops or mangles the footer] → The supervisor parses the final message with release-please's own parser before merge and confirms #161's new title after merge; fallback is `BEGIN_COMMIT_OVERRIDE` on the merged PR body and a workflow re-run.
- [#161 merged early: now it cuts alpha.23; between the carrier and G4 it ships operator 1.0.0-beta.1 before the cli beta.1 and its ceiling gate exists] → The supervisor posts a hold comment on #161 before any section starts (tasks.md, Hold H1); only the supervisor merges.
- [A Dependabot Go PR merges between the carrier and #161] → Harmless: it joins the beta.1 changelog, and the footer commit stays in the window.
- [A docs or fix commit after G5 cuts beta.2 before the cli embeds beta.1] → The cli ceiling gate compares MAJOR.MINOR only (cli change `adopt-beta-release-line`, implementing 0021 OQ14), so an operator `1.0.0-beta.2` does not refuse a cli on `1.0.0-beta.N`.
- [`:latest` moves to beta builds] → Accepted by the owner; the README states it.
- [The beta.1 release jobs fail after the tag exists: `image-release` and `publish-examples` run only when the release-please job reports `releases_created`, and a new push does not re-run them] → Use "Re-run failed jobs" on the same workflow run, which keeps the release-please outputs; never push a commit expecting a re-release. A version burned without an image moves the target to beta.2.
