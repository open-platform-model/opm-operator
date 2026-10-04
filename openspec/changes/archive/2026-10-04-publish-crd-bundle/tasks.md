Delivery: one PR per section (proposal.md). Each section has its own gate, named as in docs-kit `docs/orchestration.md`; do not start a section before its gate holds.

## 1. Adopt docs-kit

Gate G2-operator: docs-kit's `add-crd-extractor` (with C18's sample selection: the automatic kubebuilder file-name pick and the `hideSamplesMatching`, `stripLabels` and `weight` config fields), `add-authored-docs` and `generalize-build-assembly` are released. Use the first docs-kit release that carries all three as `vX.Y.Z` below.

- [x] 1.1 `.opm-docs-version`: `vX.Y.Z`. `.tasks/opm-docs.sh`: copy catalog_opm's byte for byte. `.gitignore`: `/out/` and `/.bin/`.
- [x] 1.2 `Taskfile.yml`: `tools:opm-docs`, `docs:bundle` (`--project opm-operator --out out`), `docs:pins:check`, `docs:bundle:check`, as catalog_opm has them (design.md D3).
- [x] 1.3 `docs-kit.cue` exactly as design.md D1; re-read the four `Named(...)` values in `internal/controller` at this commit and use them. Verify: `task docs:bundle` writes `out/opm-operator/` and the page's front matter has `weight: 7`.
- [x] 1.4 Sample selection (design.md D2): the bundle's ModuleInstance entry has no Example, and the ModulePackage and Platform entries show the samples crdref shows, without the scaffold labels. Verify: the build passes and the three entries match; any difference stops the section for docs-kit (do not move or rename samples).
- [x] 1.5 Parity: diff `out/opm-operator/content/reference/operator-resources.md` from its first `## ` heading on against the committed page between crdref's markers. Verify: no difference beyond those docs-kit's C18 parity record lists; record the commit and the result in design.md D1. An unlisted difference stops the section.
- [x] 1.6 Backfill dry run: in a scratch worktree of `v1.0.0-beta.4`, run `.bin/opm-docs build --project opm-operator --release v1.0.0-beta.4 --source <that worktree> --out <scratch>` from this tree (`main`'s config). Verify: it builds and lints; record the result in design.md D3.
- [x] 1.7 `.github/workflows/docs.yml`: catalog_opm's with `project: opm-operator`, tags `vX.Y.Z`, `publish.yml@vX.Y.Z`, the backfill floor `v1.0.0-beta.4` in the dispatch comment. `.github/workflows/release.yml`: `publish-docs` after `image-release` (design.md D3). `.github/workflows/lint.yml`: a "docs-kit pins agree" step running `task docs:pins:check`. Verify: `actionlint` clean; `task docs:pins:check` passes.
- [x] 1.8 `AGENTS.md`: a "Docs bundles" paragraph under "Registry" or a new heading (PR check, edge on `main`, a bundle per release after `image-release`; preview with `task docs:bundle` or `opm-docs serve`; recover with `gh workflow run docs.yml --ref main -f mode=release -f tag=vX.Y.Z`; fix a released page with `mode=revision`, dispatched by hand (opm-operator#188); after G2-switch an authored fix reaches the site only by a release or a revision, and a CRD description fix only by a release), the tasks in "Core Commands", `task docs:bundle:check` in the verification checklist, and `reconciledBy` beside the controller-registration rule. `openspec/config.yaml`: `task docs:bundle:check` as validation gate 5.
- [x] 1.9 `openspec validate publish-crd-bundle --strict` passes; `task dev:fmt dev:vet dev:lint dev:test` and `task docs:bundle:check` green, then commit `ci(docs): publish the operator docs bundle with docs-kit`.

## 2. Publish the bundle the cli pins

Gate: section 1 is merged. Part of gate G2-pins (core `v2.0.0-beta.1`, library `v1.0.0-beta.1` and opm-operator `v1.0.0-beta.4` backfilled, then cli `v1.0.0-beta.6`). This section's deliverable is a publishing operation (the owner's), so its steps are the implementation.

- [x] 2.1 Owner: dispatch `gh workflow run docs.yml --ref main -f mode=release -f tag=v1.0.0-beta.4` (owner decision 2026-10-03), then check `ghcr.io/open-platform-model/docs/opm-operator` is public and linked to `open-platform-model/opm-operator` (change it in the package settings if not).
- [x] 2.2 Optional: a fresh operator release (release PR opm-operator#178) publishes through `publish-docs`; it is not needed here. If the release cascade moves the cli's `PinnedOperatorVersion` before cli `v1.0.0-beta.6`, that version needs its bundle first.
- [x] 2.3 Verify the full, release, minor and major tags of `1.0.0-beta.4` with `cosign verify` and docs-kit C9's identity flags, or an anonymous `opm-docs pull` with a scratch `bundles.cue` naming `opm-operator` under `docs`.
- [x] 2.4 Record in design.md the run URL, version, digest and the verification; tell the cli that the operator's part of G2-pins holds.
- [x] 2.5 `openspec validate publish-crd-bundle --strict` passes; `task dev:fmt dev:vet dev:lint dev:test` green, then commit `docs(openspec): record the first operator docs bundle`.

## 3. Retire hack/crdref

Gates (docs-kit `docs/orchestration.md` step 8; G2-edge is defined there by docs-kit#43, which merges before this plan): G2-switch, opmodel.dev's v1.0 reads core, cli, library and opm-operator from bundles (holds since opmodel.dev#38, 2026-10-04); and G2-edge, opmodel.dev's `sources-main` job reads the operator's `main` from its `edge` docs bundle (design.md D4). Section 3 changes no page and takes G2-edge deliberately (design.md D4).

- [x] 3.1 `internal/controller/docskit_reconciledby_test.go`: the `reconciledBy` test of design.md D5, with crdref's controller scan moved into it. Verify: it passes, and fails naming the kind when one `Named(...)` is changed locally.
- [x] 3.2 Delete `hack/crdref/`. `.tasks/dev.yaml`: delete `docs:reference` and `docs:reference:check`. `.github/workflows/lint.yml`: delete the "Generated resource reference is current" step and its comment, and the "Docs bundle matches crdref" step with its comment. `Taskfile.yml`: delete `docs:bundle:parity` and its comment (the `reconciledBy` test of 3.1 replaces it, design.md D5); drop it from `AGENTS.md` in 3.3. The committed page keeps its old block for now; it is excluded from the bundle and no longer read from git. `docs-kit.cue`: reword the two comments that name crdref, keeping their rules ("The hello fixture is a test module, not an example to copy."; "kubebuilder's scaffold labels on every sample; removed only when the value matches.").
- [x] 3.3 `AGENTS.md`: drop the `dev:docs:reference` and `docs:bundle:parity` commands and the marker rule; the API-marker, `*_types.go` and `config/samples` checklist lines name `task docs:bundle:check` where they name `dev:docs:reference`; in the "Docs bundles" paragraph, the sentences "Until then the committed ... crdref keeps it current" and "Until crdref is retired, `task docs:bundle:parity` ... keeps it true" become: the `reconciledBy` test in `task dev:test` keeps it true. Controller renames still need a `reconciledBy` edit. Verify: `grep -rn "crdref\|docs:reference" --exclude-dir=.git --exclude-dir=out --exclude-dir=.bin --exclude-dir=openspec . | grep -v -e operator-resources.md -e docs-kit.cue` finds nothing (the page block and the exclude comment go in sections 4 and 5, the main specs at the archive in 5.4).
- [x] 3.4 `task dev:fmt dev:vet dev:lint dev:test` and `task docs:bundle:check` green, then commit `ci(docs): retire hack/crdref`.
- [x] 3.5 (added at implementation) `.tasks/cascade/cascade.sh`: delete step 6 (the crdref regenerator) and `SAMPLES_EDITED`; `.tasks/cascade/classes`: drop the page's line and its comment; `.tasks/cascade/test.sh`: drop S11's crdref wording; the `deps-cascade` delta in `specs/deps-cascade/spec.md` (design.md D4). Verify: `task -x deps:cascade:test` passes.

## 4. Reduce the resource page to its intro

Gate: section 3 is merged. A Markdown-only commit, alone in its PR, so a docs revision of the backfilled `1.0.0-beta.4` can apply it (design.md D4; beta.5 and later need none).

- [x] 4.1 `docs/site/reference/operator-resources.md`: front matter and intro only, the See-also brief folded into the intro's brief, no marker lines and no `## <Kind>` heading (design.md D4). Nothing else changes in this section. Verify: `git diff --stat` lists only that file; `task docs:bundle:check` passes (the page is still excluded).
- [x] 4.2 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `docs(site): reduce the operator resource page to its intro`.

## 5. Complete the page from the bundle

Gate: section 4 is merged, and section 2 is checked off with 2.4's run, version and digest recorded (GHCR holds `1.0.0-beta.4` and `1.0.0-beta.5`), since 5.4 archives the change.

- [x] 5.1 `docs-kit.cue`: the `markdown` source becomes `{kind: "markdown", dir: "docs/site"}`, and its comment says the authored intro completes the generated page (no crdref, no exclude). Verify: `task docs:bundle:check` passes and the built page starts with the authored front matter (`weight: 7`) and intro, then `## ModuleInstance`.
- [x] 5.2 design.md D4 records section 4's squash commit as the one the next `1.0.0-beta.4` docs revision must apply first.
- [x] 5.3 Check `main` together with this tree: `task docs:bundle`; fetch opmodel.dev, opm and catalog_opm and put each at `origin/main` (a detached checkout or fresh clones named by `OPM_SRC_OPM` and `OPM_SRC_CATALOG_OPM`); then in that opmodel.dev checkout `OPM_BUNDLES_LOCAL="opm-operator@v1.0=<this worktree>/out/opm-operator" task build:edge`. Verify: it builds green, so every link from another repository's `main` into `/docs/reference/operator-resources/` resolves.
- [x] 5.4 `openspec archive publish-crd-bundle --yes`; then set `openspec/specs/operator-resource-reference/spec.md`'s Purpose to the bundle-built page, and replace the placeholder Purpose of the new `openspec/specs/docs-bundle/spec.md` with what the operator's docs bundle is for (the resource reference and the authored pages, published per release and as `edge`). Verify: `openspec validate --specs --strict` passes for `docs-bundle` and `operator-resource-reference`, and 3.3's grep without the exclusions finds nothing outside `openspec/changes/archive/`.
- [x] 5.5 `task dev:fmt dev:vet dev:lint dev:test` and `task docs:bundle:check` green, then commit `ci(docs): complete the operator resource page from the bundle`.
- [x] 5.6 The PR's body carries the post-merge check, since the archive rides the PR: after merge, the operator `Docs` edge run for the merge commit succeeds; then `gh workflow run Site --repo open-platform-model/opmodel.dev --ref main`, and its `sources-main` job is green with the operator's edge commit equal to the merge commit in its summary.
