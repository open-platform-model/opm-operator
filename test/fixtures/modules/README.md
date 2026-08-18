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

Publishing goes through `opm module publish`, so every publish gate runs over
these fixtures — the same pipeline the official templates use, and a fixture that
violates a gate fails CI instead of shipping. On an operator release, CI
publishes any module whose version is not already present and attaches the
`moduleinstance.yaml` manifests to the GitHub Release. To bump a fixture, run
`opm module version set <semver> test/fixtures/modules/<module>` and re-pin its
`moduleinstance.yaml` (`task examples:pin`) and its modulepackage fixture.

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

## Publishing locally

To exercise the modules against a local registry (e.g. for the registry-backed
integration tests or the local e2e path), publish them with the local mapping:

```bash
CUE_REGISTRY='testing.opmodel.dev=localhost:5000+insecure,opmodel.dev=ghcr.io/open-platform-model,registry.cue.works' \
OPM_REGISTRY="$CUE_REGISTRY" \
  task examples:publish
```

Only the fixture prefix needs to point at the local registry; core and the
catalogs still resolve from GHCR. Both variables are required and are read by
different tools: `cue` reads `CUE_REGISTRY`, while `opm` reads `OPM_REGISTRY`
(`--registry` > `OPM_REGISTRY` > `~/.opm/config.cue`) and never consults
`CUE_REGISTRY`.

`task examples:publish` reads each module's declared version from its identity
package and publishes it if absent (idempotent); `task examples:bundle` collects
the manifests into `dist/` for release upload.
