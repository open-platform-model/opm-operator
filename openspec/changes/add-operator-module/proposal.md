## Why

The operator is installed from a kustomize-built manifest (`config/default`, rendered by `task operator:installer` into `dist/install.yaml`, and copied into the cli as its embedded manifest). Its tuning lives as patches on a live Deployment that the next install overwrites, and nothing compares the CRDs and RBAC the controller declares with what users apply.

OPM should install its own controller with OPM. The owner decided on 2026-10-04 that the operator ships as an OPM module published from this repository on a version train of its own, that `opm operator install` deploys it as a CLI-owned ModuleInstance pulled from a registry (a mirror covers air-gapped clusters), and that the module renders the operator through the first-party catalog's abstractions rather than as objects written in the manifest's shape, so the operator is the proof that the catalog carries a production controller. That instance stays CLI-owned for its whole life; the operator never reconciles the instance that deploys it.

This change authors that module, rendered through the catalog, with its generated parts checked against the controller's own declarations and its hand-written Deployment and Service checked against the kustomize tree, which stays the source of `dist/install.yaml` until the module replaces it. Publishing it and installing it are separate changes.

## What Changes

- **New module `opmodel.dev/modules/opm_operator` on the v0 major**, under `modules/opm_operator/` in this repository. It renders the four CRDs, the Namespace, the controller Deployment, its ServiceAccount, the metrics Service, and every Role, ClusterRole and binding the operator ships, each through the catalog resource made for its kind (`#CRDs`, `#Namespaces`, `#StatelessWorkload` with its traits, `#ServiceAccount`, `#Role`). The one exception is temporary: the five administrator ClusterRoles render through the catalog's raw-objects resource (`resources/v1alpha1 #Objects`, `objects@v1alpha1`), because the released catalog's `#Role` cannot yet render a role with no subjects.
- **The pod carries `seccompProfile: RuntimeDefault`** through the catalog surface catalog_opm PR #141 adds (change `add-seccomp-and-subjectless-roles`, which since the supervisor's split of 2026-10-04 ships `seccompProfile` only). The pods satisfy Pod Security `restricted`, as the manifest's pods do today.
- **The five administrator ClusterRoles render unbound through `objects@v1alpha1`.** Roles with no subjects moved to a separate catalog_opm change, `add-subjectless-roles`, which waits for a cli release that fixes a false refusal in the cli's catalog compatibility gate. Until a catalog release carries it, the module writes these five roles as raw objects with no binding, rules still taken from the generated RBAC data. A follow-up switches them to `#Role` once that release exists (task 6.1).
- **Generated CRDs and RBAC.** `hack/operator-module/generate.sh` imports `config/crd/bases/*.yaml` and every Role and ClusterRole listed in `config/rbac/kustomization.yaml` into CUE with `cue import`, from `config/` or any tree given as `SRC=`. A drift check fails when `config/` is stale against `task dev:manifests` or when the module's data are stale against `config/`, on every pull request, and can be pointed at the `config/` of an operator release tag for the module's future release gate.
- **Fixed names and one selector.** Every object keeps the name the earlier manifest gave it, except the three bindings, which take the catalog's names. The Deployment's selector is pinned by a test so no module version ever changes it. The module renders only for the instance `opm-operator` in `opm-operator-system`.
- **`#config`, the operator's tuning surface:** image repository, registry mapping, default service account, resources, replicas and extra arguments. The image tag and digest are not values: the module names the operator image it deploys by tag and digest, and that operator version is readable from the module's source without a render.
- **A render test against the module's own pins**, in `task dev:test`, asserting the object set, names, selector, security posture and every `#config` field's effect, and comparing the controller's pod spec and metrics Service with a kustomize build of `config/default` so the two sources cannot drift before the module becomes the only one.

Not in this change: rendering the five administrator ClusterRoles through `#Role` (the follow-up of task 6.1, after a catalog release carries `add-subjectless-roles`); the module's release unit, tags, signing, publishing and the install manifest rendered from it (`release-operator-module`); the module's place in the release cascade; everything the cli does (locating the operator, install, migration from a manifest install). Nothing is published; `dist/install.yaml` and `config/` are unchanged.

## Dependencies / gates

**GATED on an opm catalog release carrying the seccomp profile.** Implementation MUST NOT start until catalog_opm PR #141 (change `add-seccomp-and-subjectless-roles`, seccomp only after the split) has merged and a release of `opmodel.dev/catalogs/opm` v4 carrying it is published on GHCR, because the module pins that release and the seccomp field does not exist before it (the latest release today, 4.5.2, lacks it). This gate does not wait for `add-subjectless-roles`; the module works around its absence as described above.

**GATED on an operator release carrying `refuse-own-instance`.** Implementation MUST NOT start until opm-operator PR #211 (`refuse-own-instance`: the operator refuses to reconcile the instance that deploys it) has merged and an operator release containing it is published. That release is the module's minimum operator version: the module must never name an operator below it, because an older operator would adopt, prune and wedge on its own CLI-owned instance once someone flips the instance's owner. Task 1.1 records it and task 3.2 writes it to `hack/operator-module/min-operator-version`; the render test refuses a module that names an older operator.

Task 1.1 checks both gates and stops if either does not hold. The proposal is authored ahead of them; the supervisor releases implementation.

The render test also needs a core `v2` version that the operator's pinned library (`go.mod`, library v1.0.0-beta.1) renders; task 1.1 checks that too.

Downstream: `release-operator-module` in this repository starts only after this change merges and reads its directory, image file and task names.

## Capabilities

### New Capabilities

- `operator-module`: the operator's OPM module: its object set and names, its rendering through catalog resources (raw objects only for the five administrator ClusterRoles, until the catalog can render them), its generated CRDs and RBAC and their drift check, its Pod Security posture, its fixed selector and instance coordinates, its `#config` tuning surface, the operator version it names and the minimum it must not fall below, and its offline render against its own pins.

### Modified Capabilities

None.

## Impact

- **New tree** `modules/opm_operator/` (CUE module with `cue.mod`, `identity/`, `operator/`, `module.cue`, `components.cue`, two generated files, README). It ships in no artifact of the operator release: the image, `dist/install.yaml` and the docs bundle are unchanged.
- **Scripts and tasks**: `hack/operator-module/{generate,drift-check}.sh`, `hack/operator-module/min-operator-version` (one line, the minimum operator release tag), a new `.tasks/operator-module.yaml` include (`operator-module:generate`, `operator-module:drift`); `dev:test` gains `:tool:kustomize` as a dependency for the parity comparison.
- **Release cascade**: the module is not in `task deps:cascade`; a cascade PR whose library bump breaks the module's render test carries the module's `cue mod get` fix on its own branch.
- **CI**: `test.yml` gains the manifests-then-drift step; the render test runs inside the existing `task dev:test`.
- **Tests**: a new `test/integration/operatormodule` package (render test, plus a Pod Security admission check).
- **API types, controllers, RBAC markers**: none. No reconcile phase changes.
- **Release class**: none for the operator binary. Every commit and the PR title use a hidden type (`build`, `test`, `ci`, `docs`; the PR title such as `build(module): add the operator's OPM module`, since the squash merge keeps only the title), because release-please's root package covers the whole tree and a `feat` here would cut an operator release with no binary change, until `release-operator-module` gives the module its own release unit. For the module itself this is its first version (`0.1.0` on the `v0` major); nothing is published here.
