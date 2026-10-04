## Context

The operator's install shape lives in kustomize today: `config/crd/bases` and `config/rbac/role.yaml` come from `task dev:manifests` (`controller-gen rbac:roleName=manager-role crd ...`, `.tasks/dev.yaml`), the other roles, the ServiceAccount and the bindings are kubebuilder scaffolds in `config/rbac`, the Deployment is `config/manager/manager.yaml` with the metrics argument patched in by `config/default/manager_metrics_patch.yaml`, and `config/default/kustomization.yaml` prefixes every name with `opm-operator-` and puts everything in `opm-operator-system`. `task operator:installer` renders that into `dist/install.yaml`, which the release job uploads (`.github/workflows/release.yml`, job `image-release`) and the cli embeds.

Nothing in CI checks that `config/` is what `task dev:manifests` would generate: `task dev:test` runs `manifests` as a dependency and never diffs the result.

Experiment 01 of enhancement 0028 (`enhancements/0028/experiments/01-operator-module-render/`) built a working module of the operator against core `v2.0.0-beta.2` and catalog opm `4.5.2`, with a `cue import` generator and a regenerate-and-diff drift check. It rendered 19 objects with no cluster; through the catalog, three binding names and the Deployment selector differed from the manifest and the pod lost `seccompProfile`, because the catalog had no seccomp field and its role resource required a subject. 0028:D12 adds both to the catalog; this change starts from that module and makes it production-grade.

The operator's Go module pins library `v1.0.0-beta.1` (`go.mod`), which provides `Kernel.AcquireModuleFromDir`, `Kernel.SynthesizeInstance`, `Kernel.Render` (`library/opm/kernel`) and the platform generator `opm/helper/platformmodule` the Platform reconciler already uses (`internal/platform/layout.go`).

## Goals / Non-Goals

**Goals:**

- A module tree that `opm module publish` would accept, rendering the operator only through catalog resources, with no hand-maintained copy of anything the controller generates.
- Every property 0028 fixes on the module (names, selector, Pod Security, the operator version, the tuning surface) asserted by a test that runs in `task dev:test`.
- A drift check usable both on pull requests and by the future module release against an operator release tag.

**Non-Goals:**

- Publishing, tagging, a release-please package for the module, or a release job (the module's release change, 0028:D1, OQ11).
- Rendering `dist/install.yaml` from the module (0028:D2:R9/R13).
- Adding the module to `task deps:cascade` (0028:D7). Until then its pins move by hand with `cue mod get`, in the module's own PRs.
- Any controller, API or RBAC marker change. Reconcile phases (Source, Render, Apply, Prune, Status) are untouched: this change adds a CUE tree, scripts and tests.

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

The directory mirrors the module path's leaf, as `test/fixtures/modules/<leaf>` does for fixtures. Alternatives: `module/` (ambiguous in a repository about ModuleInstances), `config/module/` (`config/` is kustomize's tree and `hack/render-config.sh` copies it whole).

### The operator release the module deploys is its own package

`identity/` stays exactly the two fields core's `#IdentityPackage` gate and the cli's version command own. The operator release goes in a sibling package so it is readable with `cue eval ./operator -e Version` from the source or from a pulled module, without a render or a cluster (0028:D1:R11):

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

The values above are the latest published release when this was written; task 3.2 takes the latest published release at implementation time and reads its digest from GHCR. The tag is derived from `Version`, so the two cannot disagree.

### Names are constants and the instance coordinates are fixed

Experiment 01 derived every name from `#ctx.instance.name` and `#ctx.instance.namespace`, which reproduces the manifest only for `opm-operator` in `opm-operator-system`; `--name opm -n opm-system` rendered `opm-controller-manager` in `opm-system`. 0028:D2:R10 needs the names on every install path, and 0028:D3:R11/R18 make the instance a singleton with those coordinates, so the module states them as constants and refuses any other instance:

```cue
#ctx: _ // re-declared: a field of the embedded #Module is not in lexical scope in other files

let _instance = {name: "opm-operator", namespace: "opm-operator-system"}
#ctx: instance: name:      _instance.name
#ctx: instance: namespace: _instance.namespace
```

A mismatched instance fails unification at render, naming both values. The Namespace, every `metadata.name` and every subject namespace read the constants. Whether a module constraint on `#ctx.instance` surfaces through the kernel as a readable render error is unverified; section 1 checks it. If it does not, the fallback is a `#config`-independent guard field whose failure names the expected coordinates.

Alternative: keep deriving names. Rejected: an instance under another name would render a second operator with its own cluster-scoped roles and the same four CRDs, and the cli's fixed-name locator (0028:D3:R15) would not find it.

### Every object through a catalog resource

| Objects | Catalog resource |
| --- | --- |
| Namespace | `resources/v1alpha1 #Namespaces` |
| 4 CRDs | `resources/v1beta1 #CRDs`, each imported `spec` embedded whole plus the controller-gen annotation |
| Deployment, ServiceAccount, metrics Service | one `controller-manager` component: `blueprints/v1beta1 #StatelessWorkload`, `#ServiceAccount`, `#Volumes`, traits `#SecurityContext`, `#WorkloadIdentity`, `#GracefulShutdown`, `#PodMetadata`, `#Expose` |
| manager, metrics-auth, leader-election roles and their bindings | `resources/v1beta1 #Role` with one subject each |
| 5 administrator ClusterRoles | `resources/v1beta1 #Role` with no subjects (0028:D12:R2) |

Embedding each CRD's whole `spec` keeps the experiment's fail-closed property: `#CRDSchema` is closed, so a field it cannot carry (`spec.conversion`) refuses the render instead of being dropped. The `controller-manager` component carries `metadata: labels: "control-plane": "controller-manager"`, which the catalog puts in the selector and the pod labels; the e2e suite selects pods by it (`test/e2e/e2e_test.go`).

The experiment's catalog workarounds stay until the catalog fixes them (0028:D12 lists them as follow-ups): CPU written as a number, `automountToken: true` set on both identities, `restartPolicy` and `updateStrategy` set explicitly, `readOnly: false` on the `emptyDir` volume.

### Generated CRD and RBAC data, with `cue import`

`hack/operator-module/generate.sh` (from the experiment's `hack/generate.sh`) concatenates `config/crd/bases/*.yaml` in sorted order and runs `cue import -l '#crdSource:' -l metadata.name`; it does the same for the eight role files in `config/rbac` (`role.yaml`, `leader_election_role.yaml`, `metrics_auth_role.yaml`, `metrics_reader_role.yaml`, the three `moduleinstance_*_role.yaml`, `transformerregistration_admin_role.yaml`) into `#rbacSource`, keyed by unprefixed name. The experiment imported only the six ClusterRoles and hand-wrote the leader-election and metrics-auth rules; importing all eight leaves the module no hand-written rule (0028:D2:R2). `@embed` is not an option: the kernel loads only `.cue` files of a module tree (`library/opm/internal/sourcetree/sourcetree.go`).

The generated files carry a `DO NOT EDIT` header and are listed in AGENTS.md "Generated Files And Scaffold Boundaries".

### The drift check has two stages and a release mode

```bash
# PR mode (test.yml): config/ must be current, then the module data must match it.
task dev:manifests && git diff --exit-code -- config/crd/bases config/rbac/role.yaml
hack/operator-module/drift-check.sh                    # regenerate into a temp dir, diff the bodies

# Release mode (for the module's release job, not wired here):
hack/operator-module/drift-check.sh --ref v1.0.0-beta.5 # git archive <ref> config | extract, then diff
```

The first stage is new to this repository and catches a forgotten `task dev:manifests`, which would otherwise make the second stage compare the module against a stale `config/`. Release mode exists because 0028:D2:R1 compares the module with the operator release it deploys, not with `main`: a PR may change a CRD after an operator release and before the module release that names it, and the module release must then refuse (0028:D2:R3). Both run under the `cue` version CI pins (`CUE_VERSION`), since `cue import` formatting is part of the compared bytes.

Alternative: a Go test that compares rendered CRDs with the YAML. Kept as part of the render test (it is the spec's equality), but the byte-level check stays the gate: it names the stale file and needs no registry.

### Pod Security `restricted`

The pod sets `runAsNonRoot: true` and `seccompProfile: {type: RuntimeDefault}` through `#SecurityContext`; the container sets `allowPrivilegeEscalation: false`, `capabilities.drop: [ALL]`, `readOnlyRootFilesystem: true` and the same seccomp profile through the container security context, using the fields 0028:D12:R1 adds. The exact field names come from the catalog release (task 1.1 records them).

The test proves admission, not field presence: an envtest API server, a Namespace labeled `pod-security.kubernetes.io/enforce: restricted`, and a Pod built from the rendered template must be admitted. PodSecurity is a default-enabled admission plugin of kube-apiserver; that envtest's API server enforces it is unverified, so section 1 checks it. If it does not, the test evaluates the pod with `k8s.io/pod-security-admission/policy` against the `restricted` level, the library the admission plugin itself uses.

### `#config`

```cue
#config: {
	image: repository: string & !="" | *operator.Image.repository
	registry?:              string & !=""   // --registry
	defaultServiceAccount?: string & !=""   // --default-service-account
	resources: res.#ResourceRequirementsSchema | *{
		requests: {cpu: "100m", memory: "256Mi"}
		limits: {cpu: 2, memory: "4Gi"}
	}
	replicas: int & >=1 | *1
	extraArgs: [...string & !~"^--(registry|default-service-account|metrics-bind-address|leader-elect|health-probe-bind-address)(=|$)"] | *[]
}
```

`#config` is closed, so `image.tag`, `image.digest` and `image.pullPolicy` are refused (0028:D5:R6); pull policy stays `IfNotPresent` as in `config/manager/manager.yaml`. The rendered image is `"\(#config.image.repository):\(operator.Image.tag)@\(operator.Image.digest)"` (0028:D5:R5). Arguments render in a fixed order: `--metrics-bind-address=:8443`, `--leader-elect`, `--health-probe-bind-address=:8081`, then `--registry`, `--default-service-account`, then `extraArgs`. The `extraArgs` pattern keeps the typed mapping the only place the mapping is set, so the value readable from the instance is the one the operator runs with (0028:D5:R4).

`GOMEMLIMIT` is not a value. `config/manager/manager.yaml` asks that it move with the memory limit and `TestInstallerManagerMemoryLimits` guards the pair by hand; the module derives it as the floor of 80 percent of `resources.limits.memory`, in MiB, accepting `Mi` and `Gi` limits and refusing any other unit with a message. `4Gi` gives `3276MiB`, the manifest's value. Alternative: a `goMemLimit` value as in the experiment. Rejected: it reintroduces the trap the manifest's comment warns about, on every instance's values.

### The render test renders against the module's own pins, in Go

`test/integration/operatormodule` renders the module the way install will (0028:D11): no cluster, no Platform, a platform generated from the module's own catalog pin.

```go
k := kernel.New(/* CUE_REGISTRY from the environment, as the other registry-backed specs */)
deps := readModuleDeps("../../../modules/opm_operator/cue.mod/module.cue") // catalog opm v4 pin
files, _ := platformmodule.Generate(platformmodule.Input{ /* one entry: the pinned catalog */ })
platformDir := writeTemp(files)
plat, _ := k.AcquirePlatformFromDir(ctx, platformDir)
mod, _ := k.AcquireModuleFromDir(ctx, "../../../modules/opm_operator")
inst, _ := k.SynthesizeInstance(ctx, kernel.InstanceInput{Module: mod, Name: "opm-operator", Namespace: "opm-operator-system", Values: values})
res, _ := k.Render(ctx, kernel.RenderInput{Instance: inst, Platform: plat, RuntimeName: "operator-module-test"})
```

It asserts the 19 objects and their names (0028:D2:R10), the fixed selector as a literal (0028:D2:R11), the absence of a raw-objects component (0028:D2:R12), CRD `spec` equality with `config/crd/bases` (0028:D2:R1), role rules equal to `config/rbac` (0028:D2:R2), the image and every `#config` effect (0028:D5:R1/R5), the refusals (tag value, a typed flag in `extraArgs`, another instance name), and runs the Pod Security admission check. It follows the repository's registry-backed pattern: it skips when GHCR is unreachable and fails under `OPM_TEST_REGISTRY_FORCE=1`, which PR CI sets.

Whether `SynthesizeInstance` resolves a module acquired from a directory whose path (`opmodel.dev/modules/opm_operator`) has no published version is unverified: the kernel imports the module by path and version, and the operator's own renders always acquire from a registry. Section 1 checks it. Fallback: the test drives the pinned cli (`.opm-cli-version`, installed by `test.yml`) with `opm module build modules/opm_operator` under `KUBECONFIG=/nonexistent`, which experiment 01 measured renders against the module's own pins, and parses its YAML.

### Commit types stay hidden until the module has a release unit

release-please's single package `.` covers the whole tree (`release-please-config.json`), so a `feat` commit under `modules/` would cut an operator release with no binary change. This change commits as `build(module)`, `test(module)`, `ci` and `docs`. The module's release change adds its own package and excludes `modules/opm_operator` from `.` (0028:OQ11).

## Research & Decisions

### Starting point

**Context**: the module had to exist before any other 0028 change could be designed against it.
**Explored**: `enhancements/0028/experiments/01-operator-module-render/` (module, `hack/generate.sh`, `hack/drift-check.sh`, `out/compare.txt`); `experiments/02-cli-bootstrap-install/` installed, upgraded and deleted that module's catalog-path form on kind.
**Decision**: copy the experiment's module and scripts and change them as recorded above: constants for names, all eight roles imported, subject-less roles through `#Role`, seccomp through the catalog, image tag and digest out of `#config`, derived `GOMEMLIMIT`, the operator package.
**Rationale**: every catalog workaround and every render cost is already measured there (5.7 s and 507 MB cold, 2 to 4 s warm).

### Catalog surfaces of 0028:D12

**Context**: the seccomp field and the subject-less role do not exist in catalog opm `4.5.2`, the latest release.
**Explored**: `catalog_opm/src/resources/v1beta1/role.cue` (`#RoleSchema.subjects` requires one subject; the transformer always renders a binding); the catalog's security-context trait has no seccomp field. The catalog change `add-seccomp-and-subjectless-roles` was not yet written when this proposal was.
**Decision**: gate implementation on its release (proposal, "Dependencies / gates"); task 1.1 records the released field names in this section before any module code is written.
**Rationale**: the module pins a published release; designing against unreleased field names would fix names the catalog change has not chosen.

## Risks / Trade-offs

- [A catalog release changes how it derives workload selector labels] → the module's selector would change, which 0028:D2:R11 forbids and which would make a module upgrade fail on the immutable selector. The render test pins the selector literally, so the catalog pin bump fails CI; the fix is then a catalog change or holding the pin, never a module release with a new selector.
- [`cue import` output differs across `cue` versions] → the drift check would fail with no real drift. Both CI and the task use the pinned `CUE_VERSION`; a `cue` bump regenerates the files in the same PR.
- [The module's core and catalog pins go stale, since `task deps:cascade` does not cover them yet] → accepted until 0028:D7's cascade change; the render test catches a pin the library can no longer render.
- [The module's data on `main` track `main`'s `config/` while its image names the last operator release] → expected between an operator release and the next module release; release mode of the drift check is what refuses a module release whose data differ from the release it names.
- [Implementation waits on two other repositories' releases] → the gate is task 1.1, and nothing in sections 2 to 5 can be done meaningfully without it.

## Migration Plan

None. Nothing is published, `dist/install.yaml` and `config/` are unchanged, and no running cluster sees the module until the cli change installs it. Rollback is reverting the PR.
