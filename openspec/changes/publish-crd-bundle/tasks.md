Delivery: one PR per section (proposal.md). Each section has its own gate, named as in docs-kit `docs/orchestration.md`; do not start a section before its gate holds.

## 1. Adopt docs-kit

Gate G2-operator: docs-kit's `add-crd-extractor` (with the sample-selection fix: kubebuilder file-name pick, scaffold-label stripping, `hideSamplesMatching`, `weight`), `add-authored-docs` and `generalize-build-assembly` are released. Use the first docs-kit release that carries all three as `vX.Y.Z` below.

- [ ] 1.1 `.opm-docs-version`: `vX.Y.Z`. `.tasks/opm-docs.sh`: copy catalog_opm's byte for byte. `.gitignore`: `/out/` and `/.bin/`.
- [ ] 1.2 `Taskfile.yml`: `tools:opm-docs`, `docs:bundle` (`--project opm-operator --out out`), `docs:pins:check`, `docs:bundle:check`, as catalog_opm has them (design.md D3).
- [ ] 1.3 `docs-kit.cue` exactly as design.md D1; re-read the four `Named(...)` values in `internal/controller` at this commit and use them. Verify: `task docs:bundle` writes `out/opm-operator/` and the page's front matter has `weight: 7`.
- [ ] 1.4 Sample selection (design.md D2): the bundle's ModuleInstance entry has no Example, and the ModulePackage and Platform entries show the samples crdref shows, without the scaffold labels. Verify: the build passes and the three entries match; any difference stops the section for docs-kit (do not move or rename samples).
- [ ] 1.5 Parity: diff `out/opm-operator/content/reference/operator-resources.md` from its first `## ` heading on against the committed page between crdref's markers. Verify: no difference beyond those docs-kit's C18 parity record lists; record the commit and the result in design.md D1. An unlisted difference stops the section.
- [ ] 1.6 Backfill dry run: in a scratch worktree of `v1.0.0-beta.4`, run `.bin/opm-docs build --project opm-operator --release v1.0.0-beta.4 --source <that worktree> --out <scratch>` from this tree (`main`'s config). Verify: it builds and lints; record the result in design.md D3.
- [ ] 1.7 `.github/workflows/docs.yml`: catalog_opm's with `project: opm-operator`, tags `vX.Y.Z`, `publish.yml@vX.Y.Z`, the backfill floor `v1.0.0-beta.4` in the dispatch comment. `.github/workflows/release.yml`: `publish-docs` after `image-release` (design.md D3). `.github/workflows/lint.yml`: a "docs-kit pins agree" step running `task docs:pins:check`. Verify: `actionlint` clean; `task docs:pins:check` passes.
- [ ] 1.8 `AGENTS.md`: a "Docs bundles" paragraph under "Registry" or a new heading (PR check, edge on `main`, a bundle per release after `image-release`; preview with `task docs:bundle` or `opm-docs serve`; recover with `gh workflow run docs.yml --ref main -f mode=release -f tag=vX.Y.Z`; fix a released page with `mode=revision`, dispatched by hand (opm-operator#188); after G2-switch an authored fix reaches the site only by a release or a revision, and a CRD description fix only by a release), the tasks in "Core Commands", `task docs:bundle:check` in the verification checklist, and `reconciledBy` beside the controller-registration rule. `openspec/config.yaml`: `task docs:bundle:check` as validation gate 5.
- [ ] 1.9 `openspec validate publish-crd-bundle --strict` passes; `task dev:fmt dev:vet dev:lint dev:test` and `task docs:bundle:check` green, then commit `ci(docs): publish the operator docs bundle with docs-kit`.

## 2. Publish the bundle the cli pins

Gate: section 1 is merged. Part of gate G2-pins (core `v2.0.0-beta.1`, library `v1.0.0-beta.1` and opm-operator `v1.0.0-beta.4` backfilled, then cli `v1.0.0-beta.6`). This section's deliverable is a publishing operation (the owner's), so its steps are the implementation.

- [ ] 2.1 Owner: dispatch `gh workflow run docs.yml --ref main -f mode=release -f tag=v1.0.0-beta.4` (owner decision 2026-10-03), then check `ghcr.io/open-platform-model/docs/opm-operator` is public and linked to `open-platform-model/opm-operator` (change it in the package settings if not).
- [ ] 2.2 Optional: a fresh operator release (release PR opm-operator#178) publishes through `publish-docs`; it is not needed here. If the release cascade moves the cli's `PinnedOperatorVersion` before cli `v1.0.0-beta.6`, that version needs its bundle first.
- [ ] 2.3 Verify the full, release, minor and major tags of `1.0.0-beta.4` with `cosign verify` and docs-kit C9's identity flags, or an anonymous `opm-docs pull` with a scratch `bundles.cue` naming `opm-operator` under `docs`.
- [ ] 2.4 Record in design.md the run URL, version, digest and the verification; tell the cli that the operator's part of G2-pins holds.
- [ ] 2.5 `openspec validate publish-crd-bundle --strict` passes; `task dev:fmt dev:vet dev:lint dev:test` green, then commit `docs(openspec): record the first operator docs bundle`.

## 3. Retire hack/crdref

Gate G2-switch: opmodel.dev's v1.0 reads core, cli, library and opm-operator from bundles (docs-kit `docs/orchestration.md`).

- [ ] 3.1 `internal/controller/docskit_reconciledby_test.go`: the `reconciledBy` test of design.md D5, with crdref's controller scan moved into it. Verify: it passes, and fails naming the kind when one `Named(...)` is changed locally.
- [ ] 3.2 Delete `hack/crdref/`. `.tasks/dev.yaml`: delete `docs:reference` and `docs:reference:check`. `.github/workflows/lint.yml`: delete the "Generated resource reference is current" step and its comment. The committed page keeps its old block for now; it is excluded from the bundle and no longer read from git.
- [ ] 3.3 `AGENTS.md`: drop the `dev:docs:reference` commands and the marker rule; API marker and `config/samples` edits now need `task docs:bundle:check`, controller renames a `reconciledBy` edit. Verify: `grep -rn "crdref\|docs:reference" --exclude-dir=archive . | grep -v operator-resources.md` finds nothing outside `openspec/changes/`.
- [ ] 3.4 `task dev:fmt dev:vet dev:lint dev:test` and `task docs:bundle:check` green, then commit `ci(docs): retire hack/crdref`.

## 4. Reduce the resource page to its intro

Gate: section 3 is merged. A Markdown-only commit, alone in its PR, so a docs revision of `1.0.0-beta.4` can apply it (design.md D4).

- [ ] 4.1 `docs/site/reference/operator-resources.md`: front matter and intro only, the See-also brief folded into the intro's brief, no marker lines and no `## <Kind>` heading (design.md D4). Nothing else changes in this section. Verify: `git diff --stat` lists only that file; `task docs:bundle:check` passes (the page is still excluded).
- [ ] 4.2 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `docs(site): reduce the operator resource page to its intro`.

## 5. Complete the page from the bundle

Gate: section 4 is merged.

- [ ] 5.1 `docs-kit.cue`: the `markdown` source becomes `{kind: "markdown", dir: "docs/site"}`. Verify: `task docs:bundle:check` passes and the built page starts with the authored front matter (`weight: 7`) and intro, then `## ModuleInstance`.
- [ ] 5.2 design.md D4 records section 4's squash commit as the one the next `1.0.0-beta.4` docs revision must apply first.
- [ ] 5.3 `openspec archive publish-crd-bundle --yes`; then set `openspec/specs/operator-resource-reference/spec.md`'s Purpose to the bundle-built page. Verify: `openspec validate --specs --strict` passes for `docs-bundle` and `operator-resource-reference`.
- [ ] 5.4 `task dev:fmt dev:vet dev:lint dev:test` and `task docs:bundle:check` green, then commit `ci(docs): complete the operator resource page from the bundle`.
