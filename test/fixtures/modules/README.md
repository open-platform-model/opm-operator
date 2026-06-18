# Example test modules

OPM example modules used as operator test fixtures **and** as ready-to-apply
"getting started" examples. All are authored under the public CUE module path
`opmodel.dev/modules/test/<module>@v0`, which resolves to
`ghcr.io/open-platform-model` under the standard `opmodel.dev` registry mapping
(the same one `core` and `catalog` already require) — so a consumer needs no
extra registry configuration.

| Module      | Workload                | Renders                                                        | Demonstrates                                  |
| ----------- | ----------------------- | ------------------------------------------------------------- | --------------------------------------------- |
| `hello`     | ConfigMaps              | one ConfigMap                                                  | minimal kernel-probe fixture                  |
| `hello-web` | `StatelessWorkload`     | one Deployment                                                 | minimal container workload                    |
| `podinfo`   | `StatelessWorkload`     | Deployment + Service, HTTP `livenessProbe` / `readinessProbe` | stateless web app with health probes          |
| `redis`     | `StatefulWorkload`      | StatefulSet + headless Service + PVC, exec readiness probe    | stateful app with persistence + an exec probe |

Each module declares its own semver in `module.cue` (`metadata.version`),
independent of the operator's release version. On an operator release, CI
publishes any module whose version is not already present, and attaches the
`modulerelease.yaml` manifests to the GitHub Release.

> **Note:** `podinfo` and `redis` pin `opmodel.dev/catalogs/opm@v0.6.0`, the
> first catalog release with headless-Service support (`expose.clusterIP:
> "None"`). They resolve only once that catalog version is published.

## Apply an example against a running operator

Prerequisites: the opm-operator is running in the cluster, a `Platform` named
`cluster` is applied and `Ready` (see
`config/samples/releases_v1alpha1_platform.yaml`), and the controller can
resolve `opmodel.dev/*` from a reachable registry.

```bash
# Deploy the stateless podinfo example (Deployment + Service + probes):
kubectl apply -f test/fixtures/modules/podinfo/modulerelease.yaml

# Deploy the stateful redis example (StatefulSet + headless Service + PVC):
kubectl apply -f test/fixtures/modules/redis/modulerelease.yaml

# Watch the ModuleRelease reconcile and the workload come up:
kubectl get modulerelease -n default
kubectl rollout status deploy/podinfo-podinfo -n default
kubectl rollout status statefulset/redis-redis -n default
```

Each `modulerelease.yaml` bundles a `ServiceAccount` + `Role` + `RoleBinding`
granting the applier just the resource kinds that module renders, plus the
`ModuleRelease` itself. Override module config (image, replicas, persistence,
…) via the `spec.values` field on the `ModuleRelease`.

To remove an example:

```bash
kubectl delete -f test/fixtures/modules/podinfo/modulerelease.yaml
```

## Publishing locally

To exercise the modules against a local registry (e.g. for the registry-backed
integration tests or the local e2e path), publish them with the local mapping:

```bash
CUE_REGISTRY='opmodel.dev=localhost:5000+insecure,registry.cue.works' \
  task examples:publish
```

`task examples:publish` reads each module's declared version and publishes it
if absent (idempotent); `task examples:bundle` collects the manifests into
`dist/` for release upload.
