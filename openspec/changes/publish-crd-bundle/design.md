## Context

The operator releases with release-please (`release.yml`: `release-please`, then `image-release`, `publish-examples` and `publish-release`, which makes the draft GitHub Release public; outputs `releases_created` and `tag_name`; one release run at a time). Its site pages are `docs/site/{start,operating,diagnostics,reference}/`. `docs/site/reference/operator-resources.md` holds authored front matter (`weight: 7`) and a one-line intro, crdref's generated block (lines 10-434), then an authored `## See also` heading whose body is only an authoring brief. crdref (`hack/crdref/main.go`) picks each kind's example from `config/samples/<group>_<version>_<kind>.yaml` and leaves it out when it references `testing.opmodel.dev` (`main.go:212-221`); finds the controller by scanning `internal/controller` for `For(&<Kind>{})` and `Named("<name>")`; links decision citations to `/enhancements/NNNN/decisions/`. opmodel.dev's v1.0 reads these pages from `main` today and fails its build on a broken internal link.

docs-kit contracts read: C5, C6, C9, C12, C15 (completable pages: authored front matter and body first, then the generated body; the authored body must not hold the generated first heading), C18 (`crd`). Defined by docs-kit's `generalize-build-assembly`, `add-crd-extractor` and `add-authored-docs`. Reference adopter: catalog_opm.

Reconcile phases: none affected. This change touches documentation, CI and one test.

## Goals / Non-Goals

**Goals:** the operator's docs bundle with its authored pages and its resource reference at today's URL; publishing on every release; deleting crdref once the site reads the bundle, without losing the "Served by" guarantee.

**Non-Goals:** CRD or doc-comment changes; one page per kind (C18 keeps one page); the site's switch.

## Decisions

### D1. `docs-kit.cue`

```cue
bundles: "opm-operator": {
	placement: {kind: "docs", root: "/docs/", owns: ["reference/operator-resources.md"]}
	version: {from: "tag", prefix: "v"}
	sources: [
		// The authored pages ship in the same bundle (docs-kit DESIGN decision
		// 20). Until the site reads this bundle, the committed page carries
		// crdref's block, which would collide with the crd page, so it is
		// excluded; after G2-switch the block and this exclude go, and the
		// authored intro then completes the generated page.
		{kind: "markdown", dir: "docs/site", exclude: ["reference/operator-resources.md"]},
		{
			kind:        "crd"
			dir:         "./config/crd/bases"
			samples:     "./config/samples"
			page:        "reference/operator-resources.md"
			title:       "Operator resources"
			description: "One generated entry per operator resource kind: ModuleInstance, ModulePackage, Platform and TransformerRegistration."
			weight:      7 // the committed page's weight, so the page keeps its place before the intro completes it
			order:       ["ModuleInstance", "ModulePackage", "Platform", "TransformerRegistration"]
			// The hello fixture is a test module, not an example to copy (crdref's rule).
			hideSamplesMatching: ["testing.opmodel.dev"]
			// kubebuilder's scaffold labels on every sample, which crdref strips
			// (hack/crdref/main.go:205-208); removed only when the value matches.
			stripLabels: {
				"app.kubernetes.io/name":       "opm-operator"
				"app.kubernetes.io/managed-by": "kustomize"
			}
			// The Named(...) of the controller whose builder calls For(&<Kind>{})
			// in internal/controller; the reconciledBy test keeps these in step.
			reconciledBy: {
				ModuleInstance:          "moduleinstance"
				ModulePackage:           "modulepackage"
				Platform:                "platform"
				TransformerRegistration: "transformerregistration"
			}
			citations: "link"
		},
	]
}
```

The names are what crdref's scan finds at `9835474` (`internal/controller/moduleinstance_controller.go:135`, `platform_controller.go:578`, `modulepackage_controller.go:182`, `transformerregistration_controller.go:602`) and what the committed page states ("The operator's `moduleinstance` controller watches every ModuleInstance."). The title and description are the committed page's front matter.

**At adoption** (`main` at `bc4e8aa`, docs-kit `v0.4.0`): the four `Named(...)` values are unchanged (`moduleinstance_controller.go:160`, `modulepackage_controller.go:193`, `platform_controller.go:578`, `transformerregistration_controller.go:602`), so `docs-kit.cue` is D1 as written. `task dev:docs:reference:check` passes. Parity (task 1.5): the built page from its first `## ` heading on is byte-identical to the committed page between crdref's markers, once the blank line that follows the BEGIN marker and the one before the END marker are dropped (they pad the marker comments and are not part of crdref's body). No C18-listed difference shows. Sample selection (D2): ModuleInstance has no Example, ModulePackage and Platform show their samples without the two scaffold labels, and TransformerRegistration has no sample file; the same as crdref's block.

### D2. Sample selection matches crdref (docs-kit fix assumed, verified at adoption)

`config/samples` holds two ModuleInstance documents: `opmodel.dev_v1alpha1_moduleinstance.yaml` (the `testing.opmodel.dev` hello fixture, used by kustomize and `test/integration/crdvalidation`) and `opmodel.dev_v1alpha1_moduleinstance_jellyfin.yaml`, plus a Flux `OCIRepository` and `kustomization.yaml`. crdref reads only the file named `<group>_<version>_<kind>.yaml`, takes its first document of the kind, strips the kubebuilder scaffold labels, and hides a sample that references `testing.opmodel.dev`; so today's ModuleInstance entry has no Example and the jellyfin file is never read.

docs-kit#13 settled this in `add-crd-extractor` (C18): the kubebuilder file-name pick (`<group>_<version>_<kind>.yaml`, first matching document) is automatic, and the rest is config, not defaults: `hideSamplesMatching` (a sample containing one of the strings is not shown), `stripLabels` (each label removed when its value matches, `labels` dropped when empty) and `weight`. This plan sets `hideSamplesMatching: ["testing.opmodel.dev"]`, `stripLabels` with crdref's two scaffold labels (`app.kubernetes.io/name: opm-operator`, `app.kubernetes.io/managed-by: kustomize`) and `weight: 7` (D1). If the released extractor lacks any of it, adoption stops for docs-kit rather than move or rename samples, which kustomize, `task examples:pin` and the crdvalidation tier depend on.

### D3. Publishing

`docs.yml` is catalog_opm's with `project: opm-operator` and tags `vX.Y.Z`; the dispatch comment states that release-mode backfills work for `v1.0.0-beta.4` (its tree has the four CRDs, the samples and `docs/site/`). `release.yml` gains:

```yaml
  publish-docs:
    name: Publish the operator docs bundle
    needs: [release-please, image-release]
    if: needs.release-please.outputs.releases_created == 'true'
    permissions:
      contents: read
      packages: write
      id-token: write
    # Pinned by docs-kit release tag, not a SHA: the signing certificate names
    # publish.yml at this ref, and the site trusts only docs-kit's v* tags
    # (docs-kit C5, C9). Moves with .opm-docs-version in one PR.
    uses: open-platform-model/docs-kit/.github/workflows/publish.yml@vX.Y.Z
    with:
      project: opm-operator
      mode: release
      tag: ${{ needs.release-please.outputs.tag_name }}
```

It follows `image-release` (orchestration's choice) and runs beside `publish-examples`; `publish-release` does not wait for it, so a docs failure never holds the release a draft, and the dispatch recovers it. The workflow-level `concurrency` group already serializes release runs.

**Backfill dry run** (task 1.6, 2026-10-03, `opm-docs` `v0.4.0`, config from the adoption branch): in a detached scratch worktree of `v1.0.0-beta.4` (`0d3532b`, which has no `docs-kit.cue`), `.bin/opm-docs build --project opm-operator --release v1.0.0-beta.4 --source <beta.4 worktree> --out <scratch>` exits 0 with 5 pages (the four authored pages and the generated `reference/operator-resources.md`), version `1.0.0-beta.4`, revision 0, `source.ref` `v1.0.0-beta.4`; `opm-docs lint --bundle` on it is OK. beta.4's committed resource page predates crdref and holds only an authoring brief, so it is excluded and the generated page stands alone. Running this branch's crdref over the beta.4 tree (`go run ./hack/crdref -root <beta.4 worktree>`) and comparing its block with the backfilled page gives a byte-identical body, matching C18's parity record for beta.4.

Tasks: root `Taskfile.yml` gains `tools:opm-docs`, `docs:bundle`, `docs:pins:check`, `docs:bundle:check`, named as in the other adopters; `.tasks/opm-docs.sh` is copied byte for byte and installs to `.bin/` (C12), not the repository's `bin/` (`LOCALBIN`), so the shared script stays identical. The operator has no `task check`: `docs:bundle:check` joins the validation gates in `openspec/config.yaml` and the verification checklist in `AGENTS.md`, and `lint.yml` gains a `task docs:pins:check` step (offline). `.gitignore` gains `/out/` and `/.bin/`.

### D4. The completed page (after G2-switch and G2-edge)

**Gate.** G2-switch holds (opmodel.dev#38, 2026-10-04). G2-edge is opmodel.dev `add-edge-build` section 2 merged: until then the site's `sources-main` job reads the operator's `main` checkout, where the reduced page of section 4 has no `## <Kind>` sections for other `main` pages to link, and only from then does it read the `edge` bundle, where section 5 completes the page (owner decision 2026-10-04 on `pull-reference-bundles` OQ1). docs-kit's orchestration gates step 8, which starts with section 3, on both.

`operator-resources.md` becomes its front matter (`title`, `description`, `type: reference`, `weight: 7`) and its intro sentence: no marker lines, no `## <Kind>` heading (the generated body's first heading is `## ModuleInstance`, C18 D4). The trailing `## See also` cannot stay where it is: a completable page puts the whole authored body before the generated entries (C15), so the heading would stand empty above them. Its brief moves into the intro's brief ("link the Install the operator and Delete an instance safely guides and the Operator conditions page from the intro"), and the heading goes.

The reduction lands on `main` as its own Markdown-only commit (its own section, so the squash keeps it alone), before the `exclude` goes. That keeps every release cut before it revisable: the backfilled `1.0.0-beta.4` and `1.0.0-beta.5`, the release v1.0 reads since cli `1.0.0-beta.7` pins it (opmodel.dev#38, 2026-10-04), and any release cut before section 4 merges. A docs revision builds the release tree with `main`'s config and applies one Markdown-only or comment-only commit (C3 "Docs revisions"). Once `main` drops the exclude, such a tree still holds the full page with its `## ModuleInstance` heading, which a completable page refuses; so the first revision of each of those releases after that must apply the reduction commit, and every later revision carries it (revisions accumulate their patches). Revisions are dispatched by hand for now (opm-operator#188, tracked in docs-kit#16).

`hack/crdref/` goes with its tests, `.tasks/dev.yaml` `docs:reference` and `docs:reference:check`, and `lint.yml`'s "Generated resource reference is current" step. `AGENTS.md`'s rule about the generated markers becomes "the resource reference is generated by docs-kit from the CRD types; their doc comments are the page".

### D5. `reconciledBy` stays true (after G2-switch and G2-edge)

**Until section 3 (added in review of section 1):** crdref's block is the guard. `task docs:bundle:parity`, a `Lint` step after "Generated resource reference is current", builds the bundle and diffs the generated page from its first `## ` heading on against crdref's block in the committed page (the blank lines padding the marker comments dropped). crdref reads "Served by" from the controllers, so a renamed `Named(...)` without a `reconciledBy` edit fails `Lint` instead of publishing a wrong page into an immutable release bundle. Checked at adoption: it passes, works in a depth-1 clone, and fails naming both controller names when `Named("platform")` becomes `Named("platform-reconciler")` and crdref is rerun. Section 3 deletes the task and its step with crdref; the test below replaces it.

crdref's guarantee that "Served by" names the real controller would be lost with the scan. A test, `internal/controller/docskit_reconciledby_test.go`, keeps it: it reads `docs-kit.cue` with `cuelang.org/go` (already a dependency), takes the `crd` source's `reconciledBy`, scans `internal/controller/*.go` with `go/parser` for builder chains holding one `For(&<api package>.<Kind>{}, ...)` (the controllers import the API as `releasesv1alpha1` and pass predicates after the object, checked 2026-10-04) and one `Named("<name>")` (crdref's scan, moved), and fails naming the kind when the two maps differ. It runs in `task dev:test`.

## Research & Decisions

### Where publish-docs sits in the release job graph

**Context**: orchestration says after `image-release`; the release only becomes public in `publish-release`.
**Options considered**: 1. after `image-release`, beside `publish-examples` - a docs bundle can publish for a release that ends as a stuck draft; the docs never block the release. 2. after `publish-release` - the bundle exists only for public releases; a docs failure still cannot block the release, but the bundle waits for the slowest job.
**Decision**: option 1, as orchestration says.
**Rationale**: a draft that the runbook recovers with "Re-run failed jobs" still has the same tag and content, so its bundle is right; and the cli can only pin a public release anyway.

### A test for `reconciledBy` (orchestration: optional)

**Decision**: yes, when crdref is deleted (D5).
**Rationale**: the page states a fact about the code, and the operator's spec has always required that fact to come from the code. The scan already exists; moving it into a test costs little and keeps the guarantee without putting Go inference into docs-kit.

### Citation links

**Decision**: `citations: "link"`, matching the committed page (for example `[0015:D3/D16](/enhancements/0015/decisions/)` at line 99).
**Rationale**: parity with crdref; the enhancements section serves `/enhancements/NNNN/decisions/` in every site version.

## Risks / Trade-offs

- From G2-switch until the first operator release after the exclude goes, the site shows a bundle built with the `exclude`: the generated page alone, with D1's title, description and `weight: 7`, but no intro.
- Sample selection (D2) blocks adoption until docs-kit's fix is released.
- After G2-switch, a fix to a CRD description reaches the site only through an operator release: a docs revision applies only Markdown or comment changes, and CRD YAML is neither. `docs:` commits do not release the operator, so such a fix waits for the next releasable commit.
