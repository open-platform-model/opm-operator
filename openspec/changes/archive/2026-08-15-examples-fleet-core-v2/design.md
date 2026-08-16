# Design — examples-fleet-core-v2

## Overview

Pure fixture-and-pipeline work: no operator Go behavior changes. The risk profile is entirely about published-artifact hygiene — immutable tags, exact-version cross-repo pins, and a namespace another enhancement plans to relocate. Every decision below optimizes for "the release's `publish-examples` job ships correct v2 artifacts on the first cut".

## Research & Decisions

### Patch-bump on the `@v0` line, not a major

**Context**: republishing v2-authored bytes needs new tags. Two workspace precedents conflict: this repo's own v0→v1 crossing kept the module major and bumped the patch (`podinfo` `0.1.0` → `0.1.2`, `cue.mod` stayed `@v0`); the `modules` repo's v1→v2 split enforces cross-train major separation per branch (jellyfin at `@v3` on main vs `@v2` on the v1 branch), CI-enforced, precisely so two schema lines never share a major.
**Decision**: patch bumps on `@v0` (`hello 0.0.5`, `podinfo 0.1.4`, `redis 0.1.7`; `hello_web` continues its lineage at `0.1.3` as the first tag on the renamed path — no immutability conflict on a new path).
**Rationale**: the `modules` repo's separation exists because *both* trains keep publishing; here nothing publishes the v1-line fixtures after the crossing (no maintenance branch, single pipeline), so the collision the rule prevents cannot occur. And the namespace is scheduled for relocation by 0011's `registry-cleanup` — investing major-separation ceremony in tags that will be superseded wholesale is waste. The repo's own crossing precedent is the cheaper, already-blessed path. The mixed-line residue (v1-authored `≤0.1.3`, v2-authored `≥0.1.4` in one `@v0` repo) is acceptable for *test fixtures* whose consumers pin exact versions.

### `hello-web`: renamed to `hello_web` now (decision reversed from an earlier draft)

**Context**: a v2 `hello-web` is schema-impossible (hyphen fails `#SnakeNameType`, and the leaf-equals-name assertion binds the metadata name to the module path's leaf — name and path must move together). An earlier draft dropped the fixture, reasoning the rename belonged wholly to 0011 `registry-cleanup` (D17 item 2) and would drag `cli/examples/*` along. Both grounds weakened on inspection: the CLI decided examples go podinfo-only regardless (no external consumer of a renamed fixture remains), and a new name is a **new registry path** — publishing `opmodel.dev/modules/test/hello_web@v0` touches nothing about the old hyphenated artifacts, which stay for 0011's registry-side cleanup exactly as under a drop. The CUE package inside the fixture was already `hello_web`.
**Decision**: rename at the source — both fixture directories, the `cue.mod` module lines (`modules/test/hello_web@v0`, `releases/test/hello_web@v0`), metadata, `moduleinstance.yaml` coordinate, and every fleet enumeration. First published tag on the renamed path continues the lineage at `0.1.3`. Recorded cross-slice as delivering the source-side half of 0011 D17 item 2; the registry-side residue (relocate/delete the old `hello-web` artifacts and `-e2e` tags) stays with `registry-cleanup`.
**Rationale**: the rename is near-cosmetic in-repo, keeps the fleet's fourth coverage subject (a second blueprint consumer plus the ModulePackage mirror variety), and does part of 0011's work early instead of deleting a working fixture only for 0011 to recreate the name later.

### Fixture metadata: minimum conformant v2, no identity subpackage

**Context**: v2 requires snake `name` == path leaf, full major-suffixed `modulePath`, bare `version`. The v2 exemplar (`modules/jellyfin`) additionally ships an `identity/` package (0010 D38 / 0011 D12) — but that is publish-gate conformance for `opm module publish`, which this pipeline does not use (bare `cue mod publish`), and core documents absence as an accepted exposure.
**Decision**: reauthor `metadata` only; no `identity/` packages. The hand-authored workload-type labels are dropped (the catalog blueprint stamps the transitional duplicate until the catalog drops it; matching reads `matchLabels` regardless since library alpha.13).
**Rationale**: smallest conformant change; the identity packages arrive when 0011's `modules-publish-cutover`-equivalent reaches this pipeline (recorded as future work in the proposal). Keeping the fixtures exemplar-shaped where it is free (snake names already are), and only where it is free.

### Version-source convention survives (grep, not export)

**Context**: `examples:publish` greps `version:` out of `module.cue` because `cue export` trips the blueprint closedness false-positive. The v2 reauthoring keeps `version:` as a plain quoted literal in `metadata`, so the grep keeps working.
**Decision**: keep the grep; add a comment in `.tasks/examples.yaml` noting the v2 metadata shape it depends on (first `version:` line in the file must remain the module version).

### `examples:bundle` RELEASES_DIR fix

**Context**: `RELEASES_DIR` defaults to `test/fixtures/releases`, which does not exist (the directory is `modulepackages/`) — the OCIRepository/ModulePackage manifest branch has been dead since the rename, so releases have never bundled them.
**Decision**: point it at `test/fixtures/modulepackages`; verify the bundle content in the release dry-run.

## Reconcile phase impact (Source, Render, Apply, Prune, Status)

None — fixture and pipeline work only. The registry-backed integration tier and e2e specs go from interim-red (post-retarget) back to green; the e2e `lifecycle` Flux path exercises the corrected bundle directory.

## Technical Notes

### Per-fixture edit list

Each of `hello`, `hello_web`, `podinfo`, `redis` (`test/fixtures/modules/<m>/`):
1. `cue.mod/module.cue`: deps → `opmodel.dev/core@v2 v2.0.0-alpha.4`, `opmodel.dev/catalogs/opm@v2 v2.0.0-alpha.3`.
2. `module.cue`: `import m "opmodel.dev/core@v2"`; `res ".../resources/v1beta1"`; `metadata.modulePath: "opmodel.dev/modules/test/<m>@v0"`; `version` bumped.
3. `components.cue`: `bp ".../blueprints/v1beta1"`, `tr ".../traits/v1beta1"` (hello_web, podinfo, redis); resources import (hello); hand-authored workload-type labels removed.
4. `moduleinstance.yaml`: coordinate → the module's (possibly renamed) path + bumped `v`-prefixed tag.

`hello_web` additionally: directory rename from `hello-web/`, `cue.mod` module line → `opmodel.dev/modules/test/hello_web@v0`, `metadata.name: "hello_web"` (package name already agrees).

Each `test/fixtures/modulepackages/<m>/`: `cue.mod` deps → core v2 + the bumped test-module version; `instance.cue` core import → v2. The `hello-web` mirror renames to `releases/test/hello_web@v0`; its `instance.cue` import drops the explicit `:hello_web` qualifier.

### Enumeration updates

`.tasks/release.yaml:7,19` `PKGS: "hello hello_web podinfo redis"`; `test/integration/reconcile/kernel_package_renderer_test.go:125` loop (four names, `hello_web` spelled snake); `config/samples/opmodel.dev_v1alpha1_moduleinstance.yaml` pin (`hello v0.0.5`); integration pins `hello v0.0.5` (`kernel_module_renderer_test.go:119`, `acquire_test.go:59`), `redis v0.1.7` (`example_modules_test.go:95`); `acquire_test.go:70` flips to the full-address assertion (`opmodel.dev/modules/test/hello@v0`); `openspec/specs/example-test-modules` normative fleet text moves to the new name (delta in this change).

### Publish verification

The release pipeline publishes on the next cut automatically (`SINCE=<prev-tag>` sees the fixture diffs). Before the release: one manual `task examples:publish PRERELEASE=e2e.g<sha>` via the e2e workflow (which runs on push to main) proves the v2 modules publish and render end-to-end; the `-e2e` tags it creates are within 0011's existing cleanup inventory class.
