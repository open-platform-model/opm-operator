## Context

Three registry-backed specs need a second catalog the generated platform can pin, import and build
next to `opmodel.dev/catalogs/opm@v4`: as a claim's contribution (`platform_claims_test.go`,
`platform_claim_watch_test.go`) and as a disabled subscription (`platform_controller_test.go`). They
use the retired `opmodel.dev/catalogs/k8s@v1` at `1.0.0-alpha.2`. The repo's other fixtures are
modules under `test/fixtures/modules`, published by `hack/fixtures.sh` (byte-identical with the cli)
to `testing.opmodel.dev/modules/operator/*`. PR CI (`test.yml`) seeds a job-local registry from the
tree; `publish-fixtures.yml` publishes to GHCR on merge.

Reconcile phase impact: none. Source, Render, Apply, Prune and Status code is untouched; only test
inputs and test tooling change.

## Goals / Non-Goals

**Goals:**

- A catalog the specs own, published on the testing domain, that a platform can subscribe beside
  `opmodel.dev/catalogs/opm@v4`.
- Published through the one fixture flow, so its publish gates, changed-implies-bumped check and PR
  seeding are the ones the module fleet already has.
- The cli copy of the flow stays byte-identical.

**Non-Goals:**

- A catalog fixture in the cli. Nothing there needs one; the cli gains the capability only because
  the script is shared.
- Teaching the workspace `task deps:pins:fixtures` the catalog root (workspace follow-up).
- Exercising the test catalog's transformer in a render. The specs build the platform; no module
  renders against `probe`.

## Decisions

### D1. Coordinate: `testing.opmodel.dev/catalogs/operator/provider@v0` at `0.1.0`

The module fleet's convention (`testing.opmodel.dev/modules/<repo>/<name>@v0`) with the `catalogs`
kind segment. `provider` names the role the specs give it: the catalog a provider's
`TransformerRegistration` claim contributes. `@v0` and `0.1.0`: a fixture never promises
compatibility, and the catalog compat gate treats a `v0` build as alpha-exempt.

### D2. Content: the smallest valid `#Catalog`

```text
test/fixtures/catalogs/provider/
  cue.mod/module.cue                  module path above, source self, core v2.0.0-beta.1
  identity/identity.cue               ModulePath, Version (literal), RegistryPath, kindPrefix
  resources/v1alpha1/probe.cue        #ProbeResource  fqn <RegistryPath>/resources/probe@v1alpha1
  transformers/probe_transformer.cue  #ProbeTransformer  fqn <RegistryPath>/transformers/probe-transformer@0.1.0
  catalog.cue                         package provider: c.#Catalog, #resources, #transformers
```

Every FQN interpolates `id.kindPrefix`, so it hangs off
`testing.opmodel.dev/catalogs/operator/provider` and cannot collide with an `opm@v4` key. The
transformer requires the resource and renders `spec.probe.data` as one ConfigMap. It imports no
Kubernetes schema, so the catalog adds no transitive dependency to the platform closure. Core is
pinned at `v2.0.0-beta.1`, the library's `schema.DefaultSchemaVersion()`: a higher pin would raise
the closure's core and break the "core pin follows the library" assertion in
`platform_controller_test.go`.

### D3. One flow: a catalog root in `hack/fixtures.sh`

```bash
CATALOGS_DIR=${CATALOGS_DIR:-}            # default: $(dirname "$FIXTURES_DIR")/catalogs when it exists
fixture_dirs()  # catalogs first, then modules
kind_of <dir>   # "catalog" under CATALOGS_DIR, else "module"
"$OPM_BIN" "$kind" publish [--dry-run] "$dir"
"$OPM_BIN" "$kind" version set "<ver>-<prerelease>" "$stagedcopy"
```

`pins`, `check`, `seed` and `publish` iterate both roots; the progress line names the kind
(`==> provider (catalog): publishing v0.1.0`) and the bump hint names the right command. `consumers`
also skips cue.mods under the catalog root in its unlisted-consumer scan. The operator's tasks pass
`FIXTURES_DIR=test/fixtures/modules`, so the default finds `test/fixtures/catalogs` with no Taskfile
change. In the cli, `tests/fixtures/catalogs` does not exist, so `CATALOGS_DIR` stays empty and every
subcommand behaves as before. Catalogs go first so a module fixture may one day depend on a catalog
fixture.

`test/fixtures/fixtures.go` (shared) gains the Go spelling of the same root:

```go
func CatalogDir() (string, error)                // filepath.Dir(Dir()) + "/catalogs"
func LoadCatalog(name string) (Coordinate, error) // identity of <CatalogDir()>/<name>
func MustCatalog(t Failer, name string) Coordinate
```

`Load` and `LoadCatalog` share one unexported `loadIdentity`. `fixtures_test.go` (shared) checks
every catalog fixture loads and sits under `testing.opmodel.dev/catalogs/`, skipping when the root
is absent, and extends the identity-literal check to catalogs.

### D4. Tests read the coordinate, never a literal

```go
// platform_claims_test.go
func claimCatalogPath() string    { return fixtures.MustCatalog(GinkgoT(), "provider").ModulePath }
func claimCatalogVersion() string { return fixtures.MustCatalog(GinkgoT(), "provider").Version }

// platform_controller_test.go
second := fixtures.MustCatalog(GinkgoT(), "provider")
Registry: map[string]releasesv1alpha1.Subscription{
	catalogPath:       {Version: fixtures.CatalogVersion()},
	second.ModulePath: {Version: second.Version, Enable: &disabled},
}
```

The constants become functions because `GinkgoT()` is only valid inside a running spec. The
platform-controller spec also asserts the disabled entry's pin (`v` + `second.Version`), which the
old literal never checked.

## Research & Decisions

### Can the shared flow publish a catalog, or is an operator-local path needed?

**Context**: the brief allowed an operator-local publishing path only if extending the shared
script were disproportionate.
**Explored**: `opm catalog publish` and `opm module publish` share one pipeline
(`cli/internal/publish`): same plan output, same `already holds` refusal and `N refusal` summary
that `only_already_published` matches, same exit codes. `opm catalog version set` exists for the
pre-release staging. The namespace and kind-segment gates bind only inside `opmodel.dev` and
`community.opmodel.dev` (`cli/internal/publish/gates.go`), so a `testing.opmodel.dev/catalogs/...`
path is unconstrained.
**Decision**: extend the shared script (D3).
**Rationale**: the change is a kind switch at three call sites plus a second root; a second
publishing path would duplicate the gates, seeding and changed-implies-bumped logic this repo
already keeps in one place.

### Does the fixture pass the gates and drive the specs?

**Context**: unverified until run.
**Explored**: with `opm` built from cli `main` (`db4f9fd9`) and with the CI pin
(`.opm-cli-version`, `v1.0.0-beta.4`); a throwaway `registry:2` on `localhost:5055` (removed
afterwards), mapping `testing.opmodel.dev=localhost:5055+insecure,opmodel.dev=ghcr.io/open-platform-model,registry.cue.works`.

| Run | Result |
| --- | --- |
| `opm catalog publish --dry-run` (both binaries) | GO: 2 members, 0 refused; compat 1 alpha-exempt |
| `hack/fixtures.sh check`, upstream GHCR | catalog GO (absent upstream), four modules ok, rc 0 |
| `hack/fixtures.sh check`, upstream = seeded registry | FAIL provider: changed since origin/main but v0.1.0 already published; names `opm catalog version set`; rc 1 |
| `hack/fixtures.sh seed`, twice | first pushes catalog + four modules; second reports all five already present, rc 0 |
| `seed` with `PRERELEASE=e2e.gtest123` | pushes `v0.1.0-e2e.gtest123`; tree unchanged |
| claim-watch integration spec, seeded mapping, `OPM_TEST_REGISTRY_FORCE=1` | 1 passed |
| same spec, GHCR mapping, fresh CUE cache | fails after 90s: the catalog does not resolve (the spec really builds it) |
| `internal/controller` Platform specs, seeded mapping | 49 passed |

**Decision**: no spike section; the table is the proof.
**Rationale**: every assumption in D1-D4 is measured above.

### Where the window between merge and GHCR bites

**Context**: the specs resolve the catalog from whatever `testing.opmodel.dev` maps to.
**Explored**: `test.yml` seeds from the tree on every pull request and on every push to `main`;
`publish-fixtures.yml` runs on a push to `main` touching `test/fixtures/catalogs/**` or
`hack/fixtures.sh`.
**Decision**: accept the window; state it in the PR body.
**Rationale**: only a GHCR-backed `task dev:test` started before `publish-fixtures.yml` finishes
sees the gap; `task dev:test:seeded` never does.
