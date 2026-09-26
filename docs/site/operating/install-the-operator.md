---
title: "Install the operator"
description: "Install the OPM operator so the cluster reconciles instances on its own."
type: how-to
sidebar:
  order: 20
---

<!-- One sentence: installing opm-operator puts a controller in namespace `opm-operator-system` that renders and applies every ModuleInstance whose `spec.owner` is absent or `operator` (the ones created with kubectl or GitOps), plus every ModulePackage. You need it when the cluster should reconcile instances without anyone running `opm`. A cluster where only the CLI deploys needs only the CRDs, which "Deploy a module with the CLI" installs with `opm operator install --crds-only`.

There are two supported install paths, both built from the same release asset `install.yaml`: `opm operator install`, which applies a copy embedded in the CLI, and `kubectl apply` of the GitHub release asset. The kustomize tree under `config/` with `task operator:controller:install IMG=...` is the contributor path for a locally built image, not for users.

Check against: opm-operator/README.md, opm-operator/.github/workflows/release.yml, opm-operator/config/default/kustomization.yaml, cli/internal/cmd/operator/install.go, cli/internal/operator/manifest.go -->

## Before you begin

<!-- The state the reader must already be in, as links:
- kubectl access with cluster-admin rights. The manifest creates the Namespace `opm-operator-system`, four CRDs under `opmodel.dev` (moduleinstances, modulepackages, platforms, transformerregistrations), ClusterRoles and ClusterRoleBindings.
- If installing with `opm`: the CLI configured, from the "Deploy a module with the CLI" tutorial, and registry access from your machine. The install looks up the newest catalog release before it touches the cluster.
- Egress from the cluster to ghcr.io. The operator resolves modules, core and catalogs from `ghcr.io/open-platform-model` by default.
- If ModulePackages will load instance packages from Flux sources: Flux's source-controller installed first. The manager checks for the OCIRepository, GitRepository and Bucket CRDs only at startup, and logs "Flux source CRDs not installed; ModulePackage source watches disabled" when they are absent. ModuleInstance needs no Flux.

Check against: cli/internal/operator/dist/install.yaml, opm-operator/cmd/main.go, opm-operator/internal/controller/modulepackage_controller.go, cli/internal/cmd/operator/install.go -->

## Steps

1. Apply the operator manifest.
   <!-- If you use the opm CLI, run `opm operator install`. It server-side applies the manifest embedded in the CLI (opm-operator v1.0.0-alpha.19 in CLI v1.0.0-alpha.21; Verify the pairing at publish time), waits for the CRDs to be Established and the Deployment to roll out, and prints "opm-operator <version> installed (embedded, <n> resource(s) applied)". `--version <tag>` fetches that release's `install.yaml` from GitHub instead ("fetched"). `--timeout` bounds the whole wait (default 5m). Re-running is safe: install is idempotent and waits out objects left terminating by an earlier uninstall.
   Otherwise, run `kubectl apply --server-side -f https://github.com/open-platform-model/opm-operator/releases/download/<tag>/install.yaml`. The release manifest pins the image by digest, `ghcr.io/open-platform-model/opm-operator:<tag>@sha256:...`. Verify: the README's `releases/latest/download/install.yaml` URL. Every operator release is a GitHub prerelease, and GitHub's latest alias skips prereleases, so it likely returns 404. The README's cosign command verifies the image signature, if the author wants to include it.
   Check against: cli/internal/cmd/operator/install.go, cli/internal/operator/install.go, cli/internal/operator/fetch.go, opm-operator/README.md, opm-operator/release-please-config.json, opm-kind-demo/Taskfile.yml -->
2. Wait for the controller to roll out.
   <!-- If you used kubectl, run `kubectl -n opm-operator-system rollout status deployment/opm-operator-controller-manager` (the kind demo passes `--timeout=180s`). `opm operator install` has already waited.
   Check against: cli/internal/operator/dist/install.yaml, opm-kind-demo/Taskfile.yml -->
3. Create the cluster Platform.
   <!-- If you installed with `opm operator install`, it has already created the Platform `cluster`, subscribed to the newest published release of `opmodel.dev/catalogs/opm@v4` ("seeded cluster Platform subscribed to <path> <version>"). `--catalog-prerelease` picks the newest prerelease instead, and `--skip-platform` creates none. An existing Platform is reported and left untouched. If an earlier `opm instance apply` ran on this cluster with no Platform present, it already seeded one from that user's local default platform.
   Otherwise, apply a Platform: `apiVersion: opmodel.dev/v1alpha1`, `kind: Platform`, `metadata.name: cluster` (the only name the CRD admits), `spec.type: kubernetes`, and `spec.registry` keyed by the major-suffixed catalog path, each entry naming one published build in `version`: `opmodel.dev/catalogs/opm@v4` at "4.4.0" and `opmodel.dev/catalogs/k8s@v1` at "1.0.0-alpha.3", as in the operator's sample. Optional: `spec.skewPolicy` Warn or Refuse. Until the Platform is generated, every ModuleInstance and ModulePackage waits with Ready=False, reason PlatformNotReady. Verify: whether a CLI-seeded Platform should also subscribe `k8s@v1`; the CLI seeds only the first catalog.
   Check against: opm-operator/api/v1alpha1/platform_types.go, opm-operator/config/samples/opmodel.dev_v1alpha1_platform.yaml, cli/internal/platform/cluster.go, cli/internal/config/templates.go, opm-operator/internal/status/conditions.go -->
4. Give the operator an identity to apply with.
   <!-- The controller's own ClusterRole `opm-operator-manager-role` grants no verbs on workload kinds. The operator impersonates a ServiceAccount in the instance's namespace. If each ModuleInstance names its own `spec.serviceAccountName`, there is nothing to do at install time: create that ServiceAccount and bind it to a role covering what the module renders. The kind demo binds `opm-applier` to cluster-admin, a demo shortcut to warn against. If you want one ServiceAccount name for every namespace, add `--default-service-account=<name>` to the manager container's args. It must exist in each namespace, or reconcile stalls with reason ImpersonationFailed. Verify: what an instance with neither set does. It falls back to the controller's own identity, which appears to lack the RBAC to create workloads. Also note that the flag help cites `docs/TENANCY.md`, which does not exist.
   Check against: opm-operator/cmd/main.go, opm-operator/internal/reconcile/moduleinstance.go, opm-operator/config/rbac/role.yaml, opm-kind-demo/jellyfin/moduleinstance.yaml -->
5. Point the operator at your module registry.
   <!-- If modules come from anywhere other than `opmodel.dev` and `testing.opmodel.dev` on `ghcr.io/open-platform-model`, add `--registry=<mapping>` to the manager args. It uses the CUE_REGISTRY syntax. The default is `testing.opmodel.dev=ghcr.io/open-platform-model,opmodel.dev=ghcr.io/open-platform-model,registry.cue.works`, and an empty value falls back to the `OPM_REGISTRY` env var. Verify: whether a later `opm operator install` resets edited manager args, and how the operator authenticates to a private registry.
   Check against: opm-operator/cmd/main.go, opm-operator/config/manager/manager.yaml -->
6. Grant users access to ModuleInstances.
   <!-- If people will manage instances with the opm CLI without cluster-admin, run `opm operator install --rbac --user <name>` (or `--group <name>`). It creates the ClusterRole `opm-cli-user` (all verbs on moduleinstances, get/patch/update on moduleinstances/status, get/list on platforms) and a ClusterRoleBinding. `--user` and `--group` need `--rbac` and exclude each other. Otherwise, bind one of the shipped ClusterRoles: `opm-operator-moduleinstance-admin-role`, `opm-operator-moduleinstance-editor-role` or `opm-operator-moduleinstance-viewer-role`. Verify: the rules of the three shipped roles.
   Check against: cli/internal/operator/rbac.go, cli/internal/cmd/operator/install.go, opm-operator/config/rbac/moduleinstance_admin_role.yaml, opm-operator/config/rbac/moduleinstance_editor_role.yaml, opm-operator/config/rbac/moduleinstance_viewer_role.yaml -->

## Check that it worked

<!-- Run `kubectl get platform cluster`. Success: TYPE `kubernetes`, READY `True`, REASON `Generated`, OPERATOR showing the installed version (v1.0.0-alpha.19). READY False with reason BuildFailed means a subscribed catalog version did not resolve: it is not published, or ghcr.io is unreachable. Point to "Operator conditions" for that. Check the manager's logs with `kubectl -n opm-operator-system logs deploy/opm-operator-controller-manager`.

Check against: opm-operator/api/v1alpha1/platform_types.go, opm-operator/internal/status/conditions.go, opm-kind-demo/QUICKSTART.md -->

## Related

<!-- The reference page "Operator resources" (the ModuleInstance, ModulePackage and Platform fields) and the concept page "Platforms and catalogs". For removing the operator, point to "Delete an instance safely": `opm operator uninstall` refuses while any ModuleInstance carries the `opmodel.dev/cleanup` finalizer.

Check against: opm-operator/api/v1alpha1/moduleinstance_types.go, opm-operator/api/v1alpha1/platform_types.go, cli/internal/cmd/operator/uninstall.go -->
