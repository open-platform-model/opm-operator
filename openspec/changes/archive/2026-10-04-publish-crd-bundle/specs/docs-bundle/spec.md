## ADDED Requirements

### Requirement: The operator publishes one docs bundle

The repository SHALL declare one docs-kit project, `opm-operator`, in `docs-kit.cue`: placed in a site version's `/docs/` tree, owning `reference/operator-resources.md`, versioned from tags with the prefix `v`, built from a `markdown` source over `docs/site` and a `crd` source over `config/crd/bases` with samples from `config/samples` (picked by kubebuilder file name, those referencing `testing.opmodel.dev` hidden, the two kubebuilder scaffold labels stripped), the page weight 7, one `reconciledBy` entry per kind, and decision citations turned into links. The bundle SHALL carry the authored pages under `docs/site/` and the generated resource reference together (docs-kit DESIGN decision 20). `task docs:bundle` SHALL build it into `out/opm-operator/` and `task docs:bundle:check` SHALL build and lint it without publishing.

#### Scenario: The bundle holds both kinds of page

- **WHEN** `task docs:bundle` runs on a clean checkout
- **THEN** `out/opm-operator/content/` holds the authored pages under `start/`, `operating/` and `diagnostics/`, and `reference/operator-resources.md` with one `## <Kind>` entry for each of the four kinds

#### Scenario: The test fixture is not shown as an example

- **WHEN** the ModuleInstance sample `config/samples/opmodel.dev_v1alpha1_moduleinstance.yaml` references `testing.opmodel.dev`
- **THEN** the ModuleInstance entry has no Example part, and `opmodel.dev_v1alpha1_moduleinstance_jellyfin.yaml` is not read

#### Scenario: A decision citation stays a link

- **WHEN** a CRD description cites `0015:D3/D16`
- **THEN** the generated entry shows `[0015:D3/D16](/enhancements/0015/decisions/)`

#### Scenario: A CRD that is not a CustomResourceDefinition is refused

- **WHEN** a file in `config/crd/bases` holds a document of another kind
- **THEN** `task docs:bundle:check` fails, naming the file

### Requirement: Every operator release publishes its docs bundle

`release.yml` SHALL run a `publish-docs` job that calls docs-kit's `publish.yml` with `project: opm-operator`, `mode: release` and the release's tag, in the workflow run of the push that merged the release PR, only when release-please created a release and only after `image-release` succeeded. `publish-release` SHALL NOT wait for it.

#### Scenario: A release publishes its bundle

- **WHEN** the release PR for `v1.0.0-beta.5` merges and `image-release` succeeds
- **THEN** `publish-docs` publishes `ghcr.io/open-platform-model/docs/opm-operator` for `1.0.0-beta.5`, signed by the operator's workflow

#### Scenario: A failed image publishes no docs

- **WHEN** `image-release` fails
- **THEN** `publish-docs` is skipped, and `docs.yml` dispatched with `mode: release` and the tag recovers the bundle once the release is fixed

### Requirement: Pull requests check the bundle and main publishes edge

`.github/workflows/docs.yml` SHALL run `publish.yml` in `check` mode on every pull request (permissions `contents: read`, `packages: read`), in `edge` mode on every push to `main`, and on `workflow_dispatch` in the `release` or `revision` mode with a `tag` and, for `revision`, a `fix` commit; the publishing jobs SHALL hold only `contents: read`, `packages: write` and `id-token: write`, and the workflow SHALL declare `permissions: {}` at the top.

#### Scenario: A pull request that breaks the bundle fails its check

- **WHEN** a pull request adds a link to `/catalogs/opm/4.4/` to a page under `docs/site/`
- **THEN** the `Docs / check` job fails, naming the page and the link, because a docs page reaches the Catalogs tab only through its root or a major (docs-kit C11)

#### Scenario: A merge to main publishes edge

- **WHEN** a commit lands on `main`
- **THEN** the `edge` job publishes that commit as the `edge` build of `docs/opm-operator`

### Requirement: The docs-kit release is pinned once per form and the forms agree

`.opm-docs-version` SHALL hold one docs-kit release tag, and every `open-platform-model/docs-kit/.github/workflows/publish.yml@<ref>` under `.github/workflows/` SHALL name that same tag, never a SHA (docs-kit C5, C9). `task docs:pins:check` SHALL refuse a disagreement offline; the `Lint` workflow and `task docs:bundle:check` SHALL run it.

#### Scenario: A half-moved pin is refused

- **WHEN** `.opm-docs-version` names `v0.4.0` and `release.yml` still names `publish.yml@v0.3.0`
- **THEN** the `Lint` workflow's pins step fails, listing the stale ref

#### Scenario: Agreeing pins pass

- **WHEN** both forms name `v0.4.0`
- **THEN** `task docs:pins:check` passes
