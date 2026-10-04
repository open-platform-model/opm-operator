## Context

The operator's install shape lives in kustomize today: `config/crd/bases` and `config/rbac/role.yaml` come from `task dev:manifests` (`controller-gen rbac:roleName=manager-role crd ...`, `.tasks/dev.yaml`), the other roles, the ServiceAccount and the bindings are kubebuilder scaffolds in `config/rbac`, the Deployment is `config/manager/manager.yaml` with the metrics argument patched in by `config/default/manager_metrics_patch.yaml`, and `config/default/kustomization.yaml` prefixes every name with `opm-operator-` and puts everything in `opm-operator-system`. `task operator:installer` renders that into `dist/install.yaml`, which the release job uploads (`.github/workflows/release.yml`, job `image-release`) and the cli embeds.

Nothing in CI checks that `config/` is what `task dev:manifests` would generate: `task dev:test` runs `manifests` as a dependency and never diffs the result.

Owner decisions of 2026-10-04 that this change builds on:

- The operator ships as an OPM module published from this repository, on its own version train (the release mechanics are `release-operator-module`).
- `opm operator install` deploys it as a CLI-owned ModuleInstance pulled from a registry; there is no embedded manifest, and an air-gapped cluster uses a mirror.
- The operator's own instance is CLI-owned forever; the operator never reconciles the instance that deploys it.
- The module renders through the catalog's abstractions, not as objects in the manifest's shape. The catalog first gains a seccomp profile and roles with no subjects (split by the supervisor on 2026-10-04 into catalog_opm PR #141, seccomp only, and a later change `add-subjectless-roles`); the first module install over a manifest-installed operator recreates the Deployment once and deletes the three old `*-rolebinding` objects (the cli's migration change).
- The cli pins a module version and records the operator version that module deploys.

Existing enhancement decisions are context, not implemented here. A module's compatibility surface is its `#config` schema, so `#config` below becomes the operator module's compatibility surface: removing or narrowing a field later is a breaking module release. 0021:D4 (being amended in a parallel enhancements PR) places the operator module in the module class with the install manifest as its render; producing that manifest belongs to `release-operator-module`.

The operator's Go module pins library `v1.0.0-beta.1` (`go.mod`), which provides `Kernel.AcquireModuleFromDir`, `Kernel.SynthesizeInstance`, `Kernel.Render` (`library/opm/kernel`) and the platform generator `opm/helper/platformmodule` the Platform reconciler already uses (`internal/platform/layout.go`).

### Evidence: two experiments run on 2026-10-04

The design starts from two throwaway experiments run before this change. They live outside the repositories, so their measured findings are summarised here.

**Experiment 01: render the operator as a catalog-path module, no cluster.** Inputs: operator `v1.0.0-beta.5` sources (`config/` at tag `v1.0.0-beta.5`), core `v2.0.0-beta.2`, catalog opm `4.5.2`, cue `v0.17.1`, opm cli `1.0.0-beta.7`. The module was published only to a local registry under `testing.opmodel.dev`.

- **Object set.** All 19 objects of `dist/install.yaml` rendered: 4 CRDs, 1 Namespace, 1 ServiceAccount, 1 Role, 7 ClusterRoles, 1 RoleBinding, 2 ClusterRoleBindings, 1 Service, 1 Deployment. 16 of 19 names matched. The other three were the bindings: the catalog's `#Role` names a binding after its role, so it rendered `opm-operator-manager-role`, `opm-operator-metrics-auth-role` and `opm-operator-leader-election-role` where the manifest has `...-rolebinding`.
- **No cluster needed.** `opm module build` with `KUBECONFIG=/nonexistent` rendered against a platform generated from the module's own pins; `opm instance build` gave identical output; the published module rendered the same as the local tree.
- **CRDs round-trip.** With each imported CRD `spec` embedded whole in `#CRDs`, all four rendered `spec`s equalled the source YAML by dict equality, and the `controller-gen.kubebuilder.io/version` annotation survived. Exercised: `x-kubernetes-preserve-unknown-fields`, CEL `x-kubernetes-validations`, list-type and list-map-keys, the `status` subresource, printer columns, short names. Negative check: adding `conversion: strategy: None` to a copy refused the render with `field not allowed ... conversion`. The catalog's CRD schema is closed, so embedding the whole spec fails closed where picking fields would drop silently.
- **Spec differences, all on three objects (18 spec-level lines).** The Deployment selector became the catalog's (`app.kubernetes.io/name: controller-manager`, `component.opmodel.dev/name`, `core.opmodel.dev/workload-type: stateless`, `module-instance.opmodel.dev/name`, `control-plane`) instead of `{app.kubernetes.io/name: opm-operator, control-plane: controller-manager}`; the pod template labels changed the same way, and the pods kept `control-plane: controller-manager`, which the e2e suite selects on. `seccompProfile: RuntimeDefault` was missing because the catalog's security context had no seccomp field, so the pod failed Pod Security `restricted`. The rest were API defaults (`RollingUpdate`, `restartPolicy: Always`, `type: ClusterIP`, `automountServiceAccountToken: true`).
- **Unbound roles.** The five administrator ClusterRoles could not go through `#Role`, whose schema required at least one subject, and were rendered as raw objects. A subject-less role through the catalog has not been rendered yet; the catalog_opm change `add-subjectless-roles` adds it, and until a catalog release carries it this module keeps these five roles as raw objects ("The five administrator ClusterRoles stay raw objects until the catalog can render them").
- **Names derive from the instance.** Every name was derived from `#ctx.instance.name` and `.namespace`, which reproduces the manifest only because kustomize's `namePrefix: opm-operator-` equals `<instance>-`. `--name opm -n opm-system` rendered `opm-controller-manager` in `opm-system` with every subject there. The CRDs do not vary with the instance, so the module is a cluster singleton.
- **Drift check.** `cue import -l '#crdSource:' -l metadata.name` produced a definition keyed by name, no `@embed` needed. A regenerate-and-diff script passed against the operator's live `config/`, and failed with a readable diff on a copy with one RBAC verb (`list` on serviceaccounts) and one CRD short name (`plats`) added.
- **`#config`.** Every value rendered into the Deployment: the image, `replicas: 2`, `--registry=...`, `--default-service-account=...` and two extra arguments in that order, the given resources, and a `GOMEMLIMIT`.
- **Cost.** `opm module build` cold (empty CUE cache, deps fetched from GHCR and registry.cue.works): 5.66 s, 507 MB peak RSS; warm: 2.0 to 3.9 s, 494 to 510 MB. The module was 1,977 lines of CUE, 1,713 of them generated.
- **Catalog rough edges.** Each cost a render failure reported only as "N errors in empty disjunction": the CPU string `"2"` passes the resource schema but fails the Deployment transform (write the number `2`); the ServiceAccount's optional `automountToken` is read unguarded (set it); the blueprint requires `restartPolicy` and `updateStrategy`; an `emptyDir` volume needs `readOnly` set. `#ctx: _` had to be re-declared in `module.cue` to be referenced from another file (`reference "#ctx" not found` otherwise).

**Experiment 02: install that module on kind with the cli.** Kubernetes 1.36, the experiment-01 module, the CRDs applied first and then `opm instance apply` of a CLI-owned instance.

- **Bootstrap.** With no Platform the render used the instance's own pins; the apply wrote the Namespace first, then the other 18 objects (`19 applied, 15 created, 4 unchanged`), and the ModuleInstance landed in the Namespace that same apply created. The operator wrote only a `ManagedExternally` status condition on its own instance. All 19 objects, CRDs and Namespace included, were in `status.inventory`.
- **The module must own its Namespace.** `opm instance apply --create-namespace` created the Namespace without OPM labels, and the module's own Namespace then failed the pre-apply existence check as a foreign object.
- **Reinstall** changed nothing: all 19 objects kept their uid and resourceVersion.
- **Upgrade 0.1.0 to 0.2.0** was a re-apply: `15 configured, 4 unchanged`, one new ReplicaSet, 13.9 s for apply and wait, with no Deployment delete, because both versions rendered the same selector.
- **From a manifest install** the apply failed on `Deployment ... spec.selector ... field is immutable`; after deleting the Deployment, the other objects were adopted in place and the three old `*-rolebinding` objects were left behind with nothing to prune them. Handling this is the cli's migration change.
- **Install time** on a fresh cluster equalled today's within noise (28.6 to 31.8 s against 28.7 to 30.7 s over three runs each); render plus CRDs cost 2.3 to 3.5 s of it. Today's first run, 12.8 s, is excluded as an outlier: it was the first run after the node-level image mirror was warmed.

## Goals / Non-Goals

**Goals:**

- A module tree that `opm module publish` would accept, rendering the operator only through catalog resources, with no hand-maintained copy of anything the controller generates.
- Every fixed property of the module (names, selector, instance coordinates, Pod Security, the operator version, the tuning surface) asserted by a test that runs in `task dev:test`.
- A drift check usable both on pull requests and by the future module release against an operator release tag.

**Non-Goals:**

- Publishing, tagging, signing, a release-please package for the module, the install manifest rendered from it, or the end of the operator release's own manifest (`release-operator-module`).
- Adding the module to `task deps:cascade`. The cascade's module loop also advances each fixture module's version and re-pins its consumers, which does not fit a module on its own release train; joining it belongs with the module's release unit. Until then the module's core and catalog pins move by hand with `cue mod get`.
- Anything the cli does: locating the operator, install, the migration from a manifest install, recording values across reinstalls.
- Any controller, API type or RBAC marker change. Reconcile phases (Source, Render, Apply, Prune, Status) are untouched: this change adds a CUE tree, scripts and tests. That the operator refuses to reconcile its own instance is the separate change `refuse-own-instance` (opm-operator PR #211); this change only gates on its release and refuses to name an older operator.
- Moving an instance's ownership between the cli and the operator.

## Decisions

### The module lives at `modules/opm_operator/`

```text
modules/opm_operator/
  cue.mod/module.cue         module: "opmodel.dev/modules/opm_operator@v0"; deps core v2, catalog opm v4
  identity/identity.cue      ModulePath, Version (core #IdentityPackage; tooling writes these two only)
  operator/operator.cue      Version, Image {repository, tag, digest}: the operator release deployed
  module.cue                 metadata, #config, debugValues, the instance-coordinate guard
  components.cue             components, catalog resources only
  zz_generated_crds.cue      #crdSource, from config/crd/bases (generated)
  zz_generated_rbac.cue      #rbacSource, from config/rbac (generated)
  README.md                  what the module is, how to regenerate, what not to edit
```

The directory mirrors the module path's leaf, as `test/fixtures/modules/<leaf>` does for fixtures. The path follows the first-party convention, one flat snake-case leaf under `opmodel.dev/modules/`, which the cli's publish gate admits. It starts on the `v0` major because the controller it deploys, and so its `#config`, is still on its beta line; a `v1` path would make every narrowing an import change for consumers. Alternatives for the directory: `module/` (ambiguous in a repository about ModuleInstances), `config/module/` (`config/` is kustomize's tree and `hack/render-config.sh` copies it whole).

### The operator release the module deploys is its own package

The module and the operator have separate version trains, so one module version deploys exactly one operator version, and the cli must be able to read which. `identity/` stays exactly the two fields core's `#IdentityPackage` gate and the cli's version command own. The operator release goes in a sibling package, readable with `cue eval ./operator -e Version` from the source or from a pulled module, without a render or a cluster:

```cue
package operator

// The operator release this module version deploys. A module release that
// follows an operator release moves all three together.
Version: "1.0.0-beta.5"
Image: {
	repository: "ghcr.io/open-platform-model/opm-operator"
	tag:        "v\(Version)"
	digest:     "sha256:cd48321b1ffb17481fc6818c0afa324ce0312bea4af11f36d44f8b91e63b34b1"
}
```

The values above are the latest published release when this was written, and sit below the eventual minimum operator version (the first release carrying PR #211 is at least `v1.0.0-beta.6`), so the module will never ship them; task 3.2 takes the latest published release at implementation time and reads its digest from GHCR. The tag is derived from `Version`, so the two cannot disagree. Naming the image by digest in the source keeps the image anchor the released manifest has today (the `v1.0.0-beta.5` release asset `install.yaml` pins `:v1.0.0-beta.5@sha256:cd48...`; the release job renders that digest in at release time, and the committed `dist/install.yaml` keeps `controller:latest`) without a publish-time stamp: on its own train the module is committed after the image exists, so the published module can be exactly its tagged source.

### Names are constants and the instance coordinates are fixed

Experiment 01 derived every name from `#ctx.instance`, which reproduces the manifest only for `opm-operator` in `opm-operator-system`. The names must hold on every install path, because the cli locates an operator that has no instance record (applied with kubectl or GitOps) by the fixed names of its Deployment, Namespace and CRDs, and documentation names them. The operator is also a cluster singleton: its CRDs and cluster roles do not vary with the instance, so a second instance under another name would render a second operator over the same CRDs. The module therefore states the names as constants and refuses any other instance:

```cue
#ctx: _ // re-declared: a field of the embedded #Module is not in lexical scope in other files

// The module renders only for these coordinates.
_instanceGuard: "\(#ctx.instance.namespace)/\(#ctx.instance.name)" & "opm-operator-system/opm-operator"
```

A mismatched instance fails unification when the instance is synthesized, naming the given and the expected coordinates (`#module._instanceGuard: conflicting values "opm-system/opm" and "opm-operator-system/opm-operator"`). The Namespace, every `metadata.name` and every subject namespace read the constants. Section 1 measured that a direct constraint on `#ctx.instance.name` and `.namespace` names only the one field that differs, so the module uses the `#config`-independent guard field, which always names both expected coordinates ("Research & Decisions", "Instance-coordinate guard").

The module renders the operator's Namespace itself, so the install records it as an object of the instance; experiment 02 showed that a Namespace created outside the module is refused as foreign.

Alternative: keep deriving names. Rejected: an instance under another name renders a second operator with its own cluster-scoped roles over the same four CRDs, and the cli's fixed-name locator would not find it.

### Every object through a catalog resource

| Objects | Catalog resource |
| --- | --- |
| Namespace | `resources/v1alpha1 #Namespaces` |
| 4 CRDs | `resources/v1beta1 #CRDs`, each imported `spec` embedded whole plus the controller-gen annotation |
| Deployment, ServiceAccount, metrics Service | one `controller-manager` component: `blueprints/v1beta1 #StatelessWorkload`, `#ServiceAccount`, `#Volumes`, traits `#SecurityContext`, `#WorkloadIdentity`, `#GracefulShutdown`, `#PodMetadata`, `#Expose` |
| manager, metrics-auth, leader-election roles and their bindings | `resources/v1beta1 #Role` with one subject each |
| 5 administrator ClusterRoles | `resources/v1alpha1 #Objects` (`objects@v1alpha1`), one component, no binding, until a catalog release carries `add-subjectless-roles`; then `resources/v1beta1 #Role` with `subjects` omitted |

Embedding each CRD's whole `spec` keeps experiment 01's fail-closed property: `#CRDSchema` is closed, so a field it cannot carry (`spec.conversion`) refuses the render instead of being dropped. The operator's first multi-version CRD with a conversion webhook will therefore need a catalog change first, and the render says so. The `controller-manager` component carries `metadata: labels: "control-plane": "controller-manager"`, which the catalog puts in the selector and the pod labels; the e2e suite selects pods by it (`test/e2e/e2e_test.go`).

### The five administrator ClusterRoles stay raw objects until the catalog can render them

`metrics-reader`, the three `moduleinstance-*-role` ClusterRoles and `transformerregistration-admin-role` exist for administrators to bind; the operator binds none of them. The released catalog's `#Role` requires at least one subject and always renders a binding, and the change that makes `subjects` optional, catalog_opm `add-subjectless-roles`, waits for a cli release: the cli's catalog compatibility gate falsely refuses making an open list optional ("default changed" with no authored default), and the supervisor split it out of PR #141 on 2026-10-04 so the seccomp profile ships first. This change does not wait for it. One component, `admin-roles`, renders the five ClusterRoles through `resources/v1alpha1 #Objects`, keyed by their constant names, with their `rules` taken from `#rbacSource` exactly as the `#Role` components take theirs, so the module still holds no hand-written rule. The objects resource validates each ClusterRole against its closed Kubernetes definition and never prefixes a name. No other object goes through it, and the render test asserts that: the `admin-roles` component is the only raw-objects component, and it holds exactly those five ClusterRoles and no binding.

The switch to `#Role` is a follow-up change once a catalog release carries `add-subjectless-roles` (task 6.1 filed it as opm-operator issue #215). It keeps every kind and name, so on a cluster it is an in-place update of five ClusterRoles (their component labels change), not a delete and create, and it is a patch release of the module. That follow-up also restores the spec's "no raw-objects component" rule.

Alternative: wait for the catalog release before implementing. Rejected by the supervisor (2026-10-04): it chains this change behind a cli release and a second catalog release for five objects whose rendered shape is identical either way.

### The module never names an operator that would reconcile its own instance

The instance that deploys the operator is CLI-owned forever, and `refuse-own-instance` (opm-operator PR #211) makes the operator refuse to reconcile it even when its owner is flipped to `operator` or absent. The operator recognises that instance by three signals: the fixed coordinates `opm-operator` in `opm-operator-system`, the module path `opmodel.dev/modules/opm_operator`, and a recorded inventory holding a CRD of the `opmodel.dev` group, with the rule that only the operator module may ship `opmodel.dev` CRDs. This module produces all three: constant instance coordinates (above), its path, and the four `opmodel.dev` CRDs.

An operator release older than that refusal would adopt its own objects, prune its own Deployment on delete and wedge on its own finalizer (measured in `refuse-own-instance`). So the first operator release that contains PR #211 is the module's minimum operator version. Task 1.1 records its tag and task 3.2 writes it to `hack/operator-module/min-operator-version` (one line, such as `v1.0.0-beta.6`), outside the module tree so it does not ship. The render test compares `"v"+operator.Version` with the file's tag using `golang.org/x/mod/semver` (already a direct dependency), which orders prerelease identifiers numerically, and fails naming both versions when the module names an older operator. The `v` prefix is required: `operator.Version` is bare (`1.0.0-beta.5`) while the file holds a tag, and `semver.Compare` treats an unprefixed string as invalid and lower than every valid version. This check reads only `cue eval ./operator` and the file, so it runs in a case placed before the registry skip and is enforced without GHCR too. `release-operator-module`'s release check reads the same file. The image-bump PR only ever moves forward, so the check guards a hand edit.

Alternative: a `MinVersion` field in `operator/operator.cue`. Rejected: it would ship in every published module as data no consumer reads, and the cli reads the deployed version, not a floor.

Experiment 01's catalog workarounds stay until the catalog fixes them: CPU written as a number, `automountToken: true` set on both identities, `restartPolicy` and `updateStrategy` set explicitly, `readOnly: false` on the `emptyDir` volume.

The cost of the catalog path is accepted by the owner: the binding names and the Deployment selector differ from the manifest's, so the first module install over a manifest-installed operator recreates the Deployment once and deletes the old bindings (the cli's migration change). From then on, every module version renders the same selector (below).

### Generated CRD and RBAC data, with `cue import`

`hack/operator-module/generate.sh` (from experiment 01's generator) concatenates `config/crd/bases/*.yaml` in sorted order and runs `cue import -l '#crdSource:' -l metadata.name`; it does the same for the roles into `#rbacSource`, keyed by unprefixed name. The role files are not a list in the script: it reads the `resources` of `config/rbac/kustomization.yaml` (the list kustomize builds the install from) and imports every listed file whose `kind` is `Role` or `ClusterRole`, eight today (`role.yaml`, `leader_election_role.yaml`, `metrics_auth_role.yaml`, `metrics_reader_role.yaml`, the three `moduleinstance_*_role.yaml`, `transformerregistration_admin_role.yaml`). A role added to the kustomization therefore lands in the regenerated data, and the drift check fails until the module is regenerated; the render test then fails until `components.cue` renders it, because it compares the rendered role set with `#rbacSource`. Experiment 01 imported only the six ClusterRoles and hand-wrote the leader-election and metrics-auth rules; importing all of them leaves the module no hand-written rule.

The script reads its input from `config/` by default and from `SRC=<config dir>` when set, and writes to the module directory by default and to `OUT=<dir>` when set. The drift check uses both, and the module's release gate (`release-operator-module`) regenerates from a tag's tree with `SRC=`. `@embed` is not an option: the kernel loads only `.cue` files of a module tree (`library/opm/internal/sourcetree/sourcetree.go`). The first-party modules that carry CRDs (`cert_manager`, `metallb`) hold them as `cue import` output with a README recipe and no check; the operator's CRDs are the contract the cli writes against, so here drift is a correctness bug and gets a gate.

The generated files carry a `DO NOT EDIT` header and are listed in AGENTS.md "Generated Files And Scaffold Boundaries".

### The drift check has two stages and a release mode

```bash
# PR mode (test.yml): config/ must be current, then the module data must match it.
task dev:manifests && git diff --exit-code -- config/crd/bases config/rbac/role.yaml
hack/operator-module/drift-check.sh                    # regenerate into a temp dir, diff the bodies

# Release mode (for the module's release gate, not wired here):
hack/operator-module/drift-check.sh --ref v1.0.0-beta.5 # git archive <ref> config | extract, then diff
SRC=/path/to/config hack/operator-module/drift-check.sh # or any extracted config/ tree
```

The first stage is new to this repository and catches a forgotten `task dev:manifests`, which would otherwise make the second stage compare the module against a stale `config/`. Release mode exists because a module release must agree with the operator release it deploys, not with `main`: a PR may change a CRD after an operator release and before the module release that names it, and that module release must then refuse to publish. Both run under the `cue` version CI pins (`CUE_VERSION`, `v0.17.1`), since `cue import` formatting is part of the compared bytes.

Alternative: a Go test that compares rendered CRDs with the YAML. Kept as part of the render test (it is the spec's equality), but the byte-level check stays the gate: it names the stale file and needs no registry.

### Pod Security `restricted`

The pod sets `runAsNonRoot: true` and `seccompProfile: {type: RuntimeDefault}` through the `#SecurityContext` trait; the container sets `allowPrivilegeEscalation: false`, `capabilities.drop: [ALL]` and `readOnlyRootFilesystem: true` through the container security context. That is the posture of `config/manager/manager.yaml`, which sets the seccomp profile at pod level only; `restricted` accepts a pod-level profile for every container, so the container does not repeat it, and the parity test below stays a plain comparison. The catalog change as proposed adds an optional `seccompProfile` in the Kubernetes shape to the shared `#SecurityContextSchema`, rendered at pod level from the trait and at container level from `#ContainerSchema.securityContext`; a seccomp profile set on `#StatelessWorkloadSchema.securityContext` is accepted but not propagated by the blueprint, so the module uses the trait. Task 1.1 confirms the field names against the released catalog.

The test proves admission, not field presence: an envtest API server, a Namespace labeled `pod-security.kubernetes.io/enforce: restricted`, and a Pod built from the rendered template must be admitted. PodSecurity is a default-enabled admission plugin of kube-apiserver, and envtest disables only the `ServiceAccount` admission plugin (`controller-runtime` v0.24.1, `pkg/internal/testing/controlplane/apiserver.go`, `defaultArgs`: `"disable-admission-plugins": {"ServiceAccount"}`), so the envtest API server the integration suites already start enforces it. No new Go dependency is needed.

### `#config`

Everything a platform team tunes on the operator becomes a value of its instance, so a reinstall that renders from the recorded values keeps it, where today's post-install Deployment patches are lost on every reinstall (the install guide documents that trap).

```cue
#config: {
	image: repository: string & !="" | *operator.Image.repository
	registry?:              string & !=""   // --registry
	defaultServiceAccount?: string & !=""   // --default-service-account
	resources: res.#ResourceRequirementsSchema & {
		requests: cpu:    _ | *"100m"
		requests: memory: _ | *"256Mi"
		limits: cpu:      _ | *2
		limits: memory:   _ | *"4Gi"
	}
	replicas: int & >=1 | *1
	extraArgs: [...string & !~"^--?(registry|default-service-account|metrics-bind-address|leader-elect|health-probe-bind-address)(=|$)"] | *[]
}
```

Each resource quantity carries its own default, so `resources: limits: memory: "8Gi"` keeps the CPU limit and both requests; a single default for the whole `resources` struct would be dropped by any value that sets one field, and the cli's deep merge of recorded values does not prevent that, since a first install's values may set one field alone. The exact spelling depends on how the released catalog's optional fields unify with a default (a default on an optional field makes it present); task 3.3 confirms it renders, and the render test sets one field alone to prove the rest keep their defaults. The memory limit can be changed but never removed (a CUE default cannot be unset by a value), which matches the manifest's intent that the limit and `GOMEMLIMIT` move together, and means every render has a memory limit to derive `GOMEMLIMIT` from.

`#config` is closed, so `image.tag`, `image.digest` and `image.pullPolicy` are refused: a recorded tag or digest would survive a module upgrade and keep the earlier binary running under a record that names the new module version. Pull policy stays `IfNotPresent` as in `config/manager/manager.yaml`. The rendered image is `"\(#config.image.repository):\(operator.Image.tag)@\(operator.Image.digest)"`, so a mirror needs only the repository value. Arguments render in a fixed order: `--metrics-bind-address=:8443`, `--leader-elect`, `--health-probe-bind-address=:8081`, then `--registry`, `--default-service-account`, then `extraArgs`. The operator parses flags with Go's standard `flag` package (`cmd/main.go`), which accepts `-registry=x` as well as `--registry=x`, so the pattern refuses both spellings. The `extraArgs` pattern keeps the typed field the only place the registry mapping is set, so the mapping readable from the instance's values is the one the operator runs with; only the configured mapping is readable, since the operator does not report the mapping it resolves with.

`GOMEMLIMIT` is not a value. `config/manager/manager.yaml` asks that it move with the memory limit and `TestInstallerManagerMemoryLimits` guards the pair by hand; the module derives it as the floor of 80 percent of `resources.limits.memory`, in MiB. The catalog's memory pattern already refuses every string but `<n>Mi` and `<n>Gi`; the module refuses the other form the catalog admits, a plain number of bytes, with a message naming `Mi` and `Gi`. `4Gi` gives `3276MiB`, the manifest's value; `8Gi` gives `6553MiB`. Because the memory limit always resolves (above), `GOMEMLIMIT` is always defined; the derivation reads the resolved `#config.resources.limits.memory`, never the raw values. Alternative: a `goMemLimit` value as in experiment 01. Rejected: it reintroduces the trap the manifest's comment warns about, on every instance's values.

Alternatives for the surface: only the image and the registry mapping (rejected: every argument left out keeps the reinstall trap, and losing the default service account strands every instance); only free-form extra arguments (rejected: nothing validated, and the mapping a client needs to read is buried in a string); no `extraArgs` at all (rejected: the owner's tuning surface includes additional controller arguments, and without them flags such as `--max-concurrent-renders` or `--cue-cache-dir` fall back into the post-install patch trap; the cost is that the refusal pattern becomes compatibility surface).

### The render test renders against the module's own pins, in Go

The cli will render the operator's instance against a platform generated from the module's own pins, never the cluster Platform, so that repairing the operator never depends on the Platform it serves. `test/integration/operatormodule` renders the module the same way: no cluster, no Platform, a platform generated from the module's own catalog pin.

```go
k := kernel.New(/* CUE_REGISTRY from the environment, as the other registry-backed specs */)
deps := readModuleDeps("../../../modules/opm_operator/cue.mod/module.cue") // catalog opm v4 pin
files, _ := platformmodule.Generate(platformmodule.Input{ /* one entry: the pinned catalog */ })
platformDir := writeTemp(files)
plat, _ := k.AcquirePlatformFromDir(ctx, platformDir)
mod, _ := k.AcquireModuleFromDir(ctx, "../../../modules/opm_operator")
inst, _ := k.SynthesizeInstance(ctx, kernel.InstanceInput{Module: mod, Name: "opm-operator", Namespace: "opm-operator-system", Values: values}) // values: at least {}
res, _ := k.Render(ctx, kernel.RenderInput{Instance: inst, Platform: plat, RuntimeName: "operator-module-test"})
```

It asserts the 19 objects and their names, the fixed selector as a literal, the `admin-roles` component as the only raw-objects component holding exactly the five administrator ClusterRoles, the operator version at or above `hack/operator-module/min-operator-version` (in a case that runs before the registry skip, since it needs no registry), CRD `spec` equality with `config/crd/bases`, role rules equal to `config/rbac`, the operator version read from `./operator` with `cue eval` matching the rendered image, the image and every `#config` effect, the refusals (tag value, a typed flag in `extraArgs` in both spellings, another instance name, a byte-count memory limit), and runs the Pod Security admission check. Two assertions use scratch copies of the module tree: one adds `conversion: strategy: None` to a CRD's imported `spec` and expects a render refusal naming `conversion`; one adds a verb to a copy of `config/rbac/role.yaml`, regenerates into the copy with `SRC=`/`OUT=`, and expects the rendered manager ClusterRole to differ by exactly that verb. The test is written in Ginkgo v2 with Gomega, as AGENTS.md "Testing Style" asks. It follows the repository's registry-backed pattern: it skips when GHCR is unreachable and fails under `OPM_TEST_REGISTRY_FORCE=1`, which PR CI sets.

Section 1 verified that `SynthesizeInstance` renders a module acquired from a directory whose path (`opmodel.dev/modules/opm_operator`) has no published version: the instance package is staged inside the module's own tree ("Research & Decisions", "Render path"). The test therefore renders in Go, and passes a values source on every render (`{}` for the defaults), because synthesis never falls back to `debugValues`.

### The Deployment and Service stay equal to `config/` until the module is the only source

Until `release-operator-module` renders the install manifest from this module, `config/manager/manager.yaml` and `config/default` keep producing `dist/install.yaml`, while the module writes the controller's arguments, probes, environment, volumes and security context by hand through the catalog. Two sources of one shape drift unless something compares them, so the render test does: `task dev:test` gains `:tool:kustomize` as a dependency and passes its path as `KUSTOMIZE`, and the test builds `config/default` with `hack/render-config.sh "$KUSTOMIZE" controller:latest config/default` and compares the default-values render with it.

- Compared: the Deployment's pod spec (containers with their args, env, ports, probes, resources, volume mounts and security context, the pod security context, volumes, service account, termination grace period) and the metrics Service's ports and type.
- Excluded: the image (the module names a release), labels, the selector and the pod template labels (the catalog's, pinned below), fields the catalog sets to Kubernetes API defaults (`strategy`, `restartPolicy`, the Service `type: ClusterIP`, `automountServiceAccountToken: true`), and the binding names.

A mismatch fails naming the field. The test fails rather than skips when `KUSTOMIZE` is unset under `OPM_TEST_REGISTRY_FORCE=1`, as PR CI sets. Alternative: declare `config/manager` frozen. Rejected: nothing would enforce the freeze, and an operator flag added before the module release would silently miss the module. The comparison and `TestInstallerManagerMemoryLimits` go when `release-operator-module` retires the kustomize install.

### The selector is pinned literally

Kubernetes never lets an apply change a Deployment's selector, so a module version that rendered a different one would fail every upgrade with `field is immutable` and force a Deployment delete. The selector comes from the catalog's label derivation, which the module does not control, so the render test pins the first module version's selector as a literal. A catalog or core pin bump that changes it fails CI; the fix is then a catalog change or holding the pin, never a module release with a new selector.

### Commit types stay hidden until the module has a release unit

release-please's single package `.` covers the whole tree (`release-please-config.json`), so a `feat` commit under `modules/` would cut an operator release with no binary change. This change commits as `build(module)`, `test(module)`, `ci` and `docs`. The repository squash-merges with a blank message, so only the PR title reaches `main` and release-please: the PR title must carry a hidden type too, such as `build(module): add the operator's OPM module`. `release-operator-module` adds the module's own package and excludes `modules/opm_operator` from `.`.

## Research & Decisions

### Starting point

**Context**: the module had to exist before the release, cli install and migration changes could be designed against it.
**Explored**: experiment 01 (module, generator, drift check, comparison against `dist/install.yaml`) and experiment 02 (install, reinstall, upgrade, migration and delete of that module on kind), summarised under "Evidence" above.
**Decision**: copy experiment 01's module and scripts and change them as recorded above: constants for names, all eight roles imported, the five administrator ClusterRoles kept as raw objects until the catalog renders roles with no subjects, seccomp through the catalog, image tag and digest out of `#config`, derived `GOMEMLIMIT`, the operator package.
**Rationale**: every catalog workaround and every render cost is already measured there (5.7 s and 507 MB cold, 2 to 4 s warm).

### Catalog path, not manifest-shaped objects

**Context**: experiment 01 also rendered a variant that wrote the Deployment, Service, ServiceAccount and RBAC as one catalog `objects@v1alpha1` component in the manifest's shape: the same 19 names, no spec differences, only label differences, so a migration would recreate nothing.
**Explored**: both variants' comparison output against `dist/install.yaml`.
**Decision**: the catalog path, by owner decision of 2026-10-04.
**Rationale**: OPM's own controller should exercise the catalog's workload and role abstractions; with the seccomp profile in the catalog, only the five unbound ClusterRoles stay raw objects, and those move to `#Role` once a catalog release carries `add-subjectless-roles`. The one-time Deployment recreate and binding cleanup are accepted, and the beta line allows the break.

### Catalog surfaces the module needs

**Context**: the seccomp field and the subject-less role do not exist in catalog opm `4.5.2`, the latest release.
**Explored**: `catalog_opm/src/resources/v1beta1/role.cue` (`#RoleSchema.subjects` requires one subject; the transformer always renders a binding); the catalog's security-context schema has no seccomp field. catalog_opm PR #141 (change `add-seccomp-and-subjectless-roles`) first carried both. Its dry-run publish was refused by the cli's compatibility gate (`spec.role.subjects default changed` with no authored default), a cli false positive. The supervisor split it on 2026-10-04: PR #141 ships `seccompProfile` on `#SecurityContextSchema`, rendered at pod and container level; optional `subjects` moves to `add-subjectless-roles`, gated on a cli release with the fix (cli `fix-compat-unauthored-defaults`).
**Decision**: gate implementation on a catalog release carrying PR #141 (proposal, "Dependencies / gates"); render the five administrator ClusterRoles through `objects@v1alpha1` until a release carries `add-subjectless-roles`, then switch (task 6.1). Task 1.1 records the released version and field names in this section before any module code is written.
**Recorded (task 1.1, 2026-10-04)**: catalog `opmodel.dev/catalogs/opm@v4` **v4.6.0** carries PR #141 and resolves from GHCR (`cue mod get opmodel.dev/catalogs/opm@v4.6.0`). The field is `seccompProfile?: #SeccompProfileSchema` on `resources/v1beta1 #SecurityContextSchema` (`src/resources/v1beta1/container.cue`), a Kubernetes-shaped `{type: "RuntimeDefault"}` that accepts only `RuntimeDefault`; it renders at pod level from `traits/v1beta1 #SecurityContext` (`spec.securityContext.seccompProfile`) and at container level from `#ContainerSchema.securityContext.seccompProfile`. v4.6.0 requires core `v2.0.0-beta.1`; the module pins core **v2.0.0-beta.2**, the release the operator's library (`v1.0.0-beta.4` after the merge of `main`) pins as `schema.DefaultSchemaModule`, and the spike rendered against it. No catalog release carries `add-subjectless-roles` yet: v4.6.0's `#RoleSchema` still has `subjects!: [...] & [_, ...]`.
**Rationale**: the module pins a published release; designing against unreleased field names would fix names the catalog change could still change, and waiting for the second catalog release would block the module on a cli release for five objects.

### Minimum operator version

**Context**: an operator without `refuse-own-instance` reconciles the operator's own instance once its owner is flipped (PR #211's measurements).
**Explored**: a floor field in the module's `operator` package; a floor file outside the module read by the render test and the release check.
**Decision**: gate implementation on an operator release carrying PR #211, record that release in `hack/operator-module/min-operator-version`, and fail the render test (and, in `release-operator-module`, the release check) when the module names an older operator.
**Rationale**: the floor is a release-engineering fact about this repository, not data a consumer of the module needs.
**Recorded (task 1.1, 2026-10-04)**: PR #211 squashed as `8d34b6b`; `git tag --contains 8d34b6b` gives **`v1.0.0-beta.6`**, whose GitHub Release is published (not a draft). It is the minimum operator version and, being the latest release, the version the module deploys: `ghcr.io/open-platform-model/opm-operator:v1.0.0-beta.6@sha256:7871a5dd6c2251196b4ac7ce50136a9491f4004e33036c64fa63a20ffaa9825e` (`crane digest`, equal to the image the release's `install.yaml` asset names).

### Render path (spike, task 1.2)

**Context**: whether the kernel renders a module acquired from a directory whose path has no published version.
**Explored**: a scratch module at `opmodel.dev/modules/opm_operator@v0` (one `#StatelessWorkload` with the `#SecurityContext` trait, one `resources/v1alpha1 #Objects` component holding one unbound ClusterRole), pinned to catalog v4.6.0 and core v2.0.0-beta.2, rendered from Go with `Kernel.AcquireModuleFromDir`, `SynthesizeInstance` and `Render` against a platform from `platformmodule.Generate` over the catalog pin, under the GHCR mapping.
**Decision**: the render test renders in Go through the kernel; the `opm module build` fallback is not needed.
**Rationale**: the render succeeded: `SynthesizeInstance` stages the instance package inside the module's own staged tree, so the import of `opmodel.dev/modules/opm_operator` resolves locally with no published version; the Deployment carried `seccompProfile: {type: RuntimeDefault}` at pod level and the ClusterRole rendered unbound. One requirement surfaced: `SynthesizeInstance` with no values source fails `values: incomplete value _` (it never falls back to `debugValues`), so the test always passes a values source, `{}` for the defaults.

### Instance-coordinate guard (spike, task 1.3)

**Context**: whether a module constraint on `#ctx.instance` surfaces as a readable error naming the expected coordinates.
**Explored**: `#ctx: instance: name: "opm-operator"` and `namespace: "opm-operator-system"` in the scratch module, synthesized as `opm` in `opm-system`; then a hidden guard field unifying `"<namespace>/<name>"` with `"opm-operator-system/opm-operator"`.
**Decision**: the guard field (see "Names are constants and the instance coordinates are fixed").
**Rationale**: the direct constraints fail at synthesis with `#module.#ctx.instance.name: conflicting values "opm" and "opm-operator"`, naming only the first differing field and never the namespace. The guard fails `#module._instanceGuard: conflicting values "opm-system/opm" and "opm-operator-system/opm-operator"` for a wrong name and for a wrong namespace alone (`"other/opm-operator"`), always naming both expected coordinates, and leaves the right coordinates rendering.

## Risks / Trade-offs

- [A catalog release changes how it derives workload selector labels] → the module's selector would change and every upgrade would fail on the immutable selector. The render test pins the selector literally, so the catalog pin bump fails CI; the fix is then a catalog change or holding the pin.
- [`cue import` output differs across `cue` versions] → the drift check would fail with no real drift. Both CI and the task use the pinned `CUE_VERSION`; a `cue` bump regenerates the files in the same PR.
- [The module's core and catalog pins go stale, since `task deps:cascade` does not cover them yet] → the cascade bumps the library in `go.mod`, so a library that raises its core floor fails this module's render test inside the automated cascade PR, not in a module PR. The fix rides that cascade PR: whoever lands it runs `cue mod get` for the module's core (and catalog, if needed) on the cascade branch and checks the selector literal still holds. Accepted until the module joins the cascade.
- [The module's data on `main` track `main`'s `config/` while its image names the last operator release] → expected between an operator release and the next module release; release mode of the drift check is what refuses a module release whose data differ from the release it names.
- [A future multi-version CRD with `spec.conversion`] → the render refuses it; a catalog change to `#CRDSchema` must land first. This is the intended fail-closed behaviour.
- [Implementation waits on a catalog release and an operator release] → the gates are task 1.1, and nothing in sections 2 to 5 can be done meaningfully without them. The subject-less role is deliberately not a gate.
- [The five administrator ClusterRoles render as raw objects] → the objects resource validates them against the closed Kubernetes ClusterRole definition, their rules still come from `#rbacSource`, and the render test confines raw objects to exactly those five. The follow-up of task 6.1 moves them to `#Role` once the catalog can render them; until then this module is not yet the full proof that the catalog carries the operator.
- [Cluster e2e is unavailable on the authoring host (no egress from containers and kind nodes on 2026-10-04)] → the bar is the unit and integration tests here; nothing in this change is applied to a cluster.

## Migration Plan

None. Nothing is published, `dist/install.yaml` and `config/` are unchanged, and no running cluster sees the module until the cli change installs it. Rollback is reverting the PR.
