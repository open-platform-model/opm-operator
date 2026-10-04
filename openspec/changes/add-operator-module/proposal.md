## Why

The operator is installed from a kustomize-built manifest (`config/default`, rendered by `task operator:installer` into `dist/install.yaml`, and copied into the cli as its embedded manifest). Its tuning lives as patches on a live Deployment that the next install overwrites, and nothing compares the CRDs and RBAC the controller declares with what users apply. Enhancement 0028 makes the operator an ordinary OPM module, released from this repository and installed by the cli as a CLI-owned ModuleInstance. This change authors that module: the one source of the operator's install shape, rendered through the first-party catalog, with its generated parts checked against the controller's own declarations.

## What Changes

- **New module `opmodel.dev/modules/opm_operator` on the v0 major**, under `modules/opm_operator/` in this repository. It renders the four CRDs, the Namespace, the controller Deployment, its ServiceAccount, the metrics Service, and every Role, ClusterRole and binding the operator ships, each through a catalog resource made for it (`#CRDs`, `#Namespaces`, `#StatelessWorkload` with its traits, `#ServiceAccount`, `#Role`), with no raw-object component (0028:D2:R12).
- **The five administrator ClusterRoles render as roles with no subjects**, and the pod and container carry `seccompProfile: RuntimeDefault`, using the two catalog surfaces 0028:D12 adds. The pods satisfy Pod Security `restricted` (0028:D2:R8).
- **Generated CRDs and RBAC.** `hack/operator-module/generate.sh` imports `config/crd/bases/*.yaml` and the `config/rbac` role files into CUE with `cue import` (no `@embed`: the kernel loads only `.cue` files). A drift check regenerates after `task dev:manifests` and fails on any difference, in PR CI and as a task a module release runs against the operator release it deploys (0028:D2:R1-R3).
- **Fixed names and one selector.** Every object keeps the name the earlier manifest gave it, except the three bindings, which take the catalog's names (0028:D2:R10). The Deployment's selector is pinned by a test so no module version ever changes it (0028:D2:R11). The module renders only for the instance `opm-operator` in `opm-operator-system`.
- **`#config`, the operator's tuning surface (0028:D5):** image repository, registry mapping, default service account, resources, replicas and extra arguments. The image tag and digest are not values: the module names the operator image it deploys by tag and digest, and that operator version is readable from the module before install (0028:D1:R11, 0028:D5:R6).
- **A render test against the module's own pins**, in `task dev:test`, asserting the object set, names, selector, security posture and every `#config` field's effect.

Not in this change: the module's release unit, tags and publishing (0028:D1:R4/R5/R9/R10/R13), the install manifest rendered from the module and the end of the operator's own manifest (0028:D2:R9/R13), cascade wiring (0028:D7), and everything the cli does (0028:D3, D4, D8-D11). Nothing is published; `dist/install.yaml` and `config/` are unchanged.

## Dependencies / gates

**GATED on a released catalog carrying 0028:D12.** Implementation MUST NOT start until the catalog_opm change `add-seccomp-and-subjectless-roles` has merged and a catalog release `opmodel.dev/catalogs/opm` v4 carrying it is published on GHCR, because the module pins that release and its seccomp and subject-less role fields do not exist before it. Task 1.1 checks the gate and stops if it does not hold. The proposal is authored ahead of it; the supervisor releases implementation.

The release also needs a core `v2` version the operator's pinned library (`go.mod`, library v1.0.0-beta.1) renders, for the render test; task 1.1 checks that too.

## Capabilities

### New Capabilities

- `operator-module`: the operator's OPM module: its object set and names, its catalog-only rendering, its generated CRDs and RBAC and their drift check, its Pod Security posture, its fixed selector and instance coordinates, its `#config` tuning surface, and the operator version it names.

### Modified Capabilities

None.

## Impact

- **New tree** `modules/opm_operator/` (CUE module with `cue.mod`, `identity/`, `operator/`, `module.cue`, `components.cue`, two generated files). It ships in no artifact of the operator release: the image, `dist/install.yaml` and the docs bundle are unchanged.
- **Scripts and tasks**: `hack/operator-module/{generate,drift-check}.sh`, a new `.tasks/operator-module.yaml` include (`operator-module:generate`, `operator-module:drift`).
- **CI**: `test.yml` gains the manifests-then-drift step; the render test runs inside the existing `task dev:test`.
- **Tests**: a new `test/integration/operatormodule` package (render test, plus an envtest Pod Security admission check).
- **API, controllers, RBAC markers**: none.
- **Release class**: none for the operator. Every commit uses a hidden type (`build`, `test`, `ci`, `docs`), because release-please's root package covers the whole tree and a `feat` here would cut an operator release with no binary change, until the module gets its own release unit. After GA this would be MINOR for the module and nothing for the operator.
