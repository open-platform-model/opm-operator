## Why

opmodel.dev builds the operator's pages from this repository's git tree: the authored pages under `docs/site/` and `docs/site/reference/operator-resources.md`, whose entries `hack/crdref` generates between marker lines. docs-kit phase 2 moves every product repository to a signed docs bundle built, linted and published on each release, which a site version pulls through the cli's pins (docs-kit DESIGN decision 10). docs-kit's `crd` extractor (contract C18) is crdref ported into docs-kit, with two differences that land here: the controller of each kind is stated in config (`reconciledBy`) instead of scanned from Go, and the decision-citation links crdref writes come from docs-kit's `citations: "link"` policy.

The owner decided one cutover per repository (docs-kit DESIGN decision 20): the bundle carries the operator's whole `docs/site/` with the generated reference from adoption on. While the site still reads the operator from git, the committed resource page (with crdref's block) is left out of the bundle by the `markdown` source's `exclude`, and the `crd` source writes the page alone. Once the site reads the bundle, crdref and the generated block go, and the authored front matter and intro complete the generated page (a completable page, C15).

Sequence and contracts: docs-kit `docs/orchestration.md` (phase 2, "opm-operator: `publish-crd-bundle`") and `https://github.com/open-platform-model/docs-kit/blob/main/docs/contracts.md` (C5, C6, C9, C12, C15, C18). Until a docs-kit change lands, its `openspec/changes/<change>/design.md` on docs-kit `main` shows the contract.

Delivery: one PR per section (cli needs the docs/opm-operator 1.0.0-beta.4 bundle after section 2)

## What Changes

- **Section 1, adopt (gate G2-operator, including docs-kit's sample-selection fix).** `docs-kit.cue` declares the project `opm-operator`: docs placement owning `reference/operator-resources.md`, a `markdown` source over `docs/site` excluding that page, and a `crd` source over `config/crd/bases` and `config/samples` with `weight: 7`, `hideSamplesMatching: ["testing.opmodel.dev"]`, `stripLabels` for the two kubebuilder scaffold labels crdref strips today, `reconciledBy` for the four kinds (`moduleinstance`, `modulepackage`, `platform`, `transformerregistration`, as `internal/controller` names them today) and `citations: "link"`. `.opm-docs-version`, `.tasks/opm-docs.sh`, the tasks `tools:opm-docs`, `docs:bundle`, `docs:pins:check` and `docs:bundle:check`, `.github/workflows/docs.yml`, and `publish-docs` in `release.yml` after `image-release`. A parity check of the generated entries against crdref's block, and a local dry run of the `v1.0.0-beta.4` backfill. `AGENTS.md` gains a "Docs bundles" paragraph. crdref and its check stay.
- **Section 2, the bundle the cli pins (owner, part of gate G2-pins).** A dispatched backfill of `v1.0.0-beta.4`, the version the cli's `main` pins (owner decision 2026-10-03); a fresh release is optional. Record the runs.
- **Section 3, retire crdref (gates G2-switch and G2-edge).** G2-edge (docs-kit orchestration) holds once opmodel.dev's `sources-main` job reads the operator's `main` from the `edge` docs bundle, so sections 4 and 5 can change the committed page without breaking that job's check of every `main` together. Section 3 itself leaves the page alone; it takes the gate because docs-kit's step 8 starts with it. Delete `hack/crdref/`, `dev:docs:reference` and its check, and the `Lint` step; a test that keeps `reconciledBy` equal to the controllers' `Named` names.
- **Section 4, reduce the page (after section 3).** `operator-resources.md` becomes its front matter and intro (no markers, no `## <Kind>` heading) in a Markdown-only commit of its own, so a later docs revision of the backfilled `1.0.0-beta.4` can apply it (its tree has no `docs-kit.cue`, so a revision uses `main`'s config; `1.0.0-beta.5` and later read their own config, keep the exclude and need no reduction commit).
- **Section 5, complete the page.** Remove the `exclude`, so the intro completes the generated page; archive.

## Capabilities

### New Capabilities

- `docs-bundle`: how the operator's docs bundle is configured, checked and published, and how its docs-kit release is pinned.

### Modified Capabilities

- `operator-resource-reference`: the resource reference is generated into the docs bundle by docs-kit, completed by the authored intro; crdref, its markers and its staleness check are gone; the sample admission test and a `reconciledBy` test remain.
- `deps-cascade`: the cascade no longer regenerates the resource page after moving a sample (added at section 3: `task deps:cascade`, merged after this plan, ran crdref).

## Impact

**SemVer: none (after GA as now).** No API type, CRD, controller or reconcile phase changes; Source, Render, Apply, Prune and Status are untouched. Every commit is `ci:`, `docs:` or `test:`, all hidden from release-please (`AGENTS.md`, "Commit type decides the release"), so the change cuts no operator release and nothing cascades to the cli. Principle VII: the change removes a generator (about 950 lines with its test) and adds a config file, a workflow and a small test.

**Affected:** `docs/site/reference/operator-resources.md`, `hack/crdref/`, `.tasks/dev.yaml`, `.github/workflows/{lint,release,docs}.yml`, `Taskfile.yml`, a new test beside `internal/controller`. No API type or controller.

**Downstream consumers:**

| Consumer | What it has to do |
| --- | --- |
| cli | Nothing: `PinnedOperatorVersion` is already `v1.0.0-beta.4`, which section 2 backfills (gate G2-pins). |
| opmodel.dev | Nothing until `pull-reference-bundles`; v1.0 then reads `docs/opm-operator` through the cli's pins (gate G2-switch); G2-edge holds once `add-edge-build` switches `sources-main` to edge bundles. crdref's retirement waits for both. After section 4, opmodel.dev's local explicit-mode builds read a reduced page with no kind sections; `task build:edge` is the local check. |

**Owner items:** merging docs-kit's release PRs (gate G2-operator), the `v1.0.0-beta.4` backfill dispatch, checking `ghcr.io/open-platform-model/docs/opm-operator` is public on first push.
