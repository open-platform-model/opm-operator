# opm_operator

The opm-operator's own OPM module, `opmodel.dev/modules/opm_operator@v0`. It renders the operator's whole install through the first-party catalog (`opmodel.dev/catalogs/opm@v4`), on a version train of its own: the module version (`identity/identity.cue`) is not the operator version.

This repository does not publish it yet. Until the module's release change lands, `config/` and `task operator:installer` still produce the operator's `install.yaml`, and the render test keeps the two equal.

## What it renders

19 objects, for the instance `opm-operator` in `opm-operator-system` only (any other name or namespace is refused, naming the expected coordinates):

| Objects | Catalog resource |
| --- | --- |
| Namespace `opm-operator-system` | `resources/v1alpha1 #Namespaces` |
| The four `opmodel.dev` CRDs | `resources/v1beta1 #CRDs`, each generated `spec` embedded whole |
| Deployment and ServiceAccount `opm-operator-controller-manager`, Service `opm-operator-controller-manager-metrics-service` | `blueprints/v1beta1 #StatelessWorkload` with `#ServiceAccount`, `#Volumes` and traits |
| ClusterRoles `opm-operator-manager-role`, `opm-operator-metrics-auth-role`, Role `opm-operator-leader-election-role`, and a binding for each | `resources/v1beta1 #Role` |
| ClusterRoles `opm-operator-metrics-reader`, `opm-operator-moduleinstance-{admin,editor,viewer}-role`, `opm-operator-{platform,modulepackage,transformerregistration}-viewer-role`, `opm-operator-transformerregistration-admin-role`, unbound | `resources/v1alpha1 #Objects` (raw objects) |

The eight administrator ClusterRoles render as raw objects with no binding until a catalog release can render a role with no subjects (catalog_opm `add-subjectless-roles`); their rules still come from the generated RBAC data. Nothing else uses raw objects.

Every name is a constant, the name the operator's own install manifest gives the object; only the bindings take the catalog's names (the role's own). The Deployment's selector is fixed across module versions, and the render test fails if anything changes it.

## Vetting and building it with the cli

The cli names a module's synthetic instance `<name>-debug` in `default`, which this module refuses. Give it the module's coordinates (`module vet` takes `--instance-name`, `module build` takes `--name`):

```bash
opm module vet . --instance-name opm-operator -n opm-operator-system
opm module build . --name opm-operator -n opm-operator-system
```

## Values (`#config`)

| Field | Default | Effect |
| --- | --- | --- |
| `image.repository` | `ghcr.io/open-platform-model/opm-operator` | The image repository, for a mirror. The tag and digest stay the module's. |
| `registry` | unset | `--registry=<value>`: the CUE registry mapping the operator resolves modules through. |
| `defaultServiceAccount` | unset | `--default-service-account=<value>`: the identity the operator applies as when a ModuleInstance names none. |
| `resources` | requests `100m` CPU, `256Mi`; limits `2` CPU, `4Gi` | The manager container's resources. Each quantity defaults on its own. CPU is a number of cores (`4`, `0.5`) or a millicore string (`"500m"`); a string of cores such as `"4"` is refused. The memory limit is given as `<n>Mi` or `<n>Gi`; `GOMEMLIMIT` is derived as 80 percent of it, in MiB. |
| `replicas` | `1` | The Deployment's replicas. |
| `extraArgs` | `[]` | Further controller arguments, after all of the above. `--registry`, `--default-service-account` and the module's own `--metrics-bind-address`, `--leader-elect` and `--health-probe-bind-address` are refused in either flag spelling. |

The arguments render in this order: `--metrics-bind-address=:8443`, `--leader-elect`, `--health-probe-bind-address=:8081`, `--registry`, `--default-service-account`, then `extraArgs`.

The image tag, digest and pull policy are not values. `operator/operator.cue` names the one operator release this module version deploys, by version, tag and digest; read it without a render with `cue eval ./operator -e Version`. It never falls below `hack/operator-module/min-operator-version`, the first operator release that refuses to reconcile the instance deploying it.

## Regenerating

`zz_generated_crds.cue` and `zz_generated_rbac.cue` are generated from `config/` with `cue import`. Do not edit them. `task dev:manifests` regenerates them together with `config/`, so after an API type or RBAC marker change run:

```bash
task dev:manifests
```

`task operator-module:drift` (run in CI) fails when `config/` is stale against `task dev:manifests`, then when these files are stale against `config/`; `task operator-module:drift REF=<tag>` checks them against a release tag's `config/` instead. A Role or ClusterRole added to `config/rbac/kustomization.yaml` lands in the generated data, and the render test fails until `components.cue` renders it.

The render test is `test/integration/operatormodule`, run by `task dev:test`. It renders this directory against a platform generated from the module's own catalog pin, so it needs the pins from a registry (GHCR by default).
