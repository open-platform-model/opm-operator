## Why

The operator is installed from a kustomize-built manifest (`config/default`, rendered by `task operator:installer` into `dist/install.yaml`, and copied into the cli as its embedded manifest). Its tuning lives as patches on a live Deployment that the next install overwrites, and nothing compares the CRDs and RBAC the controller declares with what users apply.

OPM should install its own controller with OPM. The owner decided on 2026-10-04 that the operator ships as an OPM module published from this repository on a version train of its own, that `opm operator install` deploys it as a CLI-owned ModuleInstance pulled from a registry (a mirror covers air-gapped clusters), and that the module renders the operator through the first-party catalog's abstractions rather than as objects written in the manifest's shape, so the operator is the proof that the catalog carries a production controller. That instance stays CLI-owned for its whole life; the operator never reconciles the instance that deploys it.

This change authors that module: the one source of the operator's install shape, rendered through the catalog, with its generated parts checked against the controller's own declarations. Publishing it and installing it are separate changes.

## What Changes

- **New module `opmodel.dev/modules/opm_operator` on the v0 major**, under `modules/opm_operator/` in this repository. It renders the four CRDs, the Namespace, the controller Deployment, its ServiceAccount, the metrics Service, and every Role, ClusterRole and binding the operator ships, each through the catalog resource made for its kind (`#CRDs`, `#Namespaces`, `#StatelessWorkload` with its traits, `#ServiceAccount`, `#Role`), with no raw-objects component.
- **The five administrator ClusterRoles render as roles with no subjects**, and the pod and container carry `seccompProfile: RuntimeDefault`, through the two catalog surfaces the catalog_opm change `add-seccomp-and-subjectless-roles` adds. The pods satisfy Pod Security `restricted`, as the manifest's pods do today.
- **Generated CRDs and RBAC.** `hack/operator-module/generate.sh` imports `config/crd/bases/*.yaml` and the eight `config/rbac` role files into CUE with `cue import`. A drift check fails when `config/` is stale against `task dev:manifests` or when the module's data are stale against `config/`, on every pull request, and can be pointed at the `config/` of an operator release tag for the module's future release gate.
- **Fixed names and one selector.** Every object keeps the name the earlier manifest gave it, except the three bindings, which take the catalog's names. The Deployment's selector is pinned by a test so no module version ever changes it. The module renders only for the instance `opm-operator` in `opm-operator-system`.
- **`#config`, the operator's tuning surface:** image repository, registry mapping, default service account, resources, replicas and extra arguments. The image tag and digest are not values: the module names the operator image it deploys by tag and digest, and that operator version is readable from the module's source without a render.
- **A render test against the module's own pins**, in `task dev:test`, asserting the object set, names, selector, security posture and every `#config` field's effect.

Not in this change: the module's release unit, tags, signing, publishing and the install manifest rendered from it (`release-operator-module`); the module's place in the release cascade; everything the cli does (locating the operator, install, migration from a manifest install). Nothing is published; `dist/install.yaml` and `config/` are unchanged.

## Dependencies / gates

**GATED on an opm catalog release carrying `add-seccomp-and-subjectless-roles`.** Implementation MUST NOT start until the catalog_opm change `add-seccomp-and-subjectless-roles` has merged and a release of `opmodel.dev/catalogs/opm` v4 carrying it is published on GHCR, because the module pins that release and its seccomp field and subject-less role do not exist before it (the latest release today, 4.5.2, has neither). Task 1.1 checks the gate and stops if it does not hold. The proposal is authored ahead of it; the supervisor releases implementation.

The render test also needs a core `v2` version that the operator's pinned library (`go.mod`, library v1.0.0-beta.1) renders; task 1.1 checks that too.

Downstream: `release-operator-module` in this repository starts only after this change merges and reads its directory, image file and task names.

## Capabilities

### New Capabilities

- `operator-module`: the operator's OPM module: its object set and names, its catalog-only rendering, its generated CRDs and RBAC and their drift check, its Pod Security posture, its fixed selector and instance coordinates, its `#config` tuning surface, the operator version it names, and its offline render against its own pins.

### Modified Capabilities

None.

## Impact

- **New tree** `modules/opm_operator/` (CUE module with `cue.mod`, `identity/`, `operator/`, `module.cue`, `components.cue`, two generated files, README). It ships in no artifact of the operator release: the image, `dist/install.yaml` and the docs bundle are unchanged.
- **Scripts and tasks**: `hack/operator-module/{generate,drift-check}.sh`, a new `.tasks/operator-module.yaml` include (`operator-module:generate`, `operator-module:drift`).
- **CI**: `test.yml` gains the manifests-then-drift step; the render test runs inside the existing `task dev:test`.
- **Tests**: a new `test/integration/operatormodule` package (render test, plus a Pod Security admission check).
- **API types, controllers, RBAC markers**: none. No reconcile phase changes.
- **Release class**: none for the operator binary. Every commit uses a hidden type (`build`, `test`, `ci`, `docs`), because release-please's root package covers the whole tree and a `feat` here would cut an operator release with no binary change, until `release-operator-module` gives the module its own release unit. For the module itself this is its first version (`0.1.0` on the `v0` major); nothing is published here.
