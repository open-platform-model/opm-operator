---
title: "Install the operator"
description: "Install the OPM operator so the cluster reconciles instances on its own."
type: how-to
weight: 16
---

The OPM operator is a controller that runs in the namespace `opm-operator-system`. It renders and applies every ModuleInstance whose `spec.owner` is absent or `operator`, such as the ones you create with kubectl or through GitOps, and every ModulePackage. The one exception is the instance that deploys the operator itself, once the install deploys the operator as a ModuleInstance: the operator never reconciles that instance, whatever its owner, so re-running the install can always repair the operator. Install it when the cluster should keep instances as declared without anyone running `opm`. A cluster where only the CLI deploys needs only the operator's resource definitions, which `opm operator install --crds-only` installs.

There are two ways to install the operator, and both apply the manifest a release publishes, `install.yaml`. `opm operator install` applies the copy built into the CLI, and `kubectl apply` reads it from the GitHub release.

## Before you begin

- `kubectl` access with cluster-admin rights. The manifest creates the namespace `opm-operator-system`, four resource definitions under `opmodel.dev` (`moduleinstances`, `modulepackages`, `platforms` and `transformerregistrations`), and ClusterRoles and ClusterRoleBindings.
- To install with `opm`: the CLI, configured with `opm config init`. See [Install the CLI](/docs/start/install-the-cli/). The CLI looks up the newest catalog release in the registry before it changes anything on the cluster.
- Network access from the cluster to `ghcr.io`. The operator resolves core, the catalogs and modules from `ghcr.io/open-platform-model`, and it exits at startup when it cannot resolve core.
- To use ModulePackages: Flux's source-controller, installed before the operator. A ModulePackage loads its instance from a Flux OCIRepository, GitRepository or Bucket, and the operator looks for those resource definitions only when it starts. ModuleInstances need no Flux.

## Steps

1. Install the operator.

   With the `opm` CLI, run:

   ```sh
   opm operator install
   ```

   The output should look similar to this, shortened:

   <!-- x-release-please-start-version -->

   ```text
   INFO installing opm-operator
   ...
   ✔ opm-operator v1.0.0-beta.5 installed (embedded, 19 resource(s) applied)
   ```

   <!-- x-release-please-end -->

   `opm operator install` server-side applies the operator release built into the CLI. It waits until the resource definitions are established and the controller has rolled out, then creates the cluster Platform (step 3). Each opm release carries one operator release, which the output names. To install another release, add `--version <tag>`: the CLI downloads that release's `install.yaml` from GitHub and reports it as `fetched`. `--timeout` bounds the whole wait, 5 minutes by default. The command is safe to run again, and it first waits out objects that an earlier uninstall left terminating.

   <!-- x-release-please-start-version -->

   Without the CLI, apply the `install.yaml` asset of a release. Pick a tag from the [operator releases](https://github.com/open-platform-model/opm-operator/releases); this page uses v1.0.0-beta.5:

   ```sh
   kubectl apply --server-side -f https://github.com/open-platform-model/opm-operator/releases/download/v1.0.0-beta.5/install.yaml
   ```

   The manifest pins the controller image by digest, `ghcr.io/open-platform-model/opm-operator:v1.0.0-beta.5@sha256:...`. To check the image's signature, run:

   ```sh
   cosign verify ghcr.io/open-platform-model/opm-operator:v1.0.0-beta.5 \
     --certificate-identity-regexp='^https://github.com/open-platform-model/opm-operator/\.github/workflows/release\.yml@refs/heads/main$' \
     --certificate-oidc-issuer=https://token.actions.githubusercontent.com
   ```

   <!-- x-release-please-end -->

   Do not use the `releases/latest/download/install.yaml` link. Every v1.0.0 operator release is marked Pre-release, GitHub's Latest link skips Pre-releases, and so that link serves the retired v0.7.5 manifest.

2. Wait for the controller to roll out.

   `opm operator install` has already waited. If you applied the manifest with kubectl, run:

   ```sh
   kubectl -n opm-operator-system rollout status deployment/opm-operator-controller-manager --timeout=180s
   ```

   The output should look similar to this:

   ```text
   deployment "opm-operator-controller-manager" successfully rolled out
   ```

3. Create the cluster Platform.

   The operator renders against the cluster's Platform, a cluster-wide resource that pins one build of each catalog. Until the operator has generated the Platform, every ModuleInstance and ModulePackage waits with `Ready=False` and reason `PlatformNotReady`.

   If you installed with `opm operator install`, it has already created the Platform `cluster`, subscribed to the newest release of `opmodel.dev/catalogs/opm@v4`. It leaves an existing Platform untouched, and with `--skip-platform` it creates none.

   Otherwise, write the Platform to a file, `platform.yaml`:

   ```yaml
   apiVersion: opmodel.dev/v1alpha1
   kind: Platform
   metadata:
     name: cluster
   spec:
     type: kubernetes
     registry:
       opmodel.dev/catalogs/opm@v4:
         version: "4.Y.Z"
   ```

   Replace `4.Y.Z` with a published build of the opm catalog, such as the newest on the [opm catalog](/catalogs/opm/4/) page.

   Apply it:

   ```sh
   kubectl apply -f platform.yaml
   ```

   `cluster` is the only name a Platform can have. Each key under `spec.registry` is a catalog path with its major version, and `version` names exactly one published build of that major. `spec.type` is informational. The optional `spec.skewPolicy` decides what happens when a module requires a newer build of core or a catalog than the Platform pins: `Warn`, the default, renders against the Platform's build and reports the skew, and `Refuse` refuses the render. See [Platforms and catalogs](/docs/concepts/platforms-and-catalogs/).

4. Give the operator an identity to apply with.

   The operator applies a module's resources as a ServiceAccount in the instance's own namespace, by impersonation. Its own ClusterRole, `opm-operator-manager-role`, grants nothing on workload kinds such as Deployments and Services. With no ServiceAccount named, the operator applies as itself, the apply is forbidden, and the instance reports reason `ApplyFailed`.

   In each namespace that holds instances, create a ServiceAccount and bind it to a role that covers what the modules render. Then name it in the `spec.serviceAccountName` of each ModuleInstance or ModulePackage. For example, the built-in `edit` ClusterRole, bound in one namespace, covers common namespaced kinds such as Deployments, StatefulSets, Services, ConfigMaps and Secrets:

   ```yaml
   apiVersion: v1
   kind: ServiceAccount
   metadata:
     name: opm-applier
     namespace: shop
   ---
   apiVersion: rbac.authorization.k8s.io/v1
   kind: RoleBinding
   metadata:
     name: opm-applier
     namespace: shop
   roleRef:
     apiGroup: rbac.authorization.k8s.io
     kind: ClusterRole
     name: edit
   subjects:
     - kind: ServiceAccount
       name: opm-applier
       namespace: shop
   ```

   A module that renders cluster-scoped objects needs a ClusterRole bound with a ClusterRoleBinding instead. Bind `cluster-admin` only on a test cluster: the operator can then apply anything a module renders.

   To use one ServiceAccount name in every namespace instead of naming it on each instance, add the manager flag `--default-service-account`:

   ```sh
   kubectl -n opm-operator-system patch deployment opm-operator-controller-manager --type=json \
     -p '[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--default-service-account=opm-applier"}]'
   ```

   `spec.serviceAccountName` still wins where it is set. The ServiceAccount must exist in every namespace that holds an instance, or that instance stalls with reason `ImpersonationFailed`.

5. Point the operator at your module registry.

   The operator resolves core, the catalogs and modules through a CUE registry mapping. By default it maps `opmodel.dev` and `testing.opmodel.dev` to `ghcr.io/open-platform-model`, and every other path to `registry.cue.works`. If your modules are published somewhere else, set the manager flag `--registry` in CUE registry syntax. The flag replaces the default, so keep the `opmodel.dev` entry:

   ```sh
   kubectl -n opm-operator-system patch deployment opm-operator-controller-manager --type=json \
     -p '[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--registry=example.com=registry.example.com,opmodel.dev=ghcr.io/open-platform-model,registry.cue.works"}]'
   ```

   Running `opm operator install` again drops the flags you added: it force-applies the shipped manifest, which restores the manager's original arguments. Add `--default-service-account` and `--registry` again after every install or upgrade.

6. Grant users access to ModuleInstances.

   People who create and edit operator-managed instances need rights on ModuleInstances only; the operator applies the workloads. The manifest ships three ClusterRoles to bind:

   - `opm-operator-moduleinstance-admin-role`: every verb on ModuleInstances.
   - `opm-operator-moduleinstance-editor-role`: create, delete, get, list, patch, update and watch.
   - `opm-operator-moduleinstance-viewer-role`: get, list and watch.

   Each of them can also read a ModuleInstance's status. For example, to let the group `shop-team` edit instances in the namespace `shop`:

   ```sh
   kubectl -n shop create rolebinding shop-team-instances \
     --clusterrole=opm-operator-moduleinstance-editor-role --group=shop-team
   ```

   For people who use the `opm` CLI, `opm operator install --rbac --user <name>` (or `--group <name>`) also creates the ClusterRole `opm-cli-user` and binds it cluster-wide. It grants every verb on ModuleInstances, get, patch and update on their status, and get and list on Platforms. `--user` and `--group` need `--rbac`, and only one of them can be given.

## Check that it worked

```sh
kubectl get platform cluster
```

The output should look similar to this:

<!-- x-release-please-start-version -->

```text
NAME      TYPE         READY   REASON      OPERATOR
cluster   kubernetes   True    Generated   v1.0.0-beta.5
```

<!-- x-release-please-end -->

`Generated` means the operator generated the platform from the subscribed catalogs and built it. `OPERATOR` is the version of the running operator. `READY` `False` with reason `BuildFailed` usually means a subscribed catalog version did not resolve: it is not published, or the cluster cannot reach the registry. The condition's message names the cause. See [Operator conditions](/docs/diagnostics/operator-conditions/).

To read the controller's log:

```sh
kubectl -n opm-operator-system logs deploy/opm-operator-controller-manager
```

The first lines should look similar to this; the second names the core version it resolved:

<!-- x-release-please-start-version -->

```text
INFO	setup	Starting opm-operator	{"version": "v1.0.0-beta.5"}
INFO	setup	OPM core schema resolved	{"version": "..."}
INFO	Flux source CRDs not installed; ModulePackage source watches disabled	{"controller": "modulepackage", "kinds": "OCIRepository,GitRepository,Bucket"}
INFO	setup	Starting manager
```

<!-- x-release-please-end -->

The Flux line appears on a cluster without Flux. If you install Flux after the operator, restart the controller so that it watches Flux sources:

```sh
kubectl -n opm-operator-system rollout restart deployment/opm-operator-controller-manager
```

## Related

- [Operator resources](/docs/reference/operator-resources/)
- [Platforms and catalogs](/docs/concepts/platforms-and-catalogs/)
- [Who owns an instance](/docs/concepts/who-owns-an-instance/)
- [Delete an instance safely](/docs/operating/delete-an-instance-safely/). To remove the operator, run `opm operator uninstall`. It keeps the resource definitions and the namespace, and refuses while any ModuleInstance still carries the operator's cleanup finalizer.
