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

Each module declares its own path and semver in its `identity/identity.cue`
package — the single source of both (core `#IdentityPackage`; enhancements 0010
D38 / 0011 D12) — and `module.cue`'s `metadata` block derives from it. Edit the
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
