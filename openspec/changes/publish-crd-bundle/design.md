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
		// excluded; section 3 deletes the block and this exclude, and the
		// authored intro then completes the generated page.
		{kind: "markdown", dir: "docs/site", exclude: ["reference/operator-resources.md"]},
		{
			kind:        "crd"
			dir:         "./config/crd/bases"
			samples:     "./config/samples"
			page:        "reference/operator-resources.md"
			title:       "Operator resources"
			description: "One generated entry per operator resource kind: ModuleInstance, ModulePackage, Platform and TransformerRegistration."
			order:       ["ModuleInstance", "ModulePackage", "Platform", "TransformerRegistration"]
			// The Named(...) of the controller whose builder calls For(&<Kind>{})
			// in internal/controller; section 3's test keeps these in step.
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

### D2. Sample selection must match crdref (verified in section 1)

`config/samples` holds two ModuleInstance documents: `opmodel.dev_v1alpha1_moduleinstance.yaml` (the `testing.opmodel.dev` hello fixture, used by kustomize and `test/integration/crdvalidation`) and `opmodel.dev_v1alpha1_moduleinstance_jellyfin.yaml`, plus a Flux `OCIRepository` and `kustomization.yaml`. crdref reads only the file named `<group>_<version>_<kind>.yaml` and hides a sample that references `testing.opmodel.dev`, so today's ModuleInstance entry has no Example and the jellyfin file is never read. docs-kit's `add-crd-extractor` design says only "one sample per kind" and refuses a second one (its D5 message), and names no fixture-registry rule. If the released extractor keeps that rule, this repository's bundle fails to build, or shows the test fixture as the example. Section 1 checks the released behavior (task 1.4) and stops for a docs-kit fix rather than move or rename samples, which kustomize, `task examples:pin` and the crdvalidation tier depend on.

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

Tasks: root `Taskfile.yml` gains `tools:opm-docs`, `docs:bundle`, `docs:pins:check`, `docs:bundle:check`, named as in the other adopters; `.tasks/opm-docs.sh` is copied byte for byte and installs to `.bin/` (C12), not the repository's `bin/` (`LOCALBIN`), so the shared script stays identical. The operator has no `task check`: `docs:bundle:check` joins the validation gates in `openspec/config.yaml` and the verification checklist in `AGENTS.md`, and `lint.yml` gains a `task docs:pins:check` step (offline). `.gitignore` gains `/out/` and `/.bin/`.

### D4. Section 3: the completed page

`operator-resources.md` becomes its front matter (`title`, `description`, `type: reference`, `weight: 7`) and its intro sentence: no marker lines, no `## <Kind>` heading (the generated body's first heading is `## ModuleInstance`, C18 D4). The trailing `## See also` cannot stay where it is: a completable page puts the whole authored body before the generated entries (C15), so the heading would stand empty above them. Its brief moves into the intro's brief ("link the Install the operator and Delete an instance safely guides and the Operator conditions page from the intro"), and the heading goes.

`hack/crdref/` goes with its tests, `.tasks/dev.yaml` `docs:reference` and `docs:reference:check`, and `lint.yml`'s "Generated resource reference is current" step. `AGENTS.md`'s rule about the generated markers becomes "the resource reference is generated by docs-kit from the CRD types; their doc comments are the page".

### D5. Section 3: `reconciledBy` stays true

crdref's guarantee that "Served by" names the real controller would be lost with the scan. A test, `internal/controller/docskit_reconciledby_test.go`, keeps it: it reads `docs-kit.cue` with `cuelang.org/go` (already a dependency), takes the `crd` source's `reconciledBy`, scans `internal/controller/*.go` with `go/parser` for builder chains holding one `For(&v1alpha1.<Kind>{})` and one `Named("<name>")` (crdref's scan, moved), and fails naming the kind when the two maps differ. It runs in `task dev:test`.

## Research & Decisions

### Where publish-docs sits in the release job graph

**Context**: orchestration says after `image-release`; the release only becomes public in `publish-release`.
**Options considered**: 1. after `image-release`, beside `publish-examples` - a docs bundle can publish for a release that ends as a stuck draft; the docs never block the release. 2. after `publish-release` - the bundle exists only for public releases; a docs failure still cannot block the release, but the bundle waits for the slowest job.
**Decision**: option 1, as orchestration says.
**Rationale**: a draft that the runbook recovers with "Re-run failed jobs" still has the same tag and content, so its bundle is right; and the cli can only pin a public release anyway.

### A test for `reconciledBy` (orchestration: optional)

**Decision**: yes, in section 3 (D5).
**Rationale**: the page states a fact about the code, and the operator's spec has always required that fact to come from the code. The scan already exists; moving it into a test costs little and keeps the guarantee without putting Go inference into docs-kit.

### Citation links

**Decision**: `citations: "link"`, matching the committed page (for example `[0015:D3/D16](/enhancements/0015/decisions/)` at line 99).
**Rationale**: parity with crdref; the enhancements section serves `/enhancements/NNNN/decisions/` in every site version.

## Risks / Trade-offs

- Between section 2 and the first operator release after section 3, the site shows a bundle built with the `exclude`: the generated page alone, with C18's title and description but no `weight` (C18's config has none) and no intro. The page can move in the Reference sidebar until then. Recorded as a docs-kit gap.
- Sample selection (D2) can block section 1 until docs-kit changes.
- After G2-switch, a fix to a CRD description reaches the site only through an operator release: a docs revision applies only Markdown or comment changes, and CRD YAML is neither. `docs:` commits do not release the operator, so such a fix waits for the next releasable commit.
