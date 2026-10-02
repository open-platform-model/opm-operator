## Why

The site's Reference tab carries a stub page for the operator's four resource kinds, `docs/site/reference/operator-resources.md`, and nothing fills it. The workspace page rules (`STYLE.md`, "Site Pages", owner decisions 2026-10-02) say every reference fact derivable from a CRD is generated in the owning repository, committed under `docs/site/reference/` between marker comments, and guarded by a check that fails when it is stale. A generated entry states only what its source proves, tags a rule with what enforces it only where that is derivable, and follows one order: summary, at a glance, spec, example, notes, served by, enforcement.

Writing the page by hand would go stale with the next CRD change, and the CRDs already carry every field, type, constraint and doc comment the page needs.

## What Changes

- **`hack/crdref`** (new Go command): reads `config/crd/bases`, `config/samples` and `internal/controller`, and writes one entry per kind into the generated block of `operator-resources.md`. With `-check` it writes nothing and fails when the committed page differs.
- **Tasks**: `task dev:docs:reference` (regenerates the CRDs, then the page) and `task dev:docs:reference:check`. The `Lint` workflow runs the check; a unit test in `hack/crdref` also fails `task dev:test` on a stale page.
- **Samples are proven current**: a new `test/integration/crdvalidation` spec dry-run creates every `opmodel.dev` object in `config/samples` with strict field validation, so an example on the page is always one the API server admits.
- **Kind doc comments** (`api/v1alpha1`): the four kinds' doc comments open with a sentence that says what the kind does, in place of kubebuilder's "X is the Schema for the xs API", because that first sentence becomes the entry's summary. The scaffold condition list on `ModuleInstance.status.conditions` (Available, Progressing, Degraded, types the operator never sets) is dropped, and one decision citation takes the `0015:D3/D16` form. CRDs and `dist/install.yaml` are regenerated to match.
- **`AGENTS.md`**: the two tasks, the marker rule, and that a types or samples edit regenerates the page.

Release class: none, before and after GA. API types change only in doc comments; no field, marker, schema rule, controller or reconcile behavior changes. The CRD descriptions in `dist/install.yaml` change, which is documentation. Every commit is `docs`, `test`, `ci` or `chore`. Complexity (Principle VII): one generator command and one integration spec; the alternative is a hand-written page the owner's rule forbids.

## Capabilities

### New Capabilities

- `operator-resource-reference`: the generated operator resource reference page, its sources, its entry shape, and the checks that keep it and its examples current.

### Modified Capabilities

(none)

## Impact

- Files: `hack/crdref/` (new), `test/integration/crdvalidation/samples_test.go` (new), `.tasks/dev.yaml`, `.github/workflows/lint.yml`, `AGENTS.md`, `api/v1alpha1/*_types.go` (doc comments), `config/crd/bases/*`, `dist/install.yaml`, `docs/site/reference/operator-resources.md`.
- Site: `opmodel.dev` builds the operator's pages from the branch head, so the page reaches the site after merge with no release. Each enhancement decision a doc comment cites becomes a link to `/enhancements/<NNNN>/decisions/`; the site build fails on a link to an enhancement it does not serve.
- Cross-repo: none. No other repository reads the generator or the page source.
