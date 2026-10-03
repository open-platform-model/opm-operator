Delivery: one PR per section (proposal.md). Each section has its own gate; do not start a section before its gate holds. Gates are defined in docs-kit `docs/orchestration.md`.

## 1. Adopt docs-kit

Gate G2-operator: docs-kit's `add-crd-extractor`, `add-authored-docs` and `generalize-build-assembly` are released. Use the first docs-kit release that carries all three as `vX.Y.Z` below.

- [ ] 1.1 `.opm-docs-version`: `vX.Y.Z`. `.tasks/opm-docs.sh`: copy catalog_opm's byte for byte. `.gitignore`: `/out/` and `/.bin/`.
- [ ] 1.2 `Taskfile.yml`: `tools:opm-docs`, `docs:bundle` (`--project opm-operator --out out`), `docs:pins:check`, `docs:bundle:check`, as catalog_opm has them (design.md D3).
- [ ] 1.3 `docs-kit.cue` exactly as design.md D1; re-read the four `Named(...)` values in `internal/controller` at this commit and use them. Verify: `task docs:bundle` writes `out/opm-operator/`.
- [ ] 1.4 Sample selection (design.md D2): the bundle's ModuleInstance entry has no Example, and the ModulePackage and Platform entries show the samples crdref shows. Verify: the build passes and the three entries match; if the released extractor refuses the two ModuleInstance files or shows the `testing.opmodel.dev` fixture, stop the section and report it to docs-kit (do not move or rename samples).
- [ ] 1.5 Parity: diff `out/opm-operator/content/reference/operator-resources.md` from its first `## ` heading on against the committed page between crdref's markers. Verify: no difference beyond those docs-kit's C18 parity record lists; record the commit and the result in design.md D1. An unlisted difference stops the section.
- [ ] 1.6 `.github/workflows/docs.yml`: catalog_opm's with `project: opm-operator`, tags `vX.Y.Z`, `publish.yml@vX.Y.Z`, the backfill floor `v1.0.0-beta.4` in the dispatch comment. `.github/workflows/release.yml`: `publish-docs` after `image-release` (design.md D3). `.github/workflows/lint.yml`: a "docs-kit pins agree" step running `task docs:pins:check`. Verify: `actionlint` clean; `task docs:pins:check` passes.
- [ ] 1.7 `AGENTS.md`: a "Docs bundles" paragraph under "Registry" or a new heading (PR check, edge on `main`, a bundle per release after `image-release`; preview with `task docs:bundle` or `opm-docs serve`; recover with `gh workflow run docs.yml --ref main -f mode=release -f tag=vX.Y.Z`; fix a released page with `mode=revision`; after the site reads the bundle, an authored fix reaches it only by a release or a revision, and a CRD description fix only by a release), the tasks in "Core Commands", `task docs:bundle:check` in the verification checklist, and `reconciledBy` beside the controller-registration rule. `openspec/config.yaml`: `task docs:bundle:check` as validation gate 5.
- [ ] 1.8 `openspec validate publish-crd-bundle --strict` passes; `task dev:fmt dev:vet dev:lint dev:test` and `task docs:bundle:check` green, then commit `ci(docs): publish the operator docs bundle with docs-kit`.

## 2. Publish release bundles

Gate: section 1 is merged. This section's deliverable is a publishing operation (the owner's), so its steps are the implementation.

- [ ] 2.1 Owner: dispatch `gh workflow run docs.yml --ref main -f mode=release -f tag=v1.0.0-beta.4`, then check `ghcr.io/open-platform-model/docs/opm-operator` is public and linked to `open-platform-model/opm-operator` (change it in the package settings if not).
- [ ] 2.2 Owner: merge the next operator release PR; `publish-docs` publishes its bundle after `image-release`.
- [ ] 2.3 Verify both versions: full, release, minor and major tags with `cosign verify` and docs-kit C9's identity flags, or an anonymous `opm-docs pull` with a scratch `bundles.cue` naming `opm-operator` under `docs`.
- [ ] 2.4 Record in design.md the run URLs, versions, digests and the verification; report the versions to the cli (`PinnedOperatorVersion` must name one of them for gate G2-pins).
- [ ] 2.5 `openspec validate publish-crd-bundle --strict` passes; `task dev:fmt dev:vet dev:lint dev:test` green, then commit `docs(openspec): record the first operator docs bundles`.

## 3. Retire hack/crdref

Gate G2-switch: opmodel.dev's `pull-reference-bundles` section 2 is merged (v1.0 reads the operator from bundles).

- [ ] 3.1 `internal/controller/docskit_reconciledby_test.go`: the `reconciledBy` test of design.md D5, with crdref's controller scan moved into it. Verify: it passes, and fails naming the kind when one `Named(...)` is changed locally.
- [ ] 3.2 Delete `hack/crdref/`. `.tasks/dev.yaml`: delete `docs:reference` and `docs:reference:check`. `.github/workflows/lint.yml`: delete the "Generated resource reference is current" step and its comment.
- [ ] 3.3 `docs/site/reference/operator-resources.md`: front matter and intro only, the See-also brief folded into the intro's brief (design.md D4). `docs-kit.cue`: the `markdown` source becomes `{kind: "markdown", dir: "docs/site"}`. Verify: `task docs:bundle:check` passes and the built page starts with the authored front matter (`weight: 7`) and intro, then `## ModuleInstance`.
- [ ] 3.4 `AGENTS.md`: drop the marker rule and the `dev:docs:reference` commands; API marker and `config/samples` edits now need `task docs:bundle:check`, controller renames a `reconciledBy` edit. Verify: `grep -rn "crdref\|docs:reference" --exclude-dir=archive .` finds nothing outside `openspec/changes/`.
- [ ] 3.5 `openspec archive publish-crd-bundle --yes`; then set `openspec/specs/operator-resource-reference/spec.md`'s Purpose to the bundle-built page. Verify: `openspec validate --specs --strict` passes for `docs-bundle` and `operator-resource-reference`.
- [ ] 3.6 `task dev:fmt dev:vet dev:lint dev:test` and `task docs:bundle:check` green, then commit `ci(docs): retire hack/crdref and the generated block`.
