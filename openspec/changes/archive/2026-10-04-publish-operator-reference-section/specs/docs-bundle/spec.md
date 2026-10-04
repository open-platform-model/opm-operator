## MODIFIED Requirements

### Requirement: The operator publishes one docs bundle

The repository SHALL declare one docs-kit project, `opm-operator`, in `docs-kit.cue`: placed in a site version's `/docs/` tree, owning `reference/operator/`, versioned from tags with the prefix `v`, built from a `markdown` source over `docs/site` and a `crd` source over `config/crd/bases` with samples from `config/samples` (picked by kubebuilder file name, those referencing `testing.opmodel.dev` hidden, the two kubebuilder scaffold labels stripped) in the section layout at `reference/operator/`, the section weight 7, one `reconciledBy` entry per kind, and decision citations turned into links. The bundle SHALL carry the authored pages under `docs/site/` and the generated resource reference together (docs-kit DESIGN decision 20). `task docs:bundle` SHALL build it into `out/opm-operator/` and `task docs:bundle:check` SHALL build and lint it without publishing.

#### Scenario: The bundle holds both kinds of page

- **WHEN** `task docs:bundle` runs on a clean checkout
- **THEN** `out/opm-operator/content/` holds the authored pages under `start/`, `operating/` and `diagnostics/`, and under `reference/operator/` the index `_index.md` and one page for each of the four kinds

#### Scenario: The test fixture is not shown as an example

- **WHEN** the ModuleInstance sample `config/samples/opmodel.dev_v1alpha1_moduleinstance.yaml` references `testing.opmodel.dev`
- **THEN** the ModuleInstance page has no Example part, and `opmodel.dev_v1alpha1_moduleinstance_jellyfin.yaml` is not read

#### Scenario: A decision citation stays a link

- **WHEN** a CRD description cites `0015:D3/D16`
- **THEN** the generated page shows `[0015:D3/D16](/enhancements/0015/decisions/)`

#### Scenario: A CRD that is not a CustomResourceDefinition is refused

- **WHEN** a file in `config/crd/bases` holds a document of another kind
- **THEN** `task docs:bundle:check` fails, naming the file
