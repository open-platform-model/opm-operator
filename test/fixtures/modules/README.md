# Example test modules

OPM example modules used as operator test fixtures **and** as ready-to-apply
"getting started" examples. All are authored under the CUE module path
`testing.opmodel.dev/modules/operator/<module>@v0` and published to
`ghcr.io/open-platform-model`, so a consumer needs no extra registry
configuration beyond the canonical mapping — which routes both `opmodel.dev`
(core, catalogs) and `testing.opmodel.dev` (these fixtures) to GHCR.

Fixtures live on the **testing** domain, never under `opmodel.dev/*`. CUE
resolves modules by longest-prefix match on the module path, so a fixture
squatting the production namespace drags that entire prefix — core and the
catalogs included — onto whatever registry serves the fixture. The publish gates
enforce it: `opm module publish` refuses a nested path under `opmodel.dev`
outright.

| Module      | Workload                | Renders                                                        | Demonstrates                                  |
| ----------- | ----------------------- | ------------------------------------------------------------- | --------------------------------------------- |
| `hello`     | ConfigMaps              | one ConfigMap                                                  | minimal kernel-probe fixture                  |
| `hello_web` | `StatelessWorkload`     | one Deployment                                                 | minimal container workload                    |
| `podinfo`   | `StatelessWorkload`     | Deployment + Service, HTTP `livenessProbe` / `readinessProbe` | stateless web app with health probes          |
| `redis`     | `StatefulWorkload`      | StatefulSet + headless Service + PVC, exec readiness probe    | stateful app with persistence + an exec probe |
| `backup_provider` | none              | one cluster-scoped `TransformerRegistration`                  | a provider registering the `backup` catalog fixture (0015:D3) |
| `backup_consumer` | volume + opm `backup` trait | one ConfigMap, rendered by the `backup` catalog          | a consumer of a provider-fulfilled contract   |

`backup_provider` and `backup_consumer` are one set with the catalog fixture
`test/fixtures/catalogs/backup` (`testing.opmodel.dev/catalogs/operator/backup@v0`),
which implements opm's provider-fulfilled backup trait. Apply
`backup_provider` first: its claim is accepted, turns active once the
instance is Ready, and adds the `backup` catalog to the generated platform.
Until then `backup_consumer`'s render is refused, naming the trait. The claim
names the catalog build literally, so a bump of the `backup` catalog re-pins
`version` in `backup_provider/components.cue`; the registry-backed spec
`test/integration/reconcile/backup_fixture_test.go` fails when they drift.
The e2e spec `test/e2e/registration_test.go` resolves that literal live from
GHCR, and PR CI publishes only `-e2e.g<sha>` pre-releases, so a catalog bump
needs two pull requests: one that publishes the new `backup` build, then one
that re-pins the claim. Bumped and re-pinned in one pull request, the claim is
refused `CatalogUnresolved` and the e2e spec fails until the catalog is
released.
An operator accepts the claim only when its library accepts a bare SemVer in
`spec.version` (library v1.0.0-beta.2 or later); earlier ones refuse it
`CatalogUnresolved`.

`backup_provider` is not a pattern for a real provider. Its claim authors
`catalog`, `version` and `provides` as literals, which 0015:D11:R1 rules out:
a real provider builds its claim with opm's `#PreBoundRegistration`, which
derives all three from the catalog the module imports. The fixture deviates on
purpose: `hack/fixtures.sh check` dry-runs every fixture against GHCR before
the tree is seeded, so a module fixture that imports a catalog fixture
version new in the same pull request would need two pull requests: one that
publishes the catalog, then one that pins it (design D1 of the archived change
`2026-10-04-add-active-provider-fixture`). Moving `backup_provider` to
`#PreBoundRegistration` is a follow-up once `backup` 0.1.0 is on GHCR.

Each module declares its own path and semver in its `identity/identity.cue`
package — the single source of both (core `#IdentityPackage`; enhancements 0010
D38 / 0011:D12) — and `module.cue`'s `metadata` block derives from it. Edit the
identity package, never the metadata block. Versions are independent of the
operator's release version.

Publishing goes through `opm module publish` (`hack/fixtures.sh`, the same script
the cli repo carries), so every publish gate runs over these fixtures and a
fixture that violates a gate fails CI instead of shipping.

The same coordinate is served from two places and only the registry mapping
decides which one a consumer sees:

- **PR CI** (`test.yml`) seeds a job-local registry from this tree
  (`task examples:seed`) and runs the registry-backed integration specs with
  `testing.opmodel.dev` mapped to it and everything else on GHCR. A fixture bump
  and every consumer of its version (integration specs, the modulepackage
  fixtures, `config/samples`) land in one PR.
- **On merge** `publish-fixtures.yml` publishes the same coordinate to GHCR
  (`task examples:publish`); releases and the e2e workflow publish there too.
  Every other context resolves fixtures from GHCR.
- **`task examples:check`** keeps the two equal: a fixture whose directory
  changed since `origin/main` must carry a version GHCR does not hold yet,
  because published versions are immutable.

To bump a fixture, run `opm module version set <semver> test/fixtures/modules/<module>`
and `task examples:pin`, which re-pins its `moduleinstance.yaml`, the sibling
modulepackage fixture's `cue.mod` dep and `config/samples`. The integration
specs read the version from the identity package (`test/fixtures/fixtures.go`)
and need no edit. Across the workspace, `task deps:pins:fixtures` at the root
does all of this for a dependency bump.

## Apply an example against a running operator

Prerequisites: the opm-operator is running in the cluster, a `Platform` named
`cluster` is applied and `Ready` (see
`config/samples/releases_v1alpha1_platform.yaml`), and the controller can
resolve `testing.opmodel.dev/*` (these fixtures) and `opmodel.dev/*` (core, the
catalogs) from a reachable registry. The operator's built-in `--registry` default
routes both to GHCR, so no configuration is needed for the published versions.

```bash
# Deploy the minimal hello_web example (one Deployment):
kubectl apply -f test/fixtures/modules/hello_web/moduleinstance.yaml

# Deploy the stateless podinfo example (Deployment + Service + probes):
kubectl apply -f test/fixtures/modules/podinfo/moduleinstance.yaml

# Deploy the stateful redis example (StatefulSet + headless Service + PVC):
kubectl apply -f test/fixtures/modules/redis/moduleinstance.yaml

# Watch the ModuleInstance reconcile and the workload come up:
kubectl get moduleinstance -n default
kubectl rollout status deploy/podinfo-podinfo -n default
kubectl rollout status statefulset/redis-redis -n default
```

Each `moduleinstance.yaml` bundles a `ServiceAccount` + `Role` + `RoleBinding`
granting the applier just the resource kinds that module renders, plus the
`ModuleInstance` itself. Override module config (image, replicas, persistence,
…) via the `spec.values` field on the `ModuleInstance`.

To remove an example:

```bash
kubectl delete -f test/fixtures/modules/podinfo/moduleinstance.yaml
```

## Testing against the tree locally

What PR CI does, on a laptop: start the workspace registry (`task registry:start`
at the workspace root), then

```bash
task dev:test:seeded
```

which seeds `localhost:5000` from `test/fixtures/modules` and runs the unit and
integration tiers with the mixed mapping (`MIXED_CUE_REGISTRY` in `Taskfile.yml`:
only the fixture prefix points at the local registry; core and the catalogs still
resolve from GHCR) and `OPM_TEST_REGISTRY_FORCE=1`. To seed alone:

```bash
task examples:seed CUE_REGISTRY='testing.opmodel.dev=localhost:5000+insecure,opmodel.dev=ghcr.io/open-platform-model,registry.cue.works'
```

`seed` refuses a mapping that points `testing.opmodel.dev` at `ghcr.io`; GHCR is
written by CI only. The script exports both `CUE_REGISTRY` and `OPM_REGISTRY`,
which are read by different tools: `cue` reads the former, `opm` reads only
`--registry` > `OPM_REGISTRY` > `~/.opm/config.cue`.

`task examples:publish` publishes each module at its declared version if absent
(idempotent); `task examples:bundle` collects the manifests into `dist/` for
release upload.
